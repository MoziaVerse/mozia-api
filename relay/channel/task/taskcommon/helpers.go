package taskcommon

import (
	"crypto/hmac"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// UnmarshalMetadata converts a map[string]any metadata to a typed struct via JSON round-trip.
// This replaces the repeated pattern: json.Marshal(metadata) → json.Unmarshal(bytes, &target).
func UnmarshalMetadata(metadata map[string]any, target any) error {
	if metadata == nil {
		return nil
	}
	// Prevent metadata from overriding model fields to avoid billing bypass.
	delete(metadata, "model")
	metaBytes, err := common.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("marshal metadata failed: %w", err)
	}
	if err := common.Unmarshal(metaBytes, target); err != nil {
		return fmt.Errorf("unmarshal metadata failed: %w", err)
	}
	return nil
}

// DefaultString returns val if non-empty, otherwise fallback.
func DefaultString(val, fallback string) string {
	if val == "" {
		return fallback
	}
	return val
}

// DefaultInt returns val if non-zero, otherwise fallback.
func DefaultInt(val, fallback int) int {
	if val == 0 {
		return fallback
	}
	return val
}

// EncodeLocalTaskID encodes an upstream operation name to a URL-safe base64 string.
// Used by Gemini/Vertex to store upstream names as task IDs.
func EncodeLocalTaskID(name string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(name))
}

// DecodeLocalTaskID decodes a base64-encoded upstream operation name.
func DecodeLocalTaskID(id string) (string, error) {
	b, err := base64.RawURLEncoding.DecodeString(id)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// BuildProxyURL constructs the video proxy URL using the public task ID.
// e.g., "https://your-server.com/v1/videos/task_xxxx/content"
func BuildProxyURL(taskID string) string {
	return fmt.Sprintf("%s/v1/videos/%s/content", system_setting.ServerAddress, taskID)
}

const SignedVideoURLTTL = 24 * time.Hour

var (
	ErrSignedVideoExpired          = errors.New("signed video url expired")
	ErrSignedVideoInvalidSignature = errors.New("invalid signed video url signature")
)

func SanitizeVideoFilename(filename, taskID string) string {
	filename = strings.TrimSpace(filename)
	filename = strings.ReplaceAll(filename, "\\", "/")
	filename = strings.TrimSpace(path.Base(filename))
	if filename == "" || filename == "." || filename == ".." || filename == "/" {
		taskID = strings.TrimSpace(taskID)
		taskID = strings.TrimSpace(path.Base(strings.ReplaceAll(taskID, "\\", "/")))
		if taskID == "" || taskID == "." || taskID == ".." || taskID == "/" {
			taskID = "video"
		}
		return taskID + ".mp4"
	}
	if path.Ext(filename) == "" {
		filename += ".mp4"
	}
	return filename
}

func TaskVideoFilename(task *model.Task) string {
	if task == nil {
		return SanitizeVideoFilename("", "")
	}
	return SanitizeVideoFilename(gjson.GetBytes(task.Data, "output.filename").String(), task.TaskID)
}

func BuildSignedVideoProxyURL(userID int, taskID, filename string) string {
	return BuildSignedVideoProxyURLAt(time.Now(), userID, taskID, filename)
}

func BuildSignedVideoProxyURLAt(now time.Time, userID int, taskID, filename string) string {
	return buildSignedVideoURLAt(now, "/v1/videos", userID, taskID, filename)
}

func BuildSignedVideoGenerationURLAt(now time.Time, userID int, taskID, filename string) string {
	return buildSignedVideoURLAt(now, "/v1/video/generations", userID, taskID, filename)
}

func buildSignedVideoURLAt(now time.Time, resourcePath string, userID int, taskID, filename string) string {
	filename = SanitizeVideoFilename(filename, taskID)
	expires := now.Add(SignedVideoURLTTL).Unix()
	signature := signedVideoSignature(userID, taskID, filename, expires)
	values := url.Values{}
	values.Set("uid", strconv.Itoa(userID))
	values.Set("expires", strconv.FormatInt(expires, 10))
	values.Set("signature", signature)
	return fmt.Sprintf("%s%s/%s/content/%s?%s",
		strings.TrimRight(system_setting.ServerAddress, "/"),
		resourcePath,
		url.PathEscape(taskID),
		url.PathEscape(filename),
		values.Encode(),
	)
}

func VerifySignedVideoProxy(userID int, taskID, filename string, expires int64, signature string, now time.Time) error {
	if now.Unix() > expires {
		return ErrSignedVideoExpired
	}
	expected := signedVideoSignature(userID, taskID, SanitizeVideoFilename(filename, taskID), expires)
	if !hmac.Equal([]byte(signature), []byte(expected)) {
		return ErrSignedVideoInvalidSignature
	}
	return nil
}

func signedVideoSignature(userID int, taskID, filename string, expires int64) string {
	payload := strings.Join([]string{
		"video-download:v1",
		strconv.Itoa(userID),
		taskID,
		filename,
		strconv.FormatInt(expires, 10),
	}, "\n")
	return common.GenerateHMAC(payload)
}

// Status-to-progress mapping constants for polling updates.
const (
	ProgressSubmitted  = "10%"
	ProgressQueued     = "20%"
	ProgressInProgress = "30%"
	ProgressComplete   = "100%"
)

// ---------------------------------------------------------------------------
// BaseBilling — embeddable no-op implementations for TaskAdaptor billing methods.
// Adaptors that do not need custom billing can embed this struct directly.
// ---------------------------------------------------------------------------

type BaseBilling struct{}

// EstimateBilling returns nil (no extra ratios; use base model price).
func (BaseBilling) EstimateBilling(_ *gin.Context, _ *relaycommon.RelayInfo) map[string]float64 {
	return nil
}

// AdjustBillingOnSubmit returns nil (no submit-time adjustment).
func (BaseBilling) AdjustBillingOnSubmit(_ *relaycommon.RelayInfo, _ []byte) map[string]float64 {
	return nil
}

// AdjustBillingOnComplete returns 0 (keep pre-charged amount).
func (BaseBilling) AdjustBillingOnComplete(_ *model.Task, _ *relaycommon.TaskInfo) int {
	return 0
}
