package seedance

import (
	"net/url"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvertToOpenAIVideoSeparatesSuccessURLAndFailureReason(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		task := &model.Task{TaskID: "task_public", Status: model.TaskStatusSuccess, Progress: "100%"}
		task.Properties.OriginModelName = "public-model"
		task.PrivateData.ResultURL = "https://cdn.example/result.mp4"

		data, err := NewSeedanceVideosTaskAdaptor().ConvertToOpenAIVideo(task)
		require.NoError(t, err)
		var video dto.OpenAIVideo
		require.NoError(t, common.Unmarshal(data, &video))
		assert.Equal(t, dto.VideoStatusCompleted, video.Status)
		assert.Equal(t, "https://cdn.example/result.mp4", video.ContentURL)
		assert.Equal(t, "https://cdn.example/result.mp4", video.Metadata["url"])
		assert.Nil(t, video.Error)
	})

	t.Run("proxy URL becomes signed download", func(t *testing.T) {
		originalServerAddress := system_setting.ServerAddress
		system_setting.ServerAddress = "https://gateway.example"
		t.Cleanup(func() {
			system_setting.ServerAddress = originalServerAddress
		})

		task := &model.Task{
			TaskID:   "task_public",
			UserId:   42,
			Status:   model.TaskStatusSuccess,
			Progress: "100%",
			PrivateData: model.TaskPrivateData{
				ResultURL: "https://gateway.example/v1/videos/task_public/content",
			},
			Data: []byte(`{"output":{"filename":"result.mp4"}}`),
		}

		data, err := NewSeedanceVideosTaskAdaptor().ConvertToOpenAIVideo(task)
		require.NoError(t, err)
		assert.Contains(t, string(data), "&signature=")
		assert.NotContains(t, string(data), `\u0026`)
		var video dto.OpenAIVideo
		require.NoError(t, common.Unmarshal(data, &video))
		downloadURL, err := url.Parse(video.ContentURL)
		require.NoError(t, err)
		assert.Equal(t, "/v1/videos/task_public/content/result.mp4", downloadURL.EscapedPath())
		assert.Equal(t, "42", downloadURL.Query().Get("uid"))
		assert.NotEmpty(t, downloadURL.Query().Get("expires"))
		assert.NotEmpty(t, downloadURL.Query().Get("signature"))
		assert.Equal(t, video.ContentURL, video.Metadata["url"])
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
