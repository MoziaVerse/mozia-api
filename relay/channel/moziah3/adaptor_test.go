package moziah3

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildFL2VARequestFromOpenAIVideoContent(t *testing.T) {
	body := `{
		"model":"minimax/minimax-h3-fl2va",
		"prompt":"animate the antlion",
		"duration":15,
		"size":"1344x768",
		"content":[
			{"type":"image_url","role":"first_frame","image_url":{"url":"https://example.com/first.jpg"}},
			{"type":"image_url","role":"last_frame","image_url":{"url":"https://example.com/last.jpg"}}
		]
	}`
	request, adaptor, info := buildUpstreamRequest(t, body, "minimax/minimax-h3-fl2va")

	assert.Equal(t, defaultUpstreamModel, request.Model)
	assert.Equal(t, "fl2va", request.Task)
	assert.Equal(t, "animate the antlion", request.Prompt)
	require.Len(t, request.Conditions, 2)
	assert.Equal(t, 0, *request.Conditions[0].FrameIndex)
	assert.Equal(t, -1, *request.Conditions[1].FrameIndex)
	assert.Equal(t, 768, *request.Target.ShortEdge)
	assert.Equal(t, "16:9", request.Target.AspectRatio)
	assert.Equal(t, 15.0, *request.Target.DurationSeconds)
	assert.Equal(t, 15.0, *request.Seconds)
	assert.Equal(t, 2.0, *request.AudioFlowShift)
	assert.Equal(t, map[string]float64{"duration": 15}, adaptor.EstimateBilling(info.context, info.relay))
}

func TestBuildRef2VARequestPreservesFractionalDurationAndReferenceAudio(t *testing.T) {
	body := `{
		"model":"minimax/minimax-h3-ref2va",
		"prompt":"follow the reference motion",
		"content":[{"type":"video_url","role":"reference_video","video_url":{"url":"https://example.com/reference.mp4"}}],
		"metadata":{
			"target":{"short_edge":768,"aspect_ratio":"16:9"},
			"duration":13.667,
			"preserve_reference_audio":true
		}
	}`
	request, adaptor, info := buildUpstreamRequest(t, body, "minimax/minimax-h3-ref2va")

	assert.Equal(t, "ref2va", request.Task)
	require.Len(t, request.Conditions, 1)
	assert.Equal(t, "video_audio", request.Conditions[0].Type)
	assert.Equal(t, "reference", request.Conditions[0].Role)
	assert.Equal(t, 13.667, *request.Target.DurationSeconds)
	assert.Nil(t, request.Seconds)
	assert.Equal(t, 3.0, *request.AudioFlowShift)
	assert.Equal(t, map[string]float64{"duration": 13.667}, adaptor.EstimateBilling(info.context, info.relay))
}

func TestBuildNativeSGLangRequestHonorsMappedModel(t *testing.T) {
	body := `{
		"model":"MiniMax/MiniMax-H3",
		"task":"fl2va",
		"prompt":"native request",
		"conditions":[{"type":"image","uri":"https://example.com/first.jpg","role":"keyframe","frame_index":0}],
		"target":{"short_edge":768,"aspect_ratio":"9:16","duration_seconds":4.5},
		"seconds":4.5,
		"seed":42
	}`
	request, _, _ := buildUpstreamRequest(t, body, "MiniMax/MiniMax-H3")

	assert.Equal(t, defaultUpstreamModel, request.Model)
	assert.Equal(t, "image", request.Conditions[0].Type)
	assert.Equal(t, "keyframe", request.Conditions[0].Role)
	assert.Equal(t, int64(42), *request.Seed)
	assert.Equal(t, 4.5, *request.Target.DurationSeconds)
}

func TestRequestURLHeaderAndTaskStatusMapping(t *testing.T) {
	adaptor := &TaskAdaptor{baseURL: "https://h3.example/v1/"}
	requestURL, err := adaptor.BuildRequestURL(nil)
	require.NoError(t, err)
	assert.Equal(t, "https://h3.example/v1/videos", requestURL)

	req := httptest.NewRequest(http.MethodPost, requestURL, nil)
	require.NoError(t, adaptor.BuildRequestHeader(nil, req, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ApiKey: "secret"}}))
	assert.Equal(t, "Bearer secret", req.Header.Get("Authorization"))

	tests := []struct {
		body       string
		status     string
		progress   string
		failReason string
	}{
		{body: `{"id":"upstream","status":"queued"}`, status: string(model.TaskStatusQueued)},
		{body: `{"id":"upstream","status":"completed"}`, status: string(model.TaskStatusSuccess)},
		{body: `{"id":"upstream","status":"cancelled","error":{"message":"operator cancelled"}}`, status: string(model.TaskStatusFailure), failReason: "operator cancelled"},
	}
	for _, tc := range tests {
		result, err := adaptor.ParseTaskResult([]byte(tc.body))
		require.NoError(t, err)
		assert.Equal(t, tc.status, result.Status)
		assert.Equal(t, tc.progress, result.Progress)
		assert.Equal(t, tc.failReason, result.Reason)
	}
}

type requestInfo struct {
	context *gin.Context
	relay   *relaycommon.RelayInfo
}

func buildUpstreamRequest(t *testing.T, body, publicModel string) (*upstreamRequest, *TaskAdaptor, requestInfo) {
	t.Helper()
	ctx, cleanup := requestContext(t, body)
	t.Cleanup(cleanup)
	info := relayInfo(publicModel)
	adaptor := &TaskAdaptor{}
	adaptor.Init(info)
	require.Nil(t, adaptor.ValidateRequestAndSetAction(ctx, info))

	reader, err := adaptor.BuildRequestBody(ctx, info)
	require.NoError(t, err)
	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	var request upstreamRequest
	require.NoError(t, common.Unmarshal(data, &request))
	return &request, adaptor, requestInfo{context: ctx, relay: info}
}

func requestContext(t *testing.T, body string) (*gin.Context, func()) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", bytes.NewBufferString(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	storage, err := common.GetBodyStorage(ctx)
	require.NoError(t, err)
	return ctx, func() { _ = storage.Close() }
}

func relayInfo(publicModel string) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		OriginModelName: publicModel,
		TaskRelayInfo:   &relaycommon.TaskRelayInfo{},
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl:    "https://h3.example",
			ApiKey:            "secret",
			IsModelMapped:     true,
			UpstreamModelName: defaultUpstreamModel,
		},
	}
}
