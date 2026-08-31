package cool

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func buildPayloadForBody(t *testing.T, upstreamModel, body string) *generateRequest {
	t.Helper()
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/video/generations", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	info := &relaycommon.RelayInfo{
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: upstreamModel,
			ChannelBaseUrl:    "https://api.mjapi.cc.cd",
		},
	}
	adaptor := &TaskAdaptor{}
	adaptor.Init(info)
	if taskErr := adaptor.ValidateRequestAndSetAction(c, info); taskErr != nil {
		t.Fatalf("ValidateRequestAndSetAction: %+v", taskErr)
	}
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		t.Fatalf("GetTaskRequest: %v", err)
	}
	payload, err := adaptor.convertToRequestPayload(c, &req, info)
	if err != nil {
		t.Fatalf("convertToRequestPayload: %v", err)
	}
	return payload
}

// 验证 SKU → cool 真实 model key + resolution 的转发路由（cool model 只认基础 key）。
func TestConvertPayloadSeedanceRouting(t *testing.T) {
	tests := []struct {
		name    string
		model   string
		body    string
		wantMod string
		wantRes string
	}{
		{"dynamic default -> 720p", "cool:seedance_2",
			`{"model":"cool:seedance_2","prompt":"hi"}`, "seedance_2", "720p"},
		{"dynamic body resolution wins", "cool:seedance_2",
			`{"model":"cool:seedance_2","prompt":"hi","resolution":"1080p"}`, "seedance_2", "1080p"},
		{"fixed 480p ignores body 1080p", "cool:seedance_2_480p",
			`{"model":"cool:seedance_2_480p","prompt":"hi","resolution":"1080p"}`, "seedance_2", "480p"},
		{"fixed fast 720p video -> base fast", "cool:seedance_2_fast_720p_video",
			`{"model":"cool:seedance_2_fast_720p_video","prompt":"hi"}`, "seedance_2_fast", "720p"},
		{"fast dynamic default -> 720p", "cool:seedance_2_fast",
			`{"model":"cool:seedance_2_fast","prompt":"hi"}`, "seedance_2_fast", "720p"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := buildPayloadForBody(t, tc.model, tc.body)
			if r.Model != tc.wantMod {
				t.Fatalf("Model: got %q want %q", r.Model, tc.wantMod)
			}
			if r.Resolution != tc.wantRes {
				t.Fatalf("Resolution: got %q want %q", r.Resolution, tc.wantRes)
			}
		})
	}
}

func TestEstimateBillingUsesBaseNoop(t *testing.T) {
	adaptor := &TaskAdaptor{}
	assert.Nil(t, adaptor.EstimateBilling(nil, nil))
}

// 非 Seedance 模型也要剥掉对外 cool: 前缀（cool 的 model key 不带前缀）。
func TestConvertPayloadNonSeedanceStripsPrefix(t *testing.T) {
	r := buildPayloadForBody(t, "cool:gpt_image_2", `{"model":"cool:gpt_image_2","prompt":"hi"}`)
	if r.Model != "gpt_image_2" {
		t.Fatalf("Model: got %q want gpt_image_2", r.Model)
	}
}

func TestConvertPayloadReferenceVideoDefaultsToVideoModel(t *testing.T) {
	r := buildPayloadForBody(t, "", `{
		"prompt":"use the reference video",
		"content":[
			{"type":"video_url","role":"reference_video","video_url":{"url":"https://example.com/ref.mp4"}}
		]
	}`)

	assert.Equal(t, defaultVideoModel, r.Model)
}

func TestConvertPayloadContentFramesOverrideLegacyFiles(t *testing.T) {
	r := buildPayloadForBody(t, "cool:seedance_2", `{
		"model":"cool:seedance_2",
		"prompt":"make it cinematic",
		"images":["https://example.com/legacy.png"],
		"content":[
			{"type":"image_url","role":"first_frame","image_url":{"url":"https://example.com/start.png"}},
			{"type":"image_url","role":"last_frame","image_url":{"url":"https://example.com/end.png"}}
		],
		"metadata":{
			"files":[{"url":"https://example.com/meta.png","type":"image"}]
		}
	}`)

	require.Len(t, r.Files, 2)
	assert.Equal(t, fileRef{URL: "https://example.com/start.png", Type: "image", Name: "start"}, r.Files[0])
	assert.Equal(t, fileRef{URL: "https://example.com/end.png", Type: "image", Name: "end"}, r.Files[1])
	assert.Equal(t, "make it cinematic", r.Prompt)
}

func TestConvertPayloadContentReferenceMediaTypes(t *testing.T) {
	r := buildPayloadForBody(t, "cool:seedance_2", `{
		"model":"cool:seedance_2",
		"prompt":"mix the references",
		"content":[
			{"type":"image_url","role":"reference_image","image_url":{"url":"https://example.com/ref.png"}},
			{"type":"video_url","role":"reference_video","video_url":{"url":"https://example.com/ref.mp4"}},
			{"type":"audio_url","role":"reference_audio","audio_url":{"url":"https://example.com/ref.mp3"}}
		]
	}`)

	require.Len(t, r.Files, 3)
	tests := []struct {
		index    int
		url      string
		fileType string
	}{
		{0, "https://example.com/ref.png", "image"},
		{1, "https://example.com/ref.mp4", "video"},
		{2, "https://example.com/ref.mp3", "audio"},
	}
	for _, tc := range tests {
		got := r.Files[tc.index]
		assert.Equal(t, tc.url, got.URL)
		assert.Equal(t, tc.fileType, got.Type)
		assert.Empty(t, got.Name)
	}
}

func TestConvertPayloadCanonicalTopLevelFieldsBeatMetadata(t *testing.T) {
	r := buildPayloadForBody(t, "cool:seedance_2", `{
		"model":"cool:seedance_2",
		"prompt":"hi",
		"aspect_ratio":"16:9",
		"size":"1:1",
		"resolution":"1080p",
		"duration":8,
		"metadata":{
			"ratio":"4:3",
			"resolution":"720p",
			"duration":3
		}
	}`)

	assert.Equal(t, "16:9", r.Ratio)
	assert.Equal(t, "1080p", r.Resolution)
	assert.Equal(t, 8, r.Duration)
}
