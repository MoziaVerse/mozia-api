package taskcommon

import (
	"net/url"
	"path"
	"strconv"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildSignedVideoProxyURLAndVerify(t *testing.T) {
	originalServerAddress := system_setting.ServerAddress
	system_setting.ServerAddress = "https://gateway.example"
	t.Cleanup(func() {
		system_setting.ServerAddress = originalServerAddress
	})

	now := time.Unix(1_700_000_000, 0)
	signedURL := BuildSignedVideoProxyURLAt(now, 42, "task_public", "../folder/final clip.mp4")

	parsed, err := url.Parse(signedURL)
	require.NoError(t, err)
	assert.Equal(t, "/v1/videos/task_public/content/final%20clip.mp4", parsed.EscapedPath())

	query := parsed.Query()
	userID, err := strconv.Atoi(query.Get("uid"))
	require.NoError(t, err)
	expires, err := strconv.ParseInt(query.Get("expires"), 10, 64)
	require.NoError(t, err)

	err = VerifySignedVideoProxy(userID, "task_public", "final clip.mp4", expires, query.Get("signature"), now.Add(23*time.Hour))
	require.NoError(t, err)
}

func TestBuildSignedVideoGenerationURLAndVerify(t *testing.T) {
	originalServerAddress := system_setting.ServerAddress
	system_setting.ServerAddress = "https://gateway.example"
	t.Cleanup(func() {
		system_setting.ServerAddress = originalServerAddress
	})

	now := time.Unix(1_700_000_000, 0)
	signedURL := BuildSignedVideoGenerationURLAt(now, 42, "task_public", "final clip.mp4")

	parsed, err := url.Parse(signedURL)
	require.NoError(t, err)
	assert.Equal(t, "/v1/video/generations/task_public/content/final%20clip.mp4", parsed.EscapedPath())

	query := parsed.Query()
	expires, err := strconv.ParseInt(query.Get("expires"), 10, 64)
	require.NoError(t, err)
	require.NoError(t, VerifySignedVideoProxy(42, "task_public", "final clip.mp4", expires, query.Get("signature"), now.Add(23*time.Hour)))
}

func TestVerifySignedVideoProxyRejectsExpiredAndTamperedFields(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	signedURL := BuildSignedVideoProxyURLAt(now, 7, "task_alpha", "video.mp4")
	parsed, err := url.Parse(signedURL)
	require.NoError(t, err)

	query := parsed.Query()
	expires, err := strconv.ParseInt(query.Get("expires"), 10, 64)
	require.NoError(t, err)
	signature := query.Get("signature")

	assert.ErrorIs(t, VerifySignedVideoProxy(7, "task_alpha", "video.mp4", expires, signature, now.Add(25*time.Hour)), ErrSignedVideoExpired)
	assert.ErrorIs(t, VerifySignedVideoProxy(8, "task_alpha", "video.mp4", expires, signature, now), ErrSignedVideoInvalidSignature)
	assert.ErrorIs(t, VerifySignedVideoProxy(7, "task_beta", "video.mp4", expires, signature, now), ErrSignedVideoInvalidSignature)
	assert.ErrorIs(t, VerifySignedVideoProxy(7, "task_alpha", "other.mp4", expires, signature, now), ErrSignedVideoInvalidSignature)
	assert.ErrorIs(t, VerifySignedVideoProxy(7, "task_alpha", "video.mp4", expires+1, signature, now), ErrSignedVideoInvalidSignature)
}

func TestTaskVideoFilenameSanitizesAndFallsBack(t *testing.T) {
	task := &model.Task{
		TaskID: "task_public",
		Data:   []byte(`{"output":{"filename":"../nested/video.mp4"}}`),
	}
	assert.Equal(t, "video.mp4", TaskVideoFilename(task))

	task.Data = []byte(`{"output":{"filename":""}}`)
	assert.Equal(t, "task_public.mp4", TaskVideoFilename(task))

	task.Data = []byte(`{"output":{"filename":".."}}`)
	assert.Equal(t, "task_public.mp4", TaskVideoFilename(task))

	task.Data = []byte(`{"output":{"filename":"download"}}`)
	assert.Equal(t, "download.mp4", TaskVideoFilename(task))

	assert.Equal(t, "download.mp4", path.Base(TaskVideoFilename(task)))
}
