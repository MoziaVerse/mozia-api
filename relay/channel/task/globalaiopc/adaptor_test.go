package globalaiopc

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateVideosModelCapabilities(t *testing.T) {
	t.Run("videos accepts documented material limits and wide ratio", func(t *testing.T) {
		adaptor, c, info := prepareRequestWithoutValidation(t, `{
			"model":"videos",
			"prompt":"use every reference",
			"duration":4,
			"ratio":"21:9",
			"referenceImages":["i1","i2","i3","i4","i5","i6","i7","i8","i9"],
			"referenceVideos":["v1","v2","v3"],
			"referenceAudios":["a1","a2","a3"]
		}`)

		require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
		assert.Equal(t, constant.TaskActionReferenceGenerate, info.Action)
	})

	t.Run("stable fast accepts its documented boundary", func(t *testing.T) {
		adaptor, c, info := prepareRequestWithoutValidation(t, `{
			"model":"videos_stable_fast",
			"prompt":"use the references",
			"duration":10,
			"ratio":"1:1",
			"referenceImages":["i1","i2","i3","i4"],
			"referenceVideos":["v1","v2","v3"],
			"referenceAudios":["a1"]
		}`)

		require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
		assert.Equal(t, constant.TaskActionReferenceGenerate, info.Action)
	})

	for _, tc := range []struct {
		name     string
		body     string
		wantCode string
	}{
		{
			name:     "stable fast rejects unsupported duration",
			body:     `{"model":"videos_stable_fast","prompt":"p","duration":5}`,
			wantCode: "invalid_duration",
		},
		{
			name:     "stable rejects duration over range",
			body:     `{"model":"videos_stable","prompt":"p","duration":16}`,
			wantCode: "invalid_duration",
		},
		{
			name:     "duration is required",
			body:     `{"model":"videos_stable","prompt":"p"}`,
			wantCode: "invalid_duration",
		},
		{
			name:     "stable rejects fifth image",
			body:     `{"model":"videos_stable","prompt":"p","duration":5,"images":["1","2","3","4","5"]}`,
			wantCode: "material_limit_exceeded",
		},
		{
			name:     "stable rejects second audio",
			body:     `{"model":"videos_stable","prompt":"p","duration":5,"audios":["1","2"]}`,
			wantCode: "material_limit_exceeded",
		},
		{
			name:     "pro rejects reference video",
			body:     `{"model":"videos_pro","prompt":"p","duration":10,"videos":["1"]}`,
			wantCode: "material_limit_exceeded",
		},
		{
			name:     "pro audio requires reference image",
			body:     `{"model":"videos_pro_fast","prompt":"p","duration":10,"audios":["1"]}`,
			wantCode: "invalid_request",
		},
		{
			name:     "videos only supports 720p",
			body:     `{"model":"videos","prompt":"p","duration":5,"resolution":"1080p"}`,
			wantCode: "invalid_size",
		},
		{
			name:     "stable only supports documented ratios",
			body:     `{"model":"videos_stable","prompt":"p","duration":5,"ratio":"4:3"}`,
			wantCode: "invalid_size",
		},
		{
			name:     "auto face is limited to videos",
			body:     `{"model":"videos_stable","prompt":"p","duration":5,"autoFace":true}`,
			wantCode: "invalid_request",
		},
		{
			name:     "frame fields must be paired",
			body:     `{"model":"videos_stable_fast","prompt":"p","duration":10,"first_image":"first"}`,
			wantCode: "invalid_request",
		},
		{
			name:     "frame mode excludes reference materials",
			body:     `{"model":"videos_stable_fast","prompt":"p","duration":10,"image":"first","lastFrameImage":"last","images":["ref"]}`,
			wantCode: "invalid_request",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			adaptor, c, info := prepareRequestWithoutValidation(t, tc.body)

			taskErr := adaptor.ValidateRequestAndSetAction(c, info)

			require.NotNil(t, taskErr)
			assert.Equal(t, tc.wantCode, taskErr.Code)
			assert.Equal(t, http.StatusBadRequest, taskErr.StatusCode)
			assert.True(t, taskErr.LocalError)
		})
	}

	t.Run("pro accepts image and three audios", func(t *testing.T) {
		adaptor, c, info := prepareRequestWithoutValidation(t, `{
			"model":"videos_pro_fast",
			"prompt":"use image and audio",
			"duration":15,
			"images":["i1"],
			"audios":["a1","a2","a3"]
		}`)

		require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
	})

	t.Run("stable prompt is limited to 5000 characters", func(t *testing.T) {
		body := `{"model":"videos_stable","duration":5,"prompt":"` + strings.Repeat("a", 5001) + `"}`
		adaptor, c, info := prepareRequestWithoutValidation(t, body)

		taskErr := adaptor.ValidateRequestAndSetAction(c, info)

		require.NotNil(t, taskErr)
		assert.Equal(t, "invalid_request", taskErr.Code)
		assert.Contains(t, taskErr.Message, "5000")
	})
}

func TestConfiguredModelMappingControlsVideosModel(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		mapping   string
		wantModel string
	}{
		{
			name:      "public stable fast alias",
			body:      `{"model":"public-fast","prompt":"p","duration":10}`,
			mapping:   `{"public-fast":"videos_stable_fast"}`,
			wantModel: "videos_stable_fast",
		},
		{
			name:      "public pro alias",
			body:      `{"model":"public-pro","prompt":"p","duration":15,"images":["i1"],"audios":["a1"]}`,
			mapping:   `{"public-pro":"videos_pro"}`,
			wantModel: "videos_pro",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			adaptor, c, info := prepareRequestWithoutValidation(t, tc.body)
			c.Set("model_mapping", tc.mapping)

			require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
			assert.Equal(t, tc.wantModel, info.UpstreamModelName)

			requestURL, err := adaptor.BuildRequestURL(info)
			require.NoError(t, err)
			assert.Equal(t, "https://provider.example/v1/videos/videos", requestURL)

			payload := buildPayload(t, adaptor, c, info)
			assert.Equal(t, tc.wantModel, payload["model"])
		})
	}
}

func TestModelCenterContract(t *testing.T) {
	for _, tc := range []struct {
		name      string
		body      string
		mapping   string
		wantModel string
		wantField string
		wantValue any
	}{
		{"sd discount", `{"model":"sd_2.0_discount","prompt":"cinematic shot","duration":5,"size":"16:9","resolution":"480p","images":["https://example.com/ref.png"]}`, "", "sd_2.0_discount", "reference_images", []any{"https://example.com/ref.png"}},
		{"minimax mapping", `{"model":"MiniMax-H3","prompt":"cinematic shot","duration":5,"resolution":"2k","aspect_ratio":"16:9","reference_images":["https://example.com/ref.png"],"reference_audios":["https://example.com/ref.mp3"]}`, `{"MiniMax-H3":"minimax-h3"}`, "minimax-h3", "reference_audios", []any{"https://example.com/ref.mp3"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			adaptor := NewModelCenterTaskAdaptor()
			_, c, info := prepareRequestWithoutValidation(t, tc.body)
			if tc.mapping != "" {
				c.Set("model_mapping", tc.mapping)
			}
			info.ChannelMeta.ChannelBaseUrl = "https://zcbservice.aizfw.cn/kyyReactApiServer"
			adaptor.Init(info)

			require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
			requestURL, err := adaptor.BuildRequestURL(info)
			require.NoError(t, err)
			assert.Equal(t, "https://zcbservice.aizfw.cn/kyyReactApiServer/v2/model-center/tasks", requestURL)
			body := buildPayload(t, adaptor, c, info)
			assert.Equal(t, tc.wantModel, body["model"])
			assert.Equal(t, tc.wantValue, body[tc.wantField])
			assert.Nil(t, body["referenceImages"])
		})
	}
}

func TestValidateRejectsUnsupportedMappedModel(t *testing.T) {
	adaptor, c, info := prepareRequestWithoutValidation(t, `{
		"model":"public-special",
		"prompt":"p",
		"duration":10
	}`)
	c.Set("model_mapping", `{"public-special":"sd_2.0_fast_special_720p"}`)

	taskErr := adaptor.ValidateRequestAndSetAction(c, info)

	require.NotNil(t, taskErr)
	assert.Equal(t, "invalid_model", taskErr.Code)
	assert.Contains(t, taskErr.Message, "configure model mapping")
}

func TestBuildRequestBodyUsesVideosContractForFlatMultimodalRequest(t *testing.T) {
	adaptor, c, info := prepareRequestWithoutValidation(t, `{
		"model":"public-videos-fast",
		"prompt":"Use image one, video one, and audio one",
		"duration":10,
		"size":"16:9",
		"resolution":"720p",
		"images":["https://example.com/reference.jpg"],
		"videos":["https://example.com/reference.mp4"],
		"audios":["https://example.com/reference.mp3"]
	}`)
	c.Set("model_mapping", `{"public-videos-fast":"videos_stable_fast"}`)
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))

	payload := buildPayload(t, adaptor, c, info)

	assert.Equal(t, "videos_stable_fast", payload["model"])
	assert.Equal(t, "Use image one, video one, and audio one", payload["prompt"])
	assert.Equal(t, float64(10), payload["duration"])
	assert.Equal(t, "16:9", payload["ratio"])
	assert.Equal(t, "720p", payload["resolution"])
	assert.Equal(t, []any{"https://example.com/reference.jpg"}, payload["referenceImages"])
	assert.Equal(t, []any{"https://example.com/reference.mp4"}, payload["referenceVideos"])
	assert.Equal(t, []any{"https://example.com/reference.mp3"}, payload["referenceAudios"])
	assert.NotContains(t, payload, "content")
	assert.NotContains(t, payload, "images")
	assert.NotContains(t, payload, "videos")
	assert.NotContains(t, payload, "audios")
}

func TestBuildRequestBodyMapsVideosAliases(t *testing.T) {
	adaptor, c, info := prepareRequest(t, `{
		"model":"videos",
		"prompt":"map historical URL aliases",
		"seconds":"8",
		"aspect_ratio":"9:16",
		"size":"720p",
		"image_urls":["https://example.com/reference.jpg"],
		"video_urls":["https://example.com/reference.mp4"],
		"audio_urls":["https://example.com/reference.mp3"],
		"autoFace":false
	}`)

	payload := buildPayload(t, adaptor, c, info)

	assert.Equal(t, float64(8), payload["duration"])
	assert.Equal(t, "9:16", payload["ratio"])
	assert.Equal(t, "720p", payload["resolution"])
	assert.Equal(t, []any{"https://example.com/reference.jpg"}, payload["referenceImages"])
	assert.Equal(t, []any{"https://example.com/reference.mp4"}, payload["referenceVideos"])
	assert.Equal(t, []any{"https://example.com/reference.mp3"}, payload["referenceAudios"])
	assert.Equal(t, false, payload["autoFace"])
	assert.NotContains(t, payload, "seconds")
	assert.NotContains(t, payload, "aspect_ratio")
	assert.NotContains(t, payload, "image_urls")
	assert.NotContains(t, payload, "video_urls")
	assert.NotContains(t, payload, "audio_urls")
}

func TestBuildRequestBodyMapsSinglePublicImage(t *testing.T) {
	adaptor, c, info := prepareRequest(t, `{
		"model":"videos_stable_fast",
		"prompt":"map the existing singular image field",
		"duration":10,
		"image":"https://example.com/reference.jpg"
	}`)

	payload := buildPayload(t, adaptor, c, info)

	assert.Equal(t, []any{"https://example.com/reference.jpg"}, payload["referenceImages"])
	assert.NotContains(t, payload, "first_image")
	assert.NotContains(t, payload, "image")
}

func TestBuildRequestBodyMapsLegacyContentToVideosFields(t *testing.T) {
	adaptor, c, info := prepareRequestWithoutValidation(t, `{
		"model":"public-videos",
		"duration":8,
		"ratio":"9:16",
		"content":[
			{"type":"text","text":"Keep the subject and rhythm"},
			{"type":"image_url","role":"reference_image","image_url":{"url":"https://example.com/reference.jpg"}},
			{"type":"video_url","role":"reference_video","video_url":{"url":"https://example.com/reference.mp4"}},
			{"type":"audio_url","role":"reference_audio","audio_url":{"url":"https://example.com/reference.mp3"}}
		]
	}`)
	c.Set("model_mapping", `{"public-videos":"videos"}`)
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))

	payload := buildPayload(t, adaptor, c, info)

	assert.Equal(t, "videos", payload["model"])
	assert.Equal(t, "Keep the subject and rhythm", payload["prompt"])
	assert.Equal(t, []any{"https://example.com/reference.jpg"}, payload["referenceImages"])
	assert.Equal(t, []any{"https://example.com/reference.mp4"}, payload["referenceVideos"])
	assert.Equal(t, []any{"https://example.com/reference.mp3"}, payload["referenceAudios"])
	assert.NotContains(t, payload, "content")
}

func TestBuildRequestBodyMapsFrameAliasesToVideosFields(t *testing.T) {
	adaptor, c, info := prepareRequestWithoutValidation(t, `{
		"model":"public-videos-fast",
		"prompt":"Create a smooth transition",
		"duration":10,
		"image":"https://example.com/first.jpg",
		"lastFrameImage":"https://example.com/last.jpg"
	}`)
	c.Set("model_mapping", `{"public-videos-fast":"videos_stable_fast"}`)
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))

	payload := buildPayload(t, adaptor, c, info)

	assert.Equal(t, "https://example.com/first.jpg", payload["first_image"])
	assert.Equal(t, "https://example.com/last.jpg", payload["last_image"])
	assert.NotContains(t, payload, "image")
	assert.NotContains(t, payload, "lastFrameImage")
	assert.NotContains(t, payload, "content")
}

func TestBuildRequestHeaderAndURL(t *testing.T) {
	adaptor := &TaskAdaptor{baseURL: "https://provider.example"}
	got, err := adaptor.BuildRequestURL(nil)
	require.NoError(t, err)
	assert.Equal(t, "https://provider.example/v1/videos/videos", got)

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	req := httptest.NewRequest(http.MethodPost, got, nil)
	info := testRelayInfo("https://provider.example", "videos_stable_fast")
	require.NoError(t, adaptor.BuildRequestHeader(c, req, info))
	assert.Equal(t, "Bearer test-key", req.Header.Get("Authorization"))
	assert.Equal(t, "application/json", req.Header.Get("Content-Type"))
	assert.Equal(t, "task_public", req.Header.Get("Idempotency-Key"))
}

func TestBuildRequestHeaderPreservesClientIdempotencyKeyAcrossAttempts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	c.Request.Header.Set("Idempotency-Key", "client-request-123")
	info := testRelayInfo("https://provider.example", "videos_stable_fast")

	for range 2 {
		req := httptest.NewRequest(http.MethodPost, "https://provider.example/v1/videos/videos", nil)
		require.NoError(t, (&TaskAdaptor{}).BuildRequestHeader(c, req, info))
		assert.Equal(t, "client-request-123", req.Header.Get("Idempotency-Key"))
	}
}

func TestEstimateBillingUsesBaseNoop(t *testing.T) {
	adaptor, c, info := prepareRequest(t, `{
		"model":"videos_stable_fast",
		"prompt":"p",
		"duration":10
	}`)

	require.Nil(t, adaptor.EstimateBilling(c, info))
}

func TestDoRequestNormalizesAcceptedStatus(t *testing.T) {
	service.InitHttpClient()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/videos/videos", r.URL.Path)
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"id":"vendor_task"}`))
	}))
	t.Cleanup(server.Close)

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	info := testRelayInfo(server.URL, "videos_stable_fast")
	adaptor := &TaskAdaptor{}
	adaptor.Init(info)

	resp, err := adaptor.DoRequest(c, info, strings.NewReader(`{"model":"videos_stable_fast","prompt":"p","duration":10}`))
	require.NoError(t, err)
	require.NotNil(t, resp)
	t.Cleanup(func() { _ = resp.Body.Close() })
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestDoResponseReadsFlatTaskIDAndHidesItFromClient(t *testing.T) {
	gin.SetMode(gin.TestMode)
	writer := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(writer)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(`{
			"id":"videos_vendor_task_123",
			"status":"queued"
		}`)),
	}
	info := testRelayInfo("https://provider.example", "videos_stable_fast")

	taskID, taskData, taskErr := (&TaskAdaptor{}).DoResponse(c, resp, info)

	require.Nil(t, taskErr)
	assert.Equal(t, "videos_vendor_task_123", taskID)
	assert.Contains(t, string(taskData), "videos_vendor_task_123")
	assert.Contains(t, writer.Body.String(), `"id":"task_public"`)
	assert.NotContains(t, writer.Body.String(), "videos_vendor_task_123")
}

func TestDoResponseTreatsFailedStatusAsSubmitFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	writer := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(writer)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(`{
			"id":"videos_vendor_task_123",
			"status":"failed",
			"error":"provider rejected prompt"
		}`)),
	}
	info := testRelayInfo("https://provider.example", "videos_stable_fast")

	taskID, taskData, taskErr := (&TaskAdaptor{}).DoResponse(c, resp, info)

	require.NotNil(t, taskErr)
	assert.Empty(t, taskID)
	assert.Nil(t, taskData)
	assert.Equal(t, "task_submit_failed", taskErr.Code)
}

func TestFetchTaskUsesResultPath(t *testing.T) {
	service.InitHttpClient()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/result/videos_vendor_task_123", r.URL.Path)
		assert.Equal(t, "Bearer poll-key", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"id":"videos_vendor_task_123","status":"processing"}`))
	}))
	t.Cleanup(server.Close)

	resp, err := (&TaskAdaptor{}).FetchTask(
		server.URL+"/v1",
		"poll-key",
		map[string]any{"task_id": "videos_vendor_task_123"},
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
		{status: "queued", want: model.TaskStatusQueued},
		{status: "processing", want: model.TaskStatusInProgress},
		{status: "failed", want: model.TaskStatusFailure},
	}
	for _, test := range statusTests {
		t.Run(test.status, func(t *testing.T) {
			body := `{"id":"t","status":"` + test.status + `","error":"provider message"}`
			got, err := (&TaskAdaptor{}).ParseTaskResult([]byte(body))
			require.NoError(t, err)
			assert.Equal(t, test.want, model.TaskStatus(got.Status))
			if test.want == model.TaskStatusFailure {
				assert.Equal(t, "provider message", got.Reason)
			}
		})
	}

	got, err := (&TaskAdaptor{}).ParseTaskResult([]byte(`{
		"id":"t",
		"status":"completed",
		"video_url":"https://cdn.example/1.mp4",
		"actualDuration":10,
		"amount":5
	}`))
	require.NoError(t, err)
	assert.Equal(t, string(model.TaskStatusSuccess), got.Status)
	assert.Equal(t, "https://cdn.example/1.mp4", got.Url)
}

func TestParseTaskResultRejectsUnknownStatusAndMissingSuccessURL(t *testing.T) {
	_, err := (&TaskAdaptor{}).ParseTaskResult([]byte(`{"id":"t","status":"new_state"}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown GlobalAiOpc task status")

	_, err = (&TaskAdaptor{}).ParseTaskResult([]byte(`{"id":"t","status":"completed"}`))
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
			OriginModelName: "public-videos-fast",
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
	info := testRelayInfo("https://provider.example", "videos_stable_fast")
	adaptor := &TaskAdaptor{}
	adaptor.Init(info)
	return adaptor, c, info
}

func testRelayInfo(baseURL, modelName string) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{PublicTaskID: "task_public"},
		OriginModelName: modelName,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl:    baseURL,
			ApiKey:            "test-key",
			UpstreamModelName: modelName,
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
