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

func TestArtsAPIContentRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tt := range []struct {
		name        string
		fields      string
		wantContent string
		wantError   string
	}{
		{
			name:        "text and first frame without top-level prompt",
			fields:      `"content":[{"type":"text","text":"在这个空间里建造木屋"},{"type":"image_url","role":"first_frame","image_url":{"url":"https://example.com/frame.jpg"}}]`,
			wantContent: `[{"type":"text","text":"在这个空间里建造木屋"},{"type":"image_url","role":"first_frame","image_url":{"url":"https://example.com/frame.jpg"}}]`,
		},
		{
			name:        "text-only content",
			fields:      `"content":[{"type":"text","text":"build a cabin"}]`,
			wantContent: `[{"type":"text","text":"build a cabin"}]`,
		},
		{
			name:        "top-level prompt overrides content text",
			fields:      `"prompt":"preferred prompt","content":[{"type":"text","text":"ignored prompt"},{"type":"image_url","role":"first_frame","image_url":{"url":"https://example.com/frame.jpg"}}]`,
			wantContent: `[{"type":"text","text":"preferred prompt"},{"type":"image_url","role":"first_frame","image_url":{"url":"https://example.com/frame.jpg"}}]`,
		},
		{
			name:        "prompt with content media takes precedence over legacy images",
			fields:      `"prompt":"build a cabin","content":[{"type":"image_url","role":"first_frame","image_url":{"url":"https://example.com/frame.jpg"}}],"images":[{"url":"https://example.com/ignored.jpg","role":"first_frame"}]`,
			wantContent: `[{"type":"text","text":"build a cabin"},{"type":"image_url","role":"first_frame","image_url":{"url":"https://example.com/frame.jpg"}}]`,
		},
		{
			name:      "empty text remains invalid",
			fields:    `"content":[{"type":"text","text":"  "}]`,
			wantError: "prompt is required",
		},
		{
			name:      "multiple text prompts remain invalid",
			fields:    `"content":[{"type":"text","text":"first"},{"type":"text","text":"second"}]`,
			wantError: "second text prompt",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", "/v1/video/generations", strings.NewReader(
				`{"model":"doubao/seedance-2.0-fast","duration":5,"size":"16:9",`+tt.fields+`}`,
			))
			c.Request.Header.Set("Content-Type", "application/json")
			storage, err := common.GetBodyStorage(c)
			require.NoError(t, err)
			t.Cleanup(func() { _ = storage.Close() })
			info := &relaycommon.RelayInfo{
				OriginModelName: "doubao/seedance-2.0-fast",
				TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
				ChannelMeta:     &relaycommon.ChannelMeta{ChannelBaseUrl: "https://ai.artsapi.com"},
			}
			info.UpstreamModelName = "artsdance-2-0-fast-260801"
			info.IsModelMapped = true
			adaptor := &TaskAdaptor{}
			adaptor.Init(info)
			taskErr := adaptor.ValidateRequestAndSetAction(c, info)
			if tt.wantError != "" {
				require.NotNil(t, taskErr)
				assert.Equal(t, 400, taskErr.StatusCode)
				assert.True(t, taskErr.LocalError)
				assert.Contains(t, taskErr.Message, tt.wantError)
				return
			}
			require.Nil(t, taskErr)
			body, err := adaptor.BuildRequestBody(c, info)
			require.NoError(t, err)
			var payload map[string]any
			require.NoError(t, common.DecodeJson(body, &payload))
			content, err := common.Marshal(payload["content"])
			require.NoError(t, err)
			assert.JSONEq(t, tt.wantContent, string(content))
			assert.Equal(t, "artsdance-2-0-fast-260801", payload["model"])
			assert.Equal(t, "16:9", payload["ratio"])
			assert.Equal(t, float64(5), payload["duration"])
			assert.NotContains(t, payload, "prompt")
			assert.NotContains(t, payload, "images")
			assert.NotContains(t, payload, "size")
		})
	}
}
