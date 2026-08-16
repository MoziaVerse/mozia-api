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

func TestBuildRequestBodySelectsMiniMaxH3FrameModelFromExternalAlias(t *testing.T) {
	adaptor, c, info := prepareRequest(t, `{
		"model":"MiniMax-H3",
		"duration":5,
		"resolution":"768P",
		"ratio":"21:9",
		"content":[
			{"type":"text","text":"animate the moment before the jump"},
			{"type":"image_url","role":"first_frame","image_url":{"url":"https://example.com/first.jpg"}},
			{"type":"image_url","role":"last_frame","image_url":{"url":"https://example.com/last.jpg"}}
		]
	}`)
	info.OriginModelName = "MiniMax-H3"
	info.UpstreamModelName = info.OriginModelName

	payload := buildPayload(t, adaptor, c, info)

	assert.Equal(t, "minimax/minimax-h3-fl2va", payload["model"])
	assert.Equal(t, "animate the moment before the jump", payload["prompt"])
	assert.Equal(t, "1344x576", payload["size"])
	assert.Equal(t, []any{"https://example.com/first.jpg", "https://example.com/last.jpg"}, payload["images"])
	assert.NotContains(t, payload, "content")
	assert.NotContains(t, payload, "ratio")
	assert.NotContains(t, payload, "resolution")
}

func TestBuildRequestBodySelectsMiniMaxH3ReferenceModelFromExternalAlias(t *testing.T) {
	adaptor, c, info := prepareRequest(t, `{
		"model":"MiniMax-H3",
		"duration":5,
		"aspect_ratio":"1:1",
		"content":[
			{"type":"text","text":"bring the photo to life"},
			{"type":"image_url","role":"reference_image","image_url":{"url":"https://example.com/reference.jpg"}}
		]
	}`)
	info.OriginModelName = "MiniMax-H3"
	info.UpstreamModelName = info.OriginModelName

	payload := buildPayload(t, adaptor, c, info)

	assert.Equal(t, "minimax/minimax-h3-ref2va", payload["model"])
	assert.Equal(t, "768x768", payload["size"])
	assert.Equal(t, []any{"https://example.com/reference.jpg"}, payload["images"])
	assert.NotContains(t, payload, "content")
}

func TestBuildRequestBodyPreservesExplicitMiniMaxH3ModelMapping(t *testing.T) {
	adaptor, c, info := prepareRequest(t, `{
		"model":"MiniMax-H3",
		"duration":5,
		"resolution":"768P",
		"content":[
			{"type":"text","text":"use the configured upstream model"},
			{"type":"image_url","role":"reference_image","image_url":{"url":"https://example.com/reference.jpg"}}
		]
	}`)
	info.OriginModelName = "MiniMax-H3"
	info.UpstreamModelName = info.OriginModelName
	c.Set("model_mapping", `{"MiniMax-H3":"minimax-h3-ref2va-int8"}`)
	require.NoError(t, relayhelper.ModelMappedHelper(c, info, nil))
	require.True(t, info.IsModelMapped)

	payload := buildPayload(t, adaptor, c, info)

	assert.Equal(t, "minimax-h3-ref2va-int8", payload["model"])
	assert.Equal(t, "https://example.com/reference.jpg", payload["images"].([]any)[0])
}

func TestBuildRequestBodyMiniMaxH3ExplicitSizeWins(t *testing.T) {
	adaptor, c, info := prepareRequest(t, `{
		"model":"MiniMax-H3",
		"duration":5,
		"size":"992x992",
		"resolution":"768P",
		"ratio":"9:16",
		"content":[
			{"type":"text","text":"prefer the explicit legacy size"}
		]
	}`)
	info.OriginModelName = "MiniMax-H3"
	info.UpstreamModelName = info.OriginModelName

	payload := buildPayload(t, adaptor, c, info)

	assert.Equal(t, "992x992", payload["size"])
}

func TestBuildRequestBodyMiniMaxH3TopLevelParametersBeatMetadata(t *testing.T) {
	adaptor, c, info := prepareRequest(t, `{
		"model":"MiniMax-H3",
		"prompt":"use canonical parameters",
		"duration":5,
		"resolution":"768P",
		"ratio":"1:1",
		"metadata":{"resolution":"2K","ratio":"adaptive"}
	}`)
	info.OriginModelName = "MiniMax-H3"
	info.UpstreamModelName = info.OriginModelName

	payload := buildPayload(t, adaptor, c, info)

	assert.Equal(t, "768x768", payload["size"])
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

func TestValidateRequestRejectsMiniMaxH3UnsupportedReferenceMedia(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "reference video",
			body: `{"model":"MiniMax-H3","duration":5,"content":[{"type":"text","text":"p"},{"type":"video_url","role":"reference_video","video_url":{"url":"https://example.com/ref.mp4"}}]}`,
		},
		{
			name: "reference audio",
			body: `{"model":"MiniMax-H3","duration":5,"content":[{"type":"text","text":"p"},{"type":"audio_url","role":"reference_audio","audio_url":{"url":"https://example.com/ref.mp3"}}]}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			adaptor, c, info := prepareRequestWithoutValidation(t, tc.body)
			info.OriginModelName = "MiniMax-H3"
			info.UpstreamModelName = info.OriginModelName

			taskErr := adaptor.ValidateRequestAndSetAction(c, info)

			require.NotNil(t, taskErr)
			assert.Equal(t, "invalid_request", taskErr.Code)
		})
	}
}

func TestValidateRequestRejectsMiniMaxH3ImageLimitViolations(t *testing.T) {
	t.Run("ref2va over 9 images", func(t *testing.T) {
		adaptor, c, info := prepareRequestWithoutValidation(t, `{
			"model":"MiniMax-H3",
			"duration":5,
			"content":[
				{"type":"text","text":"p"},
				{"type":"image_url","role":"reference_image","image_url":{"url":"https://example.com/1.jpg"}},
				{"type":"image_url","role":"reference_image","image_url":{"url":"https://example.com/2.jpg"}},
				{"type":"image_url","role":"reference_image","image_url":{"url":"https://example.com/3.jpg"}},
				{"type":"image_url","role":"reference_image","image_url":{"url":"https://example.com/4.jpg"}},
				{"type":"image_url","role":"reference_image","image_url":{"url":"https://example.com/5.jpg"}},
				{"type":"image_url","role":"reference_image","image_url":{"url":"https://example.com/6.jpg"}},
				{"type":"image_url","role":"reference_image","image_url":{"url":"https://example.com/7.jpg"}},
				{"type":"image_url","role":"reference_image","image_url":{"url":"https://example.com/8.jpg"}},
				{"type":"image_url","role":"reference_image","image_url":{"url":"https://example.com/9.jpg"}},
				{"type":"image_url","role":"reference_image","image_url":{"url":"https://example.com/10.jpg"}}
			]
		}`)
		info.OriginModelName = "MiniMax-H3"
		info.UpstreamModelName = info.OriginModelName

		taskErr := adaptor.ValidateRequestAndSetAction(c, info)

		require.NotNil(t, taskErr)
		assert.Equal(t, "invalid_request", taskErr.Code)
	})

	t.Run("fl2va legacy images over 2", func(t *testing.T) {
		adaptor, c, info := prepareRequestWithoutValidation(t, `{
			"model":"minimax/minimax-h3-fl2va",
			"prompt":"p",
			"duration":5,
			"images":[
				"https://example.com/1.jpg",
				"https://example.com/2.jpg",
				"https://example.com/3.jpg"
			]
		}`)
		info.OriginModelName = "minimax/minimax-h3-fl2va"
		info.UpstreamModelName = info.OriginModelName

		taskErr := adaptor.ValidateRequestAndSetAction(c, info)

		require.NotNil(t, taskErr)
		assert.Equal(t, "invalid_request", taskErr.Code)
	})
}

func TestValidateRequestRejectsMiniMaxH3UnsupportedResolution(t *testing.T) {
	tests := []string{
		`{"model":"MiniMax-H3","prompt":"p","duration":5,"resolution":"2K"}`,
		`{"model":"MiniMax-H3","prompt":"p","duration":5,"resolution":"768P","ratio":"adaptive"}`,
		`{"model":"MiniMax-H3","prompt":"p","duration":5,"size":"1366x768"}`,
	}

	for _, body := range tests {
		t.Run(body, func(t *testing.T) {
			adaptor, c, info := prepareRequestWithoutValidation(t, body)
			info.OriginModelName = "MiniMax-H3"
			info.UpstreamModelName = info.OriginModelName

			taskErr := adaptor.ValidateRequestAndSetAction(c, info)

			require.NotNil(t, taskErr)
			assert.Equal(t, "invalid_size", taskErr.Code)
		})
	}
}

// Geometry has to reach tasks.properties, otherwise an OOM failure cannot be
// classified afterwards as "card was busy, retry helps" versus "allocation
// exceeds card capacity, retry is futile" — the two need opposite advice.
func TestValidateRequestRecordsResolvedGeometry(t *testing.T) {
	tests := []struct {
		name         string
		body         string
		originModel  string
		wantSize     string
		wantDuration int
	}{
		{
			name:         "explicit size is recorded verbatim",
			body:         `{"model":"MiniMax-H3","prompt":"p","duration":15,"size":"1344x768"}`,
			originModel:  "MiniMax-H3",
			wantSize:     "1344x768",
			wantDuration: 15,
		},
		{
			// resolution+ratio callers would be lost entirely if we recorded the raw
			// size field, which is empty here — the adaptor normalizes them instead.
			name:         "resolution and ratio normalize to a concrete size",
			body:         `{"model":"MiniMax-H3","prompt":"p","duration":5,"resolution":"768P","ratio":"16:9"}`,
			originModel:  "MiniMax-H3",
			wantSize:     "1344x768",
			wantDuration: 5,
		},
		{
			// No geometry at all = the client's default tier. Empty is the correct
			// record here: the upstream picks its own default, and we must not
			// invent a value the gateway never sent.
			name:         "default tier records empty size, not a guess",
			body:         `{"model":"MiniMax-H3","prompt":"p","duration":15}`,
			originModel:  "MiniMax-H3",
			wantSize:     "",
			wantDuration: 15,
		},
		{
			name:         "non-H3 models still record duration",
			body:         `{"model":"cool:seedance_2_720p","prompt":"p","duration":10}`,
			originModel:  "cool:seedance_2_720p",
			wantDuration: 10,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			adaptor, c, info := prepareRequestWithoutValidation(t, tc.body)
			info.OriginModelName = tc.originModel
			info.UpstreamModelName = tc.originModel

			taskErr := adaptor.ValidateRequestAndSetAction(c, info)

			require.Nil(t, taskErr)
			assert.Equal(t, tc.wantSize, info.ResolvedSize)
			assert.Equal(t, tc.wantDuration, info.ResolvedDuration)
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
