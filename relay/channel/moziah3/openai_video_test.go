package moziah3

import (
	"net/url"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvertToOpenAIVideoReturnsSignedGatewayContentURL(t *testing.T) {
	originalServerAddress := system_setting.ServerAddress
	system_setting.ServerAddress = "https://gateway.example"
	t.Cleanup(func() { system_setting.ServerAddress = originalServerAddress })

	task := &model.Task{
		TaskID:     "task_public",
		UserId:     42,
		Status:     model.TaskStatusSuccess,
		Progress:   "100%",
		Properties: model.Properties{OriginModelName: "minimax/minimax-h3-fl2va"},
		PrivateData: model.TaskPrivateData{
			UpstreamTaskID: "upstream-id",
			ResultURL:      "https://gateway.example/v1/videos/task_public/content",
		},
	}

	data, err := (&TaskAdaptor{}).ConvertToOpenAIVideo(task)
	require.NoError(t, err)
	var response map[string]any
	require.NoError(t, common.Unmarshal(data, &response))
	assert.Equal(t, "completed", response["status"])
	assert.Equal(t, "minimax/minimax-h3-fl2va", response["model"])

	contentURL := response["content_url"].(string)
	parsed, err := url.Parse(contentURL)
	require.NoError(t, err)
	assert.Equal(t, "/v1/videos/task_public/content/task_public.mp4", parsed.EscapedPath())
	assert.NotEmpty(t, parsed.Query().Get("signature"))
	assert.Equal(t, contentURL, response["metadata"].(map[string]any)["url"])
}

func TestConvertToOpenAIVideoIncludesFailure(t *testing.T) {
	task := &model.Task{TaskID: "task_public", Status: model.TaskStatusFailure, FailReason: "GPU worker failed"}

	data, err := (&TaskAdaptor{}).ConvertToOpenAIVideo(task)
	require.NoError(t, err)
	var response map[string]any
	require.NoError(t, common.Unmarshal(data, &response))
	assert.Equal(t, "failed", response["status"])
	assert.Equal(t, "GPU worker failed", response["error"].(map[string]any)["message"])
}
