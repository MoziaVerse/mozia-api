package sora

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvertToOpenAIVideoReturnsPublicResultURL(t *testing.T) {
	task := &model.Task{
		TaskID: "task_public",
		Status: model.TaskStatusSuccess,
		PrivateData: model.TaskPrivateData{
			ResultURL: "https://api.example/v1/videos/task_public/content",
		},
		Data: []byte(`{"id":"upstream-id","task_id":"upstream-id","status":"completed","content_url":"/v1/videos/upstream-id/content","output":{"filename":"video.mp4"}}`),
	}

	data, err := (&TaskAdaptor{}).ConvertToOpenAIVideo(task)
	require.NoError(t, err)

	var response map[string]any
	require.NoError(t, common.Unmarshal(data, &response))
	assert.Equal(t, "task_public", response["id"])
	assert.Equal(t, "task_public", response["task_id"])
	assert.Equal(t, task.GetResultURL(), response["content_url"])
	assert.Equal(t, task.GetResultURL(), response["metadata"].(map[string]any)["url"])
	assert.Equal(t, "video.mp4", response["output"].(map[string]any)["filename"])
}
