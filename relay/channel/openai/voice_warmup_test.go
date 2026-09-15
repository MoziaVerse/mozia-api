package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/dto"
	"github.com/stretchr/testify/require"
)

func resetVoiceWarmup() {
	voiceWarmupSeen.Lock()
	voiceWarmupSeen.keys = make(map[[32]byte]time.Time)
	voiceWarmupSeen.Unlock()
}

func TestWarmTTSVoiceCacheOnlyPostsHDReferenceAudio(t *testing.T) {
	resetVoiceWarmup()
	received := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		received <- string(body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	t.Setenv("TTS_VOICE_WARMUP_URL", server.URL+"/v1/voices")

	WarmTTSVoiceCache(&dto.AudioRequest{Model: hdTTSModel, RefAudio: []byte(`"data:audio/wav;base64,AAAA"`), RefText: []byte(`"reference"`)})

	select {
	case body := <-received:
		require.Contains(t, body, "ref_audio")
		require.Contains(t, body, "ref_text")
	case <-time.After(time.Second):
		t.Fatal("warmup request was not sent")
	}
}

func TestWarmTTSVoiceCacheSkipsOtherModels(t *testing.T) {
	resetVoiceWarmup()
	called := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called <- struct{}{} }))
	defer server.Close()
	t.Setenv("TTS_VOICE_WARMUP_URL", server.URL+"/v1/voices")
	WarmTTSVoiceCache(&dto.AudioRequest{Model: "matrix-tts-v1", RefAudio: []byte(`"data:audio/wav;base64,AAAA"`)})
	select {
	case <-called:
		t.Fatal("unexpected warmup request")
	case <-time.After(100 * time.Millisecond):
	}
	_ = os.Unsetenv("TTS_VOICE_WARMUP_URL")
}
