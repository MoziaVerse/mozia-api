package cool

import (
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/gin-gonic/gin"
)

func TestParseSeedanceModel(t *testing.T) {
	tests := []struct {
		model      string
		wantOK     bool
		wantBase   string
		wantRes    string
		wantVideo  bool
		wantDynami bool
	}{
		{"cool:seedance_2", true, "seedance_2", "", false, true},
		{"seedance_2", true, "seedance_2", "", false, true},
		{"cool:seedance_2_480p", true, "seedance_2", "480p", false, false},
		{"cool:seedance_2_720p", true, "seedance_2", "720p", false, false},
		{"cool:seedance_2_1080p", true, "seedance_2", "1080p", false, false},
		{"cool:seedance_2_480p_video", true, "seedance_2", "480p", true, false},
		{"cool:seedance_2_1080p_video", true, "seedance_2", "1080p", true, false},
		{"cool:seedance_2_fast", true, "seedance_2_fast", "", false, true},
		{"cool:seedance_2_fast_480p", true, "seedance_2_fast", "480p", false, false},
		{"cool:seedance_2_fast_720p_video", true, "seedance_2_fast", "720p", true, false},
		// 非 Seedance 2.0 / 空串：不识别
		{"kling_3_omni", false, "", "", false, false},
		{"cool:gpt_image_2", false, "", "", false, false},
		{"seedance_1_5_pro_audio", false, "", "", false, false},
		{"", false, "", "", false, false},
	}
	for _, tc := range tests {
		t.Run(tc.model, func(t *testing.T) {
			got := parseSeedanceModel(tc.model)
			if got.OK != tc.wantOK {
				t.Fatalf("OK: got %v want %v", got.OK, tc.wantOK)
			}
			if !tc.wantOK {
				return
			}
			if got.Base != tc.wantBase || got.Resolution != tc.wantRes ||
				got.Video != tc.wantVideo || got.Dynamic != tc.wantDynami {
				t.Fatalf("got %+v", got)
			}
		})
	}
}

func TestSeedanceRatios(t *testing.T) {
	tests := []struct {
		name           string
		model          string
		resolution     string
		duration       int
		wantDuration   float64
		wantResolution float64 // -1 = 期望不返回 resolution key（固定 SKU）
	}{
		// 按秒计费：durationRatio == 秒数。
		{"dynamic 720p 5s baseline", "cool:seedance_2", "720p", 5, 5, 1.0},
		{"dynamic 1080p 10s", "cool:seedance_2", "1080p", 10, 10, 2.478 / 0.994},
		{"dynamic 480p 4s linear", "cool:seedance_2", "480p", 4, 4, 0.462 / 0.994},
		{"dynamic fast 480p 15s", "cool:seedance_2_fast", "480p", 15, 15, 0.372 / 0.800},
		{"dynamic fast 1080p -> 720p fallback", "cool:seedance_2_fast", "1080p", 5, 5, 1.0},
		{"dynamic missing res -> 720p", "cool:seedance_2", "", 5, 5, 1.0},
		// 固定 SKU：只有 duration 倍率，不叠加 resolution（单价已含分辨率）。
		{"fixed 480p only duration", "cool:seedance_2_480p", "480p", 10, 10, -1},
		{"fixed 1080p video only duration", "cool:seedance_2_1080p_video", "1080p", 5, 5, -1},
		{"fixed fast 720p video only duration", "cool:seedance_2_fast_720p_video", "720p", 4, 4, -1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			spec := parseSeedanceModel(tc.model)
			if !spec.OK {
				t.Fatalf("parseSeedanceModel(%q) not OK", tc.model)
			}
			got := seedanceRatios(spec, tc.resolution, tc.duration)
			assertFloatClose(t, got["duration"], tc.wantDuration)
			if tc.wantResolution < 0 {
				if _, ok := got["resolution"]; ok {
					t.Fatalf("fixed SKU should not emit resolution ratio, got %+v", got)
				}
				return
			}
			assertFloatClose(t, got["resolution"], tc.wantResolution)
		})
	}
}

func TestEstimateBillingDynamicReadsTopLevelResolution(t *testing.T) {
	got := estimateBillingForBody(t,
		"cool:seedance_2",
		`{"model":"cool:seedance_2","prompt":"hi","resolution":"1080p","duration":10}`,
	)
	assertFloatClose(t, got["duration"], 10)
	assertFloatClose(t, got["resolution"], 2.478/0.994)
}

func TestEstimateBillingDynamicReadsMetadataResolution(t *testing.T) {
	got := estimateBillingForBody(t,
		"seedance_2_fast",
		`{"model":"seedance_2_fast","prompt":"hi","metadata":{"resolution":"480p"},"duration":5}`,
	)
	assertFloatClose(t, got["duration"], 5)
	assertFloatClose(t, got["resolution"], 0.372/0.800)
}

func TestEstimateBillingDynamicDefaultsToBaseline(t *testing.T) {
	got := estimateBillingForBody(t,
		"seedance_2",
		`{"model":"seedance_2","prompt":"hi"}`,
	)
	assertFloatClose(t, got["duration"], 5)   // missing duration -> 5s
	assertFloatClose(t, got["resolution"], 1) // missing resolution -> 720p
}

// 固定 SKU：分辨率后缀优先，请求体 resolution 被忽略，且不叠加 resolution 倍率。
func TestEstimateBillingFixedIgnoresBodyResolution(t *testing.T) {
	got := estimateBillingForBody(t,
		"cool:seedance_2_480p",
		`{"model":"cool:seedance_2_480p","prompt":"hi","resolution":"1080p","duration":10}`,
	)
	assertFloatClose(t, got["duration"], 10)
	if _, ok := got["resolution"]; ok {
		t.Fatalf("fixed SKU must not emit resolution ratio, got %+v", got)
	}
}

func TestEstimateBillingVideoFixedOnlyDuration(t *testing.T) {
	got := estimateBillingForBody(t,
		"cool:seedance_2_720p_video",
		`{"model":"cool:seedance_2_720p_video","prompt":"hi","duration":5}`,
	)
	assertFloatClose(t, got["duration"], 5)
	if _, ok := got["resolution"]; ok {
		t.Fatalf("fixed video SKU must not emit resolution ratio, got %+v", got)
	}
}

func TestEstimateBillingSkipsNonSeedance2(t *testing.T) {
	got := estimateBillingForBody(t,
		"kling_3_omni",
		`{"model":"kling_3_omni","prompt":"hi","resolution":"1080p"}`,
	)
	if got != nil {
		t.Fatalf("expected nil ratios for non-seedance2 model, got %+v", got)
	}
}

func estimateBillingForBody(t *testing.T, upstreamModel, body string) map[string]float64 {
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
		t.Fatalf("ValidateRequestAndSetAction returned error: %+v", taskErr)
	}
	return adaptor.EstimateBilling(c, info)
}

func assertFloatClose(t *testing.T, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 0.000001 {
		t.Fatalf("got %f, want %f", got, want)
	}
}
