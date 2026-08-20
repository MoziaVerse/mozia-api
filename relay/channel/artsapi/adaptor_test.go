package artsapi

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildRequestBodyPreservesArtsAPIImageObjects(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/video/generations", strings.NewReader(`{
		"model":"public-model",
		"prompt":"animate the scene",
		"duration":4,
		"resolution":"480p",
		"ratio":"16:9",
		"generate_audio":false,
		"images":[{"url":"https://example.com/frame.png","role":"first_frame"}]
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	info := &relaycommon.RelayInfo{
		OriginModelName: "public-model",
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl: "https://ai.artsapi.com",
		},
	}
	adaptor := &TaskAdaptor{}
	adaptor.Init(info)

	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	body, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(body)
	require.NoError(t, err)

	var payload map[string]interface{}
	require.NoError(t, common.Unmarshal(data, &payload))
	images, ok := payload["images"].([]interface{})
	require.True(t, ok)
	require.Len(t, images, 1)
	image, ok := images[0].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "https://example.com/frame.png", image["url"])
	assert.Equal(t, "first_frame", image["role"])
	assert.Equal(t, false, payload["generate_audio"])
}

func TestParseTaskResultCapturesArtsAPIUsage(t *testing.T) {
	result, err := (&TaskAdaptor{}).ParseTaskResult([]byte(`{
		"id":"cgt-task",
		"status":"completed",
		"data":[{"url":"https://example.com/video.mp4"}],
		"usage":{"completion_tokens":52772,"total_tokens":52772}
	}`))

	require.NoError(t, err)
	assert.Equal(t, "cgt-task", result.TaskID)
	assert.Equal(t, "SUCCESS", result.Status)
	assert.Equal(t, "https://example.com/video.mp4", result.Url)
	assert.Equal(t, 52772, result.CompletionTokens)
	assert.Equal(t, 52772, result.TotalTokens)
}

func TestEstimateBillingUsesPerRequestDefault(t *testing.T) {
	assert.Nil(t, (&TaskAdaptor{}).EstimateBilling(nil, nil))
}

func TestValidateRejectsAutomaticDuration(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/video/generations", strings.NewReader(`{
		"model":"artsdance-2-0-fast-260801",
		"prompt":"animate the scene",
		"duration":-1
	}`))
	c.Request.Header.Set("Content-Type", "application/json")
	info := &relaycommon.RelayInfo{
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
		ChannelMeta:   &relaycommon.ChannelMeta{ChannelBaseUrl: "https://ai.artsapi.com"},
	}

	taskErr := (&TaskAdaptor{}).ValidateRequestAndSetAction(c, info)

	require.NotNil(t, taskErr)
	assert.Equal(t, "invalid_duration", taskErr.Code)
}
