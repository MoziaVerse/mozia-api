package cool

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
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

// 非 Seedance 模型也要剥掉对外 cool: 前缀（cool 的 model key 不带前缀）。
func TestConvertPayloadNonSeedanceStripsPrefix(t *testing.T) {
	r := buildPayloadForBody(t, "cool:gpt_image_2", `{"model":"cool:gpt_image_2","prompt":"hi"}`)
	if r.Model != "gpt_image_2" {
		t.Fatalf("Model: got %q want gpt_image_2", r.Model)
	}
}
