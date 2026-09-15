package openai

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/dto"
)

const hdTTSModel = "matrix-tts-v1-hd"

var voiceWarmupClient = &http.Client{Timeout: 15 * time.Second}
var voiceWarmupSlots = make(chan struct{}, 1)
var voiceWarmupSeen = struct {
	sync.Mutex
	keys map[[32]byte]time.Time
}{keys: make(map[[32]byte]time.Time)}

// WarmTTSVoiceCache registers an HD request's reference voice on an isolated
// standby instance. It is best-effort, bounded to one request at a time, and
// never affects the client response.
func WarmTTSVoiceCache(request any) {
	target := os.Getenv("TTS_VOICE_WARMUP_URL")
	audio, ok := request.(*dto.AudioRequest)
	if target == "" || !ok || audio.Model != hdTTSModel || len(audio.RefAudio) == 0 {
		return
	}
	parsedTarget, err := url.ParseRequestURI(target)
	if err != nil || parsedTarget.Scheme != "http" || parsedTarget.Path != "/v1/voices" {
		return
	}
	targetIP, err := netip.ParseAddr(parsedTarget.Hostname())
	if err != nil || !(targetIP.IsPrivate() || targetIP.IsLoopback()) {
		return
	}

	key := sha256.Sum256(append(append([]byte(nil), audio.RefAudio...), audio.RefText...))
	select {
	case voiceWarmupSlots <- struct{}{}:
	default:
		return
	}
	if !claimVoiceWarmup(key) {
		<-voiceWarmupSlots
		return
	}

	payload, err := json.Marshal(map[string]json.RawMessage{
		"ref_audio": audio.RefAudio,
		"ref_text":  audio.RefText,
	})
	if err != nil {
		<-voiceWarmupSlots
		return
	}
	go func() {
		defer func() { <-voiceWarmupSlots }()
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(payload))
		if err != nil {
			forgetVoiceWarmup(key)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := voiceWarmupClient.Do(req)
		if err != nil || resp == nil || resp.StatusCode/100 != 2 {
			forgetVoiceWarmup(key)
		}
		if resp != nil {
			resp.Body.Close()
		}
	}()
}

func claimVoiceWarmup(key [32]byte) bool {
	voiceWarmupSeen.Lock()
	defer voiceWarmupSeen.Unlock()
	now := time.Now()
	for oldKey, expiresAt := range voiceWarmupSeen.keys {
		if now.After(expiresAt) {
			delete(voiceWarmupSeen.keys, oldKey)
		}
	}
	if _, exists := voiceWarmupSeen.keys[key]; exists {
		return false
	}
	voiceWarmupSeen.keys[key] = now.Add(30 * time.Minute)
	return true
}

func forgetVoiceWarmup(key [32]byte) {
	voiceWarmupSeen.Lock()
	delete(voiceWarmupSeen.keys, key)
	voiceWarmupSeen.Unlock()
}
