package sora

import (
	"net/url"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvertToOpenAIVideoReturnsSignedContentURL(t *testing.T) {
	originalServerAddress := system_setting.ServerAddress
	system_setting.ServerAddress = "https://gateway.example"
	t.Cleanup(func() {
		system_setting.ServerAddress = originalServerAddress
	})

	task := &model.Task{
		TaskID: "task_public",
		UserId: 42,
		Status: model.TaskStatusSuccess,
		Data:   []byte(`{"id":"upstream-id","task_id":"upstream-id","status":"completed","content_url":"/v1/videos/upstream-id/content","output":{"filename":"../unsafe/video.mp4"}}`),
	}

	data, err := (&TaskAdaptor{}).ConvertToOpenAIVideo(task)
	require.NoError(t, err)

	var response map[string]any
	require.NoError(t, common.Unmarshal(data, &response))
	assert.Equal(t, "task_public", response["id"])
	assert.Equal(t, "task_public", response["task_id"])
	contentURL := response["content_url"].(string)
	parsed, err := url.Parse(contentURL)
	require.NoError(t, err)
	assert.Equal(t, "/v1/videos/task_public/content/video.mp4", parsed.EscapedPath())
	assert.Equal(t, contentURL, response["metadata"].(map[string]any)["url"])
	assert.Equal(t, "video.mp4", response["output"].(map[string]any)["filename"])
	assert.NotEmpty(t, parsed.Query().Get("uid"))
	assert.NotEmpty(t, parsed.Query().Get("expires"))
	assert.NotEmpty(t, parsed.Query().Get("signature"))
}

func TestConvertToOpenAIVideoFallsBackFilename(t *testing.T) {
	task := &model.Task{
		TaskID: "task_public",
		UserId: 42,
		Status: model.TaskStatusSuccess,
		Data:   []byte(`{"status":"completed","output":{}}`),
	}

	data, err := (&TaskAdaptor{}).ConvertToOpenAIVideo(task)
	require.NoError(t, err)

	var response map[string]any
	require.NoError(t, common.Unmarshal(data, &response))
	assert.Equal(t, "task_public.mp4", response["output"].(map[string]any)["filename"])
}

func TestConvertToOpenAIVideoDoesNotAddDownloadFieldsBeforeSuccess(t *testing.T) {
	task := &model.Task{
		TaskID: "task_public",
		UserId: 42,
		Status: model.TaskStatusQueued,
		Data:   []byte(`{"status":"queued"}`),
	}

	data, err := (&TaskAdaptor{}).ConvertToOpenAIVideo(task)
	require.NoError(t, err)

	var response map[string]any
	require.NoError(t, common.Unmarshal(data, &response))
	assert.NotContains(t, response, "content_url")
	assert.NotContains(t, response, "metadata")
	assert.NotContains(t, response, "output")
}
