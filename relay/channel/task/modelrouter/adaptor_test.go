package modelrouter

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayhelper "github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidate431MaterialLimits(t *testing.T) {
	valid := `{
		"model":"seedance-public",
		"prompt":"use every reference",
		"images":["i1","i2","i3","i4"],
		"videos":["v1","v2","v3"],
		"audios":["a1"]
	}`
	adaptor, c, info := prepareRequestWithoutValidation(t, valid)
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))

	tests := []struct {
		name string
		body string
	}{
		{
			name: "five flat images",
			body: `{"model":"seedance-public","prompt":"p","images":["1","2","3","4","5"]}`,
		},
		{
			name: "four flat videos",
			body: `{"model":"seedance-public","prompt":"p","videos":["1","2","3","4"]}`,
		},
		{
			name: "two flat audios",
			body: `{"model":"seedance-public","prompt":"p","audios":["1","2"]}`,
		},
		{
			name: "five native images",
			body: `{"model":"vendor-seedance-2","input":{"prompt":"p","image_urls":["1","2","3","4","5"]}}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			adaptor, c, info := prepareRequestWithoutValidation(t, tc.body)
			taskErr := adaptor.ValidateRequestAndSetAction(c, info)
			require.NotNil(t, taskErr)
			assert.Equal(t, "material_limit_exceeded", taskErr.Code)
			assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
			assert.True(t, taskErr.LocalError)
		})
	}
}

func TestValidate431UsesMappedSeedanceModelForNativeEnvelope(t *testing.T) {
	adaptor, c, info := prepareRequestWithoutValidation(t, `{
		"model":"public-video",
		"input":{"prompt":"p","audio_urls":["1","2"]}
	}`)
	c.Set("model_mapping", `{"public-video":"marketplace/seedance-2.0-431"}`)

	taskErr := adaptor.ValidateRequestAndSetAction(c, info)

	require.NotNil(t, taskErr)
	assert.Equal(t, "material_limit_exceeded", taskErr.Code)
}

func TestValidateRejectsExplicitInvalidDuration(t *testing.T) {
	for _, body := range []string{
		`{"model":"seedance-public","prompt":"p","duration":0}`,
		`{"model":"seedance-public","prompt":"p","duration":-1}`,
		`{"model":"seedance-public","input":{"prompt":"p"},"parameters":{"duration":1.5}}`,
	} {
		t.Run(body, func(t *testing.T) {
			adaptor, c, info := prepareRequestWithoutValidation(t, body)
			taskErr := adaptor.ValidateRequestAndSetAction(c, info)
			require.NotNil(t, taskErr)
			assert.Equal(t, "invalid_duration", taskErr.Code)
			assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
			assert.True(t, taskErr.LocalError)
		})
	}
}

func TestValidateRejectsUnsupportedFlatSize(t *testing.T) {
	adaptor, c, info := prepareRequestWithoutValidation(t, `{
		"model":"seedance-public",
		"prompt":"p",
		"size":"123x456"
	}`)

	taskErr := adaptor.ValidateRequestAndSetAction(c, info)

	require.NotNil(t, taskErr)
	assert.Equal(t, "invalid_size", taskErr.Code)
	assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
	assert.True(t, taskErr.LocalError)
}

func TestBuildRequestBodyUsesMappedModelAndPreservesNativeEnvelope(t *testing.T) {
	adaptor, c, info := prepareRequest(t, `{
		"model":"seedance-public",
		"callback_url":"https://example.com/hook",
		"input":{
			"prompt":"cinematic sunrise",
			"image_urls":["https://example.com/1.jpg"],
			"watermark":false,
			"seed":0,
			"vendor_extension":{"enabled":true}
		},
		"parameters":{"duration":8,"prompt_extend":false}
	}`)
	c.Set("model_mapping", `{"seedance-public":"marketplace/seedance-2.0-431"}`)
	require.NoError(t, relayhelper.ModelMappedHelper(c, info, nil))

	payload := buildPayload(t, adaptor, c, info)

	assert.Equal(t, "marketplace/seedance-2.0-431", payload["model"])
	assert.Equal(t, "https://example.com/hook", payload["callback_url"])
	input := requireObject(t, payload, "input")
	assert.Equal(t, false, input["watermark"])
	assert.Equal(t, float64(0), input["seed"])
	assert.Equal(t, map[string]any{"enabled": true}, input["vendor_extension"])
	parameters := requireObject(t, payload, "parameters")
	assert.Equal(t, false, parameters["prompt_extend"])
}

func TestBuildRequestBodyNormalizesFlat431Request(t *testing.T) {
	adaptor, c, info := prepareRequest(t, `{
		"model":"seedance-public",
		"prompt":"follow image one and video one",
		"images":["https://example.com/i1.jpg","https://example.com/i2.jpg"],
		"videos":["https://example.com/v1.mp4"],
		"audios":["https://example.com/a1.mp3"],
		"duration":8,
		"resolution":"720p",
		"generate_audio":false,
		"callback_url":"https://example.com/hook",
		"metadata":{"watermark":false,"seed":0,"model":"must-not-override"},
		"vendor_top_level":"kept"
	}`)
	c.Set("model_mapping", `{"seedance-public":"marketplace/seedance-2.0-431"}`)
	require.NoError(t, relayhelper.ModelMappedHelper(c, info, nil))

	payload := buildPayload(t, adaptor, c, info)

	assert.Equal(t, "marketplace/seedance-2.0-431", payload["model"])
	assert.Equal(t, "https://example.com/hook", payload["callback_url"])
	assert.Equal(t, "kept", payload["vendor_top_level"])
	assert.NotContains(t, payload, "images")
	assert.NotContains(t, payload, "metadata")
	input := requireObject(t, payload, "input")
	assert.Equal(t, "reference-to-video", input["generation_type"])
	assert.Equal(t, "follow image one and video one", input["prompt"])
	assert.Equal(t, []any{"https://example.com/i1.jpg", "https://example.com/i2.jpg"}, input["image_urls"])
	assert.Equal(t, []any{"https://example.com/v1.mp4"}, input["video_urls"])
	assert.Equal(t, []any{"https://example.com/a1.mp3"}, input["audio_urls"])
	assert.Equal(t, float64(8), input["duration"])
	assert.Equal(t, false, input["generate_audio"])
	assert.Equal(t, false, input["watermark"])
	assert.Equal(t, float64(0), input["seed"])
}

func TestBuildRequestBodySplitsOpenAIVideoSize(t *testing.T) {
	adaptor, c, info := prepareRequest(t, `{
		"model":"seedance-public",
		"prompt":"landscape shot",
		"size":"1280x720"
	}`)

	payload := buildPayload(t, adaptor, c, info)
	input := requireObject(t, payload, "input")

	assert.Equal(t, "720p", input["resolution"])
	assert.Equal(t, "16:9", input["aspect_ratio"])
	assert.NotContains(t, payload, "size")
}

func TestEstimateBillingUsesSeedanceDuration(t *testing.T) {
	adaptor, c, info := prepareRequest(t, `{
		"model":"seedance-public",
		"input":{"prompt":"p","duration":12}
	}`)
	info.UpstreamModelName = "marketplace/seedance-2.0-431"

	assert.Equal(t, map[string]float64{"duration": 12}, adaptor.EstimateBilling(c, info))

	defaultAdaptor, defaultContext, defaultInfo := prepareRequest(t, `{
		"model":"seedance-public",
		"prompt":"p"
	}`)
	defaultInfo.UpstreamModelName = "marketplace/seedance-2.0-431"
	assert.Equal(t, map[string]float64{"duration": 5}, defaultAdaptor.EstimateBilling(defaultContext, defaultInfo))
}

func TestBuildRequestURLAndHeader(t *testing.T) {
	for _, test := range []struct {
		baseURL string
		want    string
	}{
		{baseURL: "https://provider.example", want: "https://provider.example/v1/videos/generations"},
		{baseURL: "https://provider.example/v1/", want: "https://provider.example/v1/videos/generations"},
	} {
		t.Run(test.baseURL, func(t *testing.T) {
			adaptor := &TaskAdaptor{}
			info := testRelayInfo(test.baseURL)
			adaptor.Init(info)
			got, err := adaptor.BuildRequestURL(info)
			require.NoError(t, err)
			assert.Equal(t, test.want, got)
		})
	}

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	req := httptest.NewRequest(http.MethodPost, "https://provider.example/v1/videos/generations", nil)
	info := testRelayInfo("https://provider.example")
	require.NoError(t, (&TaskAdaptor{}).BuildRequestHeader(c, req, info))
	assert.Equal(t, "Bearer test-key", req.Header.Get("Authorization"))
	assert.Equal(t, "application/json", req.Header.Get("Content-Type"))
	assert.Equal(t, "true", req.Header.Get("X-MR-Async"))
	assert.Equal(t, "task_public", req.Header.Get("Idempotency-Key"))
}

func TestBuildRequestHeaderPreservesClientIdempotencyKeyAcrossAttempts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	c.Request.Header.Set("Idempotency-Key", "client-request-123")
	info := testRelayInfo("https://provider.example")

	for range 2 {
		req := httptest.NewRequest(http.MethodPost, "https://provider.example/v1/videos/generations", nil)
		require.NoError(t, (&TaskAdaptor{}).BuildRequestHeader(c, req, info))
		assert.Equal(t, "client-request-123", req.Header.Get("Idempotency-Key"))
	}
}

func TestDoRequestNormalizesAcceptedStatus(t *testing.T) {
	service.InitHttpClient()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/videos/generations", r.URL.Path)
		assert.Equal(t, "true", r.Header.Get("X-MR-Async"))
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"output":{"task_id":"vendor_task"}}`))
	}))
	t.Cleanup(server.Close)

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	info := testRelayInfo(server.URL)
	adaptor := &TaskAdaptor{}
	adaptor.Init(info)

	resp, err := adaptor.DoRequest(c, info, strings.NewReader(`{"model":"m","input":{"prompt":"p"}}`))
	require.NoError(t, err)
	require.NotNil(t, resp)
	t.Cleanup(func() { _ = resp.Body.Close() })
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestDoResponseReadsNestedTaskIDAndHidesItFromClient(t *testing.T) {
	gin.SetMode(gin.TestMode)
	writer := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(writer)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(`{
			"request_id":"request_1",
			"output":{"task_id":"seedance_vendor_task_123","task_status":"PENDING"}
		}`)),
	}
	info := testRelayInfo("https://provider.example")

	taskID, taskData, taskErr := (&TaskAdaptor{}).DoResponse(c, resp, info)

	require.Nil(t, taskErr)
	assert.Equal(t, "seedance_vendor_task_123", taskID)
	assert.Contains(t, string(taskData), "seedance_vendor_task_123")
	assert.Contains(t, writer.Body.String(), `"id":"task_public"`)
	assert.NotContains(t, writer.Body.String(), "seedance_vendor_task_123")
}

func TestFetchTaskPreservesFullTaskID(t *testing.T) {
	service.InitHttpClient()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/tasks/seedance_vendor_task_123", r.URL.Path)
		assert.Equal(t, "Bearer poll-key", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"output":{"task_status":"RUNNING"}}`))
	}))
	t.Cleanup(server.Close)

	resp, err := (&TaskAdaptor{}).FetchTask(
		server.URL+"/v1",
		"poll-key",
		map[string]any{"task_id": "seedance_vendor_task_123"},
		"",
	)
	require.NoError(t, err)
	require.NotNil(t, resp)
	t.Cleanup(func() { _ = resp.Body.Close() })
}

func TestParseTaskResultStatusesAndURLs(t *testing.T) {
	statusTests := []struct {
		status string
		want   model.TaskStatus
	}{
		{status: "PENDING", want: model.TaskStatusQueued},
		{status: "RUNNING", want: model.TaskStatusInProgress},
		{status: "FAILED", want: model.TaskStatusFailure},
		{status: "CANCELED", want: model.TaskStatusFailure},
		{status: "UNKNOWN", want: model.TaskStatusFailure},
	}
	for _, test := range statusTests {
		t.Run(test.status, func(t *testing.T) {
			body := `{"output":{"task_id":"t","task_status":"` + test.status + `","message":"provider message"}}`
			got, err := (&TaskAdaptor{}).ParseTaskResult([]byte(body))
			require.NoError(t, err)
			assert.Equal(t, test.want, model.TaskStatus(got.Status))
			if test.want == model.TaskStatusFailure {
				assert.Equal(t, "provider message", got.Reason)
			}
		})
	}

	urlBodies := []string{
		`{"output":{"task_status":"SUCCEEDED","video_url":"https://cdn.example/1.mp4"}}`,
		`{"output":{"task_status":"SUCCEEDED","output":{"video_url":"https://cdn.example/1.mp4"}}}`,
		`{"output":{"task_status":"SUCCEEDED","output":{"results":[{"url":"https://cdn.example/1.mp4"}]}}}`,
	}
	for _, body := range urlBodies {
		t.Run(body, func(t *testing.T) {
			got, err := (&TaskAdaptor{}).ParseTaskResult([]byte(body))
			require.NoError(t, err)
			assert.Equal(t, string(model.TaskStatusSuccess), got.Status)
			assert.Equal(t, "https://cdn.example/1.mp4", got.Url)
		})
	}
}

func TestParseTaskResultRejectsUnknownStatusAndMissingSuccessURL(t *testing.T) {
	_, err := (&TaskAdaptor{}).ParseTaskResult([]byte(`{"output":{"task_status":"NEW_STATE"}}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown ModelRouter task status")

	_, err = (&TaskAdaptor{}).ParseTaskResult([]byte(`{"output":{"task_status":"SUCCEEDED"}}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "without a video URL")
}

func TestConvertToOpenAIVideoUsesPersistedTaskState(t *testing.T) {
	task := &model.Task{
		TaskID:    "task_public",
		Status:    model.TaskStatusSuccess,
		Progress:  "100%",
		CreatedAt: 10,
		UpdatedAt: 20,
		Properties: model.Properties{
			OriginModelName: "seedance-public",
		},
		PrivateData: model.TaskPrivateData{ResultURL: "https://cdn.example/result.mp4"},
	}

	body, err := (&TaskAdaptor{}).ConvertToOpenAIVideo(task)
	require.NoError(t, err)
	var video dto.OpenAIVideo
	require.NoError(t, common.Unmarshal(body, &video))
	assert.Equal(t, dto.VideoStatusCompleted, video.Status)
	assert.Equal(t, "https://cdn.example/result.mp4", video.Metadata["url"])

	task.Status = model.TaskStatusFailure
	task.FailReason = "safety check failed"
	body, err = (&TaskAdaptor{}).ConvertToOpenAIVideo(task)
	require.NoError(t, err)
	require.NoError(t, common.Unmarshal(body, &video))
	require.NotNil(t, video.Error)
	assert.Equal(t, "task_failed", video.Error.Code)
	assert.Equal(t, "safety check failed", video.Error.Message)
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
	info := testRelayInfo("https://provider.example")
	adaptor := &TaskAdaptor{}
	adaptor.Init(info)
	return adaptor, c, info
}

func testRelayInfo(baseURL string) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{PublicTaskID: "task_public"},
		OriginModelName: "seedance-public",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl:    baseURL,
			ApiKey:            "test-key",
			UpstreamModelName: "seedance-public",
		},
	}
}

func buildPayload(t *testing.T, adaptor *TaskAdaptor, c *gin.Context, info *relaycommon.RelayInfo) map[string]any {
	t.Helper()
	body, err := adaptor.BuildRequestBody(c, info)
	require.NoError(t, err)
	data, err := io.ReadAll(body)
	require.NoError(t, err)
	payload := make(map[string]any)
	require.NoError(t, common.Unmarshal(data, &payload))
	return payload
}

func requireObject(t *testing.T, object map[string]any, key string) map[string]any {
	t.Helper()
	value, ok := object[key].(map[string]any)
	require.True(t, ok, "%s must be an object", key)
	return value
}
