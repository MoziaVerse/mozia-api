package seedance

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvertToOpenAIVideoSeparatesSuccessURLAndFailureReason(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		task := &model.Task{TaskID: "task_public", Status: model.TaskStatusSuccess, Progress: "100%"}
		task.Properties.OriginModelName = "public-model"
		task.PrivateData.ResultURL = "https://cdn.example/result.mp4"

		data, err := (&TaskAdaptor{}).ConvertToOpenAIVideo(task)
		require.NoError(t, err)
		var video dto.OpenAIVideo
		require.NoError(t, common.Unmarshal(data, &video))
		assert.Equal(t, dto.VideoStatusCompleted, video.Status)
		assert.Equal(t, "https://cdn.example/result.mp4", video.Metadata["url"])
		assert.Nil(t, video.Error)
	})

	t.Run("failure", func(t *testing.T) {
		task := &model.Task{TaskID: "task_public", Status: model.TaskStatusFailure, Progress: "100%", FailReason: "provider failed"}

		data, err := (&TaskAdaptor{}).ConvertToOpenAIVideo(task)
		require.NoError(t, err)
		var video dto.OpenAIVideo
		require.NoError(t, common.Unmarshal(data, &video))
		assert.Equal(t, dto.VideoStatusFailed, video.Status)
		require.NotNil(t, video.Error)
		assert.Equal(t, "provider failed", video.Error.Message)
		assert.Empty(t, video.Metadata)
	})
}
