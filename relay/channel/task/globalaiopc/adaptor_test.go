package globalaiopc

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
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateSpecialConstraintsAndMappedModels(t *testing.T) {
	t.Run("mapped special model rejects video on non video-ref sku", func(t *testing.T) {
		adaptor, c, info := prepareRequestWithoutValidation(t, `{
			"model":"public-special",
			"prompt":"p",
			"videos":["v1"],
			"duration":5
		}`)
		c.Set("model_mapping", `{"public-special":"sd_2.0_special_720p"}`)

		taskErr := adaptor.ValidateRequestAndSetAction(c, info)
		require.NotNil(t, taskErr)
		assert.Equal(t, "invalid_request", taskErr.Code)
	})

	t.Run("mapped special with_video_ref requires video", func(t *testing.T) {
		adaptor, c, info := prepareRequestWithoutValidation(t, `{
			"model":"public-special",
			"prompt":"p",
			"images":["i1"],
			"duration":5
		}`)
		c.Set("model_mapping", `{"public-special":"sd_2.0_fast_special_720p_with_video_ref"}`)

		taskErr := adaptor.ValidateRequestAndSetAction(c, info)
		require.NotNil(t, taskErr)
		assert.Equal(t, "invalid_request", taskErr.Code)
	})

	t.Run("special content requires text", func(t *testing.T) {
		adaptor, c, info := prepareRequestWithoutValidation(t, `{
			"model":"sd_2.0_special_720p",
			"duration":5,
			"content":[
				{"type":"image_url","role":"reference_image","image_url":{"url":"https://example.com/1.png"}}
			]
		}`)

		taskErr := adaptor.ValidateRequestAndSetAction(c, info)
		require.NotNil(t, taskErr)
		assert.Equal(t, "invalid_request", taskErr.Code)
	})

	t.Run("special content last frame requires first frame", func(t *testing.T) {
		adaptor, c, info := prepareRequestWithoutValidation(t, `{
			"model":"sd_2.0_special_720p",
			"duration":5,
			"content":[
				{"type":"text","text":"p"},
				{"type":"image_url","role":"last_frame","image_url":{"url":"https://example.com/last.png"}}
			]
		}`)

		taskErr := adaptor.ValidateRequestAndSetAction(c, info)
		require.NotNil(t, taskErr)
		assert.Equal(t, "invalid_request", taskErr.Code)
	})

	t.Run("special audio requires image or video", func(t *testing.T) {
		adaptor, c, info := prepareRequestWithoutValidation(t, `{
			"model":"sd_2.0_special_720p",
			"prompt":"p",
			"audios":["a1"],
			"duration":5
		}`)

		taskErr := adaptor.ValidateRequestAndSetAction(c, info)
		require.NotNil(t, taskErr)
		assert.Equal(t, "invalid_request", taskErr.Code)
	})

	t.Run("special accepts mapped with_video_ref", func(t *testing.T) {
		adaptor, c, info := prepareRequestWithoutValidation(t, `{
			"model":"public-special",
			"prompt":"p",
			"images":["i1"],
			"videos":["v1"],
			"audios":["a1"],
			"duration":5
		}`)
		c.Set("model_mapping", `{"public-special":"sd_2.0_fast_special_720p_with_video_ref"}`)

		require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
		assert.Equal(t, "sd_2.0_fast_special_720p_with_video_ref", info.UpstreamModelName)
	})

	t.Run("special material boundary and overflow", func(t *testing.T) {
		valid := `{
			"model":"sd_2.0_special_720p_with_video_ref",
			"prompt":"p",
			"images":["1","2","3","4","5","6","7","8","9"],
			"videos":["v1","v2","v3"],
			"audios":["a1","a2","a3"],
			"duration":5
		}`
		adaptor, c, info := prepareRequestWithoutValidation(t, valid)
		require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))

		for _, body := range []string{
			`{"model":"sd_2.0_special_720p","prompt":"p","images":["1","2","3","4","5","6","7","8","9","10"],"duration":5}`,
			`{"model":"sd_2.0_special_720p_with_video_ref","prompt":"p","images":["1"],"videos":["1","2","3","4"],"duration":5}`,
			`{"model":"sd_2.0_special_720p","prompt":"p","images":["1"],"audios":["1","2","3","4"],"duration":5}`,
		} {
			adaptor, c, info := prepareRequestWithoutValidation(t, body)
			taskErr := adaptor.ValidateRequestAndSetAction(c, info)
			require.NotNil(t, taskErr)
			assert.Equal(t, "material_limit_exceeded", taskErr.Code)
		}
	})

	t.Run("special alias frames are mutually exclusive with references", func(t *testing.T) {
		adaptor, c, info := prepareRequestWithoutValidation(t, `{
			"model":"sd_2.0_special_720p",
			"prompt":"p",
			"image":"https://example.com/first.png",
			"referenceImages":["https://example.com/ref.png"],
			"duration":5
		}`)
		taskErr := adaptor.ValidateRequestAndSetAction(c, info)
		require.NotNil(t, taskErr)
		assert.Equal(t, "invalid_request", taskErr.Code)
	})

	t.Run("special explicit resolution must match model", func(t *testing.T) {
		adaptor, c, info := prepareRequestWithoutValidation(t, `{
			"model":"sd_2.0_special_1080p",
			"prompt":"p",
			"resolution":"720p",
			"duration":5
		}`)
		taskErr := adaptor.ValidateRequestAndSetAction(c, info)
		require.NotNil(t, taskErr)
		assert.Equal(t, "invalid_request", taskErr.Code)
	})
}

func TestConfiguredModelMappingControlsUpstreamModelWithoutChangingSpecialRoute(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		mapping     string
		wantModel   string
		wantURLPath string
	}{
		{
			name:        "custom mapped model",
			body:        `{"model":"public-seedance","prompt":"p","duration":5,"images":["https://example.com/1.jpg"]}`,
			mapping:     `{"public-seedance":"managed-seedance","managed-seedance":"custom_upstream_model"}`,
			wantModel:   "custom_upstream_model",
			wantURLPath: "/v1/seedance-special/videos",
		},
		{
			name:        "documented special model through channel mapping",
			body:        `{"model":"public-special","prompt":"p","duration":5,"images":["https://example.com/1.jpg"]}`,
			mapping:     `{"public-special":"sd_2.1_special_custom"}`,
			wantModel:   "sd_2.1_special_custom",
			wantURLPath: "/v1/seedance-special/videos",
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
			assert.Equal(t, "https://provider.example"+tc.wantURLPath, requestURL)

			payload := buildPayload(t, adaptor, c, info)
			assert.Equal(t, tc.wantModel, payload["model"])
		})
	}
}

func TestBuildRequestBodyNormalizesFlatSpecialRequest(t *testing.T) {
	adaptor, c, info := prepareRequestWithoutValidation(t, `{
		"model":"seedance-public",
		"prompt":"first person fruit tea ad",
		"input_reference":"https://example.com/first.jpg",
		"last_frame_image":"https://example.com/last.jpg",
		"aspect_ratio":"9:16",
		"return_last_frame":true,
		"generation_type":"compat"
	}`)
	c.Set("model_mapping", `{"seedance-public":"sd_2.0_fast_special_720p"}`)
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))

	payload := buildPayload(t, adaptor, c, info)

	assert.Equal(t, "sd_2.0_fast_special_720p", payload["model"])
	assert.Equal(t, "9:16", payload["ratio"])
	assert.Equal(t, true, payload["return_last_frame"])
	assert.NotContains(t, payload, "prompt")
	assert.NotContains(t, payload, "first_image")
	assert.NotContains(t, payload, "last_image")
	assert.NotContains(t, payload, "image")
	assert.NotContains(t, payload, "input_reference")
	assert.NotContains(t, payload, "lastFrameImage")
	assert.NotContains(t, payload, "last_frame_image")
	assert.NotContains(t, payload, "aspect_ratio")
	assert.NotContains(t, payload, "generation_type")

	content, ok := payload["content"].([]any)
	require.True(t, ok)
	require.Len(t, content, 3)
	assert.Equal(t, map[string]any{
		"type": "text",
		"text": "first person fruit tea ad",
	}, content[0])
	assert.Equal(t, map[string]any{
		"type":      "image_url",
		"role":      "first_frame",
		"image_url": map[string]any{"url": "https://example.com/first.jpg"},
	}, content[1])
	assert.Equal(t, map[string]any{
		"type":      "image_url",
		"role":      "last_frame",
		"image_url": map[string]any{"url": "https://example.com/last.jpg"},
	}, content[2])
}

func TestBuildRequestBodyNormalizesReferenceMaterialsToDocumentedContent(t *testing.T) {
	adaptor, c, info := prepareRequestWithoutValidation(t, `{
		"model":"public-special",
		"prompt":"use all references",
		"images":["https://example.com/reference.jpg"],
		"videos":["https://example.com/reference.mp4"],
		"audios":["https://example.com/reference.mp3"],
		"duration":5
	}`)
	c.Set("model_mapping", `{"public-special":"sd_2.0_special_720p_with_video_ref"}`)
	require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))

	payload := buildPayload(t, adaptor, c, info)

	assert.Equal(t, "sd_2.0_special_720p_with_video_ref", payload["model"])
	assert.Equal(t, []any{
		map[string]any{"type": "text", "text": "use all references"},
		map[string]any{"type": "image_url", "role": "reference_image", "image_url": map[string]any{"url": "https://example.com/reference.jpg"}},
		map[string]any{"type": "video_url", "role": "reference_video", "video_url": map[string]any{"url": "https://example.com/reference.mp4"}},
		map[string]any{"type": "audio_url", "role": "reference_audio", "audio_url": map[string]any{"url": "https://example.com/reference.mp3"}},
	}, payload["content"])
}

func TestBuildRequestBodyPreservesNativeSpecialContent(t *testing.T) {
	t.Run("content payload", func(t *testing.T) {
		adaptor, c, info := prepareRequestWithoutValidation(t, `{
			"model":"seedance-public",
			"duration":5,
			"content":[
				{"type":"text","text":"真人口播广告"},
				{"type":"image_url","role":"reference_image","image_url":{"url":"https://example.com/1.jpg"}}
			]
		}`)
		c.Set("model_mapping", `{"seedance-public":"sd_2.0_special_720p"}`)
		require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))

		payload := buildPayload(t, adaptor, c, info)
		assert.Equal(t, "sd_2.0_special_720p", payload["model"])
		content, ok := payload["content"].([]any)
		require.True(t, ok)
		require.Len(t, content, 2)
	})
}

func TestBuildRequestBodySanitizesNativeSpecialCompatibilityFields(t *testing.T) {
	adaptor, c, info := prepareRequest(t, `{
		"model":"sd_2.0_special_720p",
		"prompt":"compatibility prompt must not leak",
		"image":"https://example.com/compatibility.jpg",
		"aspect_ratio":"9:16",
		"seconds":5,
		"callback_url":"https://example.com/callback",
		"priority":"high",
		"watermark":true,
		"content":[{"type":"text","text":"native prompt"}]
	}`)

	payload := buildPayload(t, adaptor, c, info)

	assert.Equal(t, "9:16", payload["ratio"])
	assert.Equal(t, float64(5), payload["duration"])
	assert.Equal(t, []any{map[string]any{"type": "text", "text": "native prompt"}}, payload["content"])
	assert.NotContains(t, payload, "prompt")
	assert.NotContains(t, payload, "image")
	assert.NotContains(t, payload, "aspect_ratio")
	assert.NotContains(t, payload, "seconds")
	assert.NotContains(t, payload, "callback_url")
	assert.NotContains(t, payload, "priority")
	assert.NotContains(t, payload, "watermark")
}

func TestEstimateBillingUsesBaseNoop(t *testing.T) {
	adaptor, c, info := prepareRequest(t, `{
		"model":"sd_2.0_special_720p",
		"prompt":"p",
		"duration":10
	}`)
	require.Nil(t, adaptor.EstimateBilling(c, info))
}

func TestBuildRequestURLAndHeader(t *testing.T) {
	t.Run("route is always seedance special", func(t *testing.T) {
		adaptor := &TaskAdaptor{}
		info := testRelayInfo("https://provider.example/v1/", "custom_upstream_model")
		adaptor.Init(info)
		got, err := adaptor.BuildRequestURL(info)
		require.NoError(t, err)
		assert.Equal(t, "https://provider.example/v1/seedance-special/videos", got)
	})

	t.Run("route does not depend on relay model", func(t *testing.T) {
		adaptor := &TaskAdaptor{baseURL: "https://provider.example"}
		got, err := adaptor.BuildRequestURL(nil)
		require.NoError(t, err)
		assert.Equal(t, "https://provider.example/v1/seedance-special/videos", got)
	})

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	req := httptest.NewRequest(http.MethodPost, "https://provider.example/v1/seedance-special/videos", nil)
	info := testRelayInfo("https://provider.example", "sd_2.0_special_720p")
	require.NoError(t, (&TaskAdaptor{}).BuildRequestHeader(c, req, info))
	assert.Equal(t, "Bearer test-key", req.Header.Get("Authorization"))
	assert.Equal(t, "application/json", req.Header.Get("Content-Type"))
	assert.Equal(t, "", req.Header.Get("X-MR-Async"))
	assert.Equal(t, "task_public", req.Header.Get("Idempotency-Key"))
}

func TestBuildRequestHeaderPreservesClientIdempotencyKeyAcrossAttempts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	c.Request.Header.Set("Idempotency-Key", "client-request-123")
	info := testRelayInfo("https://provider.example", "sd_2.0_special_720p")

	for range 2 {
		req := httptest.NewRequest(http.MethodPost, "https://provider.example/v1/seedance-special/videos", nil)
		require.NoError(t, (&TaskAdaptor{}).BuildRequestHeader(c, req, info))
		assert.Equal(t, "client-request-123", req.Header.Get("Idempotency-Key"))
	}
}

func TestDoRequestNormalizesAcceptedStatus(t *testing.T) {
	service.InitHttpClient()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/seedance-special/videos", r.URL.Path)
		assert.Equal(t, "", r.Header.Get("X-MR-Async"))
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"id":"vendor_task"}`))
	}))
	t.Cleanup(server.Close)

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	info := testRelayInfo(server.URL, "sd_2.0_special_720p")
	adaptor := &TaskAdaptor{}
	adaptor.Init(info)

	resp, err := adaptor.DoRequest(c, info, strings.NewReader(`{"model":"sd_2.0_special_720p","content":[{"type":"text","text":"p"}],"duration":4}`))
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
			"id":"seedance_vendor_task_123",
			"status":"queued"
		}`)),
	}
	info := testRelayInfo("https://provider.example", "sd_2.0_special_720p")

	taskID, taskData, taskErr := (&TaskAdaptor{}).DoResponse(c, resp, info)

	require.Nil(t, taskErr)
	assert.Equal(t, "seedance_vendor_task_123", taskID)
	assert.Contains(t, string(taskData), "seedance_vendor_task_123")
	assert.Contains(t, writer.Body.String(), `"id":"task_public"`)
	assert.NotContains(t, writer.Body.String(), "seedance_vendor_task_123")
}

func TestDoResponseTreatsFailedStatusAsSubmitFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	writer := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(writer)
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(strings.NewReader(`{
			"id":"seedance_vendor_task_123",
			"status":"failed",
			"error":"provider rejected prompt"
		}`)),
	}
	info := testRelayInfo("https://provider.example", "sd_2.0_special_720p")

	taskID, taskData, taskErr := (&TaskAdaptor{}).DoResponse(c, resp, info)

	require.NotNil(t, taskErr)
	assert.Equal(t, "", taskID)
	assert.Nil(t, taskData)
	assert.Equal(t, "task_submit_failed", taskErr.Code)
}

func TestFetchTaskUsesResultPath(t *testing.T) {
	service.InitHttpClient()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/result/seedance_vendor_task_123", r.URL.Path)
		assert.Equal(t, "Bearer poll-key", r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"id":"seedance_vendor_task_123","status":"processing"}`))
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

	got, err := (&TaskAdaptor{}).ParseTaskResult([]byte(`{"id":"t","status":"completed","video_url":"https://cdn.example/1.mp4"}`))
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
			OriginModelName: "sd_2.0_special_720p",
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
	info := testRelayInfo("https://provider.example", "sd_2.0_special_720p")
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
