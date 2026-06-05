package mulerun

import (
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

func TestSeedanceBillingRatios(t *testing.T) {
	tests := []struct {
		name           string
		modelID        string
		params         seedanceBillingParams
		wantDuration   float64
		wantResolution float64
	}{
		{
			name:    "fast 720p 10 seconds",
			modelID: "bytedance/seedance-2.0-fast/text-to-video",
			params: seedanceBillingParams{
				Resolution: "720p",
				Duration:   10,
			},
			wantDuration:   2,
			wantResolution: 0.58 / 0.25,
		},
		{
			name:    "standard 1080p 15 seconds",
			modelID: "bytedance/seedance-2.0/text-to-video",
			params: seedanceBillingParams{
				Resolution: "1080p",
				Duration:   15,
			},
			wantDuration:   3,
			wantResolution: 1.82 / 0.33,
		},
		{
			name:    "standard 720p 4 seconds bills linearly",
			modelID: "bytedance/seedance-2.0/image-to-video",
			params: seedanceBillingParams{
				Resolution: "720p",
				Duration:   4,
			},
			wantDuration:   0.8,
			wantResolution: 0.71 / 0.33,
		},
		{
			name:    "missing parameters use conservative defaults",
			modelID: "bytedance/seedance-2.0/reference-to-video",
			params:  seedanceBillingParams{},
			// No duration means the upstream may auto-select; bill as 15s.
			wantDuration: 3,
			// Standard Seedance maxes out at 1080p.
			wantResolution: 1.82 / 0.33,
		},
		{
			name:    "fast missing resolution defaults to 720p",
			modelID: "bytedance/seedance-2.0-fast/reference-to-video",
			params: seedanceBillingParams{
				Duration: 5,
			},
			wantDuration:   1,
			wantResolution: 0.58 / 0.25,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := seedanceBillingRatios(tc.modelID, tc.params)
			assertFloatClose(t, got["duration"], tc.wantDuration)
			assertFloatClose(t, got["resolution"], tc.wantResolution)
		})
	}
}

func TestSeedanceEstimateBillingReadsTopLevelResolution(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(
		http.MethodPost,
		"/v1/videos",
		strings.NewReader(`{"model":"bytedance/seedance-2.0-fast/text-to-video","prompt":"hello","resolution":"720p","duration":10}`),
	)
	c.Request.Header.Set("Content-Type", "application/json")

	info := &relaycommon.RelayInfo{
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "bytedance/seedance-2.0-fast/text-to-video",
			ChannelBaseUrl:    "https://api.mulerun.com",
		},
	}
	adaptor := &TaskAdaptor{}
	adaptor.Init(info)

	if taskErr := adaptor.ValidateRequestAndSetAction(c, info); taskErr != nil {
		t.Fatalf("ValidateRequestAndSetAction returned error: %+v", taskErr)
	}

	got := adaptor.EstimateBilling(c, info)
	assertFloatClose(t, got["duration"], 2)
	assertFloatClose(t, got["resolution"], 0.58/0.25)
}

func TestSeedanceEstimateBillingUsesConservativeDefaults(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(
		http.MethodPost,
		"/v1/videos",
		strings.NewReader(`{"model":"bytedance/seedance-2.0/text-to-video","prompt":"hello","duration":-1}`),
	)
	c.Request.Header.Set("Content-Type", "application/json")

	info := &relaycommon.RelayInfo{
		TaskRelayInfo: &relaycommon.TaskRelayInfo{},
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: "bytedance/seedance-2.0/text-to-video",
			ChannelBaseUrl:    "https://api.mulerun.com",
		},
	}
	adaptor := &TaskAdaptor{}
	adaptor.Init(info)

	if taskErr := adaptor.ValidateRequestAndSetAction(c, info); taskErr != nil {
		t.Fatalf("ValidateRequestAndSetAction returned error: %+v", taskErr)
	}

	got := adaptor.EstimateBilling(c, info)
	assertFloatClose(t, got["duration"], 3)
	assertFloatClose(t, got["resolution"], 1.82/0.33)
}

func assertFloatClose(t *testing.T, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 0.000001 {
		t.Fatalf("got %f, want %f", got, want)
	}
}
