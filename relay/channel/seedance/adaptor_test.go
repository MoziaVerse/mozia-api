package seedance

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayhelper "github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildRequestBodyUsesConfiguredModelMapping(t *testing.T) {
	adaptor, c, info := prepareRequest(t, `{
		"model":"cool:seedance_2_720p",
		"prompt":"camera moves forward",
		"duration":5,
		"aspect_ratio":"16:9",
		"image":"https://example.com/reference.jpg",
		"resolution":"720p",
		"metadata":{"model":"must-not-bypass-mapping","quality":"hd","watermark":false,"duration":999,"async":false}
	}`)
	c.Set("model_mapping", `{"cool:seedance_2_720p":"provider-video-model-v2"}`)
	require.NoError(t, relayhelper.ModelMappedHelper(c, info, nil))
	require.True(t, info.IsModelMapped)

	payload := buildPayload(t, adaptor, c, info)

	assert.Equal(t, "provider-video-model-v2", payload["model"])
	assert.Equal(t, float64(5), payload["duration"])
	assert.Equal(t, true, payload["async"])
	assert.Equal(t, "16:9", payload["aspect_ratio"])
	assert.Equal(t, "https://example.com/reference.jpg", payload["image"])
	assert.Equal(t, "720p", payload["resolution"])
	assert.Equal(t, "hd", payload["quality"])
	assert.Equal(t, false, payload["watermark"])
}

func TestBuildRequestBodyForwardsOriginModelWithoutMapping(t *testing.T) {
	adaptor, c, info := prepareRequest(t, `{
		"model":"provider-native-seedance",
		"prompt":"hello",
		"duration":7
	}`)
	info.OriginModelName = "provider-native-seedance"
	info.UpstreamModelName = info.OriginModelName

	payload := buildPayload(t, adaptor, c, info)

	assert.Equal(t, "provider-native-seedance", payload["model"])
	assert.Equal(t, float64(7), payload["duration"])
}

func TestBuildRequestBodyMapsSizeForProvider(t *testing.T) {
	t.Run("MiniMax H3 keeps exact size", func(t *testing.T) {
		adaptor, c, info := prepareRequest(t, `{
			"model":"minimax/minimax-h3-fl2va",
			"prompt":"camera moves forward",
			"duration":5,
			"size":"1024x576"
		}`)
		info.OriginModelName = "minimax-h3-fl2va-int8"
		info.UpstreamModelName = info.OriginModelName

		payload := buildPayload(t, adaptor, c, info)

		assert.Equal(t, "1024x576", payload["size"])
		assert.NotContains(t, payload, "ratio")
	})

	t.Run("MiniMax H3 accepts colon size separator", func(t *testing.T) {
		adaptor, c, info := prepareRequest(t, `{
			"model":"minimax/minimax-h3-fl2va",
			"prompt":"camera moves forward",
			"duration":5,
			"size":"768:448"
		}`)
		info.OriginModelName = "minimax-h3-fl2va-int8"
		info.UpstreamModelName = info.OriginModelName

		payload := buildPayload(t, adaptor, c, info)

		assert.Equal(t, "768x448", payload["size"])
		assert.NotContains(t, payload, "ratio")
	})

	t.Run("Seedance converts size to ratio", func(t *testing.T) {
		adaptor, c, info := prepareRequest(t, `{
			"model":"provider-native-seedance",
			"prompt":"camera moves forward",
			"duration":5,
			"size":"1280x720"
		}`)
		info.OriginModelName = "provider-native-seedance"
		info.UpstreamModelName = info.OriginModelName

		payload := buildPayload(t, adaptor, c, info)

		assert.Equal(t, "1280x720", payload["ratio"])
		assert.NotContains(t, payload, "size")
	})
}

func TestBuildRequestBodyPreservesMiniMaxH3ReferenceImageURL(t *testing.T) {
	imageURL := "https://dimg04.c-ctrip.com/images/reference.jpg?proc=autoorient"
	adaptor, c, info := prepareRequest(t, `{
		"model":"minimax/minimax-h3-ref2va",
		"prompt":"animate this image",
		"duration":5,
		"images":["`+imageURL+`"]
	}`)
	info.OriginModelName = "minimax-h3-ref2va-int8"
	info.UpstreamModelName = info.OriginModelName

	payload := buildPayload(t, adaptor, c, info)
	images, ok := payload["images"].([]any)
	require.True(t, ok)
	require.Len(t, images, 1)
	assert.Equal(t, imageURL, images[0])
}

func TestValidateRequestRequiresExplicitPositiveDuration(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "missing", body: `{"model":"m","prompt":"hello"}`},
		{name: "zero", body: `{"model":"m","prompt":"hello","duration":0}`},
		{name: "negative", body: `{"model":"m","prompt":"hello","duration":-5}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			adaptor, c, info := prepareRequestWithoutValidation(t, tc.body)
			taskErr := adaptor.ValidateRequestAndSetAction(c, info)
			require.NotNil(t, taskErr)
			assert.Equal(t, "invalid_duration", taskErr.Code)
			assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
		})
	}
}

func TestEstimateBillingMultipliesByRequestedSeconds(t *testing.T) {
	adaptor, c, info := prepareRequest(t, `{
		"model":"cool:seedance_2_720p",
		"prompt":"hello",
		"duration":15
	}`)

	ratios := adaptor.EstimateBilling(c, info)

	assert.Equal(t, map[string]float64{"duration": 15}, ratios)
}

func TestBuildRequestURLAcceptsRootOrV1BaseURL(t *testing.T) {
	tests := []struct {
		base string
		want string
	}{
		{base: "https://provider.example", want: "https://provider.example/v1/video/generations"},
		{base: "https://provider.example/v1", want: "https://provider.example/v1/video/generations"},
		{base: "https://provider.example/v1/", want: "https://provider.example/v1/video/generations"},
	}
	for _, tc := range tests {
		t.Run(tc.base, func(t *testing.T) {
			adaptor := &TaskAdaptor{}
			adaptor.Init(&relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: tc.base}})
			got, err := adaptor.BuildRequestURL(nil)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestSeedanceVideosBuildRequestURLUsesVideosResource(t *testing.T) {
	for _, baseURL := range []string{"https://provider.example", "https://provider.example/v1/"} {
		t.Run(baseURL, func(t *testing.T) {
			adaptor := NewSeedanceVideosTaskAdaptor()
			adaptor.Init(&relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: baseURL}})

			got, err := adaptor.BuildRequestURL(nil)

			require.NoError(t, err)
			assert.Equal(t, "https://provider.example/v1/videos", got)
			assert.Equal(t, "seedance-compatible-videos", adaptor.GetChannelName())
		})
	}
}

func TestTaskAdaptorsPollTheirConfiguredResource(t *testing.T) {
	tests := []struct {
		name    string
		adaptor *TaskAdaptor
		want    string
	}{
		{name: "seedance compatible", adaptor: &TaskAdaptor{}, want: "/v1/video/generations/upstream-task"},
		{name: "seedance compatible videos", adaptor: NewSeedanceVideosTaskAdaptor(), want: "/v1/videos/upstream-task"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			service.InitHttpClient()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, tc.want, r.URL.Path)
				assert.Equal(t, "Bearer masked-key", r.Header.Get("Authorization"))
				_, _ = w.Write([]byte(`{"status":"running"}`))
			}))
			t.Cleanup(server.Close)
			tc.adaptor.Init(&relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: server.URL}})

			resp, err := tc.adaptor.FetchTask(server.URL+"/v1", "masked-key", map[string]any{"task_id": "upstream-task"}, "")

			require.NoError(t, err)
			require.NotNil(t, resp)
			_ = resp.Body.Close()
		})
	}
}

func TestBuildRequestHeaderUsesStablePublicTaskID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	req := httptest.NewRequest(http.MethodPost, "https://provider.example/v1/video/generations", nil)
	info := &relaycommon.RelayInfo{
		TaskRelayInfo: &relaycommon.TaskRelayInfo{PublicTaskID: "task_public"},
		ChannelMeta:   &relaycommon.ChannelMeta{ApiKey: "masked-key"},
	}

	err := (&TaskAdaptor{}).BuildRequestHeader(c, req, info)

	require.NoError(t, err)
	assert.Equal(t, "Bearer masked-key", req.Header.Get("Authorization"))
	assert.Equal(t, "task_public", req.Header.Get("Idempotency-Key"))
}

func TestDoResponseAcceptsTaskIDOrID(t *testing.T) {
	for _, body := range []string{
		`{"task_id":"upstream-task","status":"queued"}`,
		`{"id":"upstream-task","status":"queued"}`,
	} {
		t.Run(body, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			writer := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(writer)
			resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}
			info := &relaycommon.RelayInfo{
				TaskRelayInfo:   &relaycommon.TaskRelayInfo{PublicTaskID: "task_public"},
				ChannelMeta:     &relaycommon.ChannelMeta{},
				OriginModelName: "public-model",
			}

			taskID, _, taskErr := (&TaskAdaptor{}).DoResponse(c, resp, info)

			require.Nil(t, taskErr)
			assert.Equal(t, "upstream-task", taskID)
			assert.Equal(t, http.StatusOK, writer.Code)
			assert.Contains(t, writer.Body.String(), `"id":"task_public"`)
		})
	}
}

func TestDoRequestAcceptsAsyncCreatedStatus(t *testing.T) {
	service.InitHttpClient()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/video/generations", r.URL.Path)
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"task_id":"upstream-task"}`))
	}))
	t.Cleanup(server.Close)

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	info := &relaycommon.RelayInfo{
		TaskRelayInfo: &relaycommon.TaskRelayInfo{PublicTaskID: "task_public"},
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl: server.URL,
			ApiKey:         "masked-key",
		},
	}
	adaptor := &TaskAdaptor{}
	adaptor.Init(info)

	resp, err := adaptor.DoRequest(c, info, strings.NewReader(`{"prompt":"hello"}`))

	require.NoError(t, err)
	require.NotNil(t, resp)
	t.Cleanup(func() { _ = resp.Body.Close() })
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestParseTaskResultNormalizesProviderStatuses(t *testing.T) {
	tests := []struct {
		status string
		want   model.TaskStatus
	}{
		{status: "pending", want: model.TaskStatusSubmitted},
		{status: "queued", want: model.TaskStatusQueued},
		{status: "running", want: model.TaskStatusInProgress},
		{status: "processing", want: model.TaskStatusInProgress},
		{status: "in_progress", want: model.TaskStatusInProgress},
	}
	for _, tc := range tests {
		t.Run(tc.status, func(t *testing.T) {
			got, err := (&TaskAdaptor{}).ParseTaskResult([]byte(`{"status":"` + tc.status + `","progress":30}`))
			require.NoError(t, err)
			assert.Equal(t, tc.want, model.TaskStatus(got.Status))
			assert.Equal(t, "30%", got.Progress)
		})
	}
}

func TestParseTaskResultSupportsCommonSuccessURLShapes(t *testing.T) {
	tests := []string{
		`{"status":"succeeded","result":{"data":[{"url":"https://cdn.example/result.mp4"}]}}`,
		`{"status":"completed","result":{"url":"https://cdn.example/result.mp4"}}`,
		`{"status":"success","data":[{"url":"https://cdn.example/result.mp4"}]}`,
		`{"status":"done","output":["https://cdn.example/result.mp4"]}`,
		`{"status":"done","video_url":"https://cdn.example/result.mp4"}`,
	}
	for _, body := range tests {
		t.Run(body, func(t *testing.T) {
			got, err := (&TaskAdaptor{}).ParseTaskResult([]byte(body))
			require.NoError(t, err)
			assert.Equal(t, string(model.TaskStatusSuccess), got.Status)
			assert.Equal(t, "https://cdn.example/result.mp4", got.Url)
		})
	}
}

func TestParseTaskResultAllowsContentEndpointFallback(t *testing.T) {
	tests := []string{
		`{"id":"upstream-task","status":"completed"}`,
		`{"id":"upstream-task","status":"completed","output":{"filename":"result.mp4","subfolder":"","type":"output"}}`,
	}
	for _, body := range tests {
		t.Run(body, func(t *testing.T) {
			got, err := NewSeedanceVideosTaskAdaptor().ParseTaskResult([]byte(body))

			require.NoError(t, err)
			assert.Equal(t, string(model.TaskStatusSuccess), got.Status)
			assert.Equal(t, "upstream-task", got.TaskID)
			assert.Empty(t, got.Url)
		})
	}
}

func TestSeedanceVideosBillingMatchesSeedanceGen(t *testing.T) {
	seedanceAdaptor, c, info := prepareRequest(t, `{"model":"video-1","prompt":"hello","duration":5}`)

	assert.Equal(t, seedanceAdaptor.EstimateBilling(c, info), NewSeedanceVideosTaskAdaptor().EstimateBilling(c, info))
}

func TestParseTaskResultTreatsTerminalErrorsAsFailure(t *testing.T) {
	for _, status := range []string{"failed", "error", "timeout", "timed_out", "expired", "cancelled", "canceled"} {
		t.Run(status, func(t *testing.T) {
			body := `{"status":"` + status + `","error":{"message":"provider failed"}}`
			got, err := (&TaskAdaptor{}).ParseTaskResult([]byte(body))
			require.NoError(t, err)
			assert.Equal(t, string(model.TaskStatusFailure), got.Status)
			assert.Equal(t, "provider failed", got.Reason)
		})
	}
}

func prepareRequest(t *testing.T, body string) (*TaskAdaptor, *gin.Context, *relaycommon.RelayInfo) {
	t.Helper()
	adaptor, c, info := prepareRequestWithoutValidation(t, body)
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	return adaptor, c, info
}

func prepareRequestWithoutValidation(t *testing.T, body string) (*TaskAdaptor, *gin.Context, *relaycommon.RelayInfo) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	info := &relaycommon.RelayInfo{
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
		OriginModelName: "cool:seedance_2_720p",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl:    "https://provider.example",
			UpstreamModelName: "cool:seedance_2_720p",
		},
	}
	adaptor := &TaskAdaptor{}
	adaptor.Init(info)
	return adaptor, c, info
}

func buildPayload(t *testing.T, adaptor *TaskAdaptor, c *gin.Context, info *relaycommon.RelayInfo) map[string]any {
	t.Helper()
	body, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(body)
	require.NoError(t, err)
	var payload map[string]any
	require.NoError(t, common.Unmarshal(data, &payload))
	return payload
}
