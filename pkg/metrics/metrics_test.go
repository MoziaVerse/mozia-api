package metrics

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVIPWatchlistEnvAndHotReload(t *testing.T) {
	r := NewRecorder()
	r.VIP().SetEnvConfig("6218:VIP-6218,7001", "enterprise")

	// Explicit alias from env
	customer, uid, ok := r.VIP().Resolve(6218, "mozia_54667f87-1f4d-", "default")
	require.True(t, ok)
	assert.Equal(t, "VIP-6218", customer)
	assert.Equal(t, "6218", uid)

	// Bare user ID falls back to username
	customer, uid, ok = r.VIP().Resolve(7001, "acme_user", "default")
	require.True(t, ok)
	assert.Equal(t, "acme_user", customer)
	assert.Equal(t, "7001", uid)

	// Unlisted user in VIP group matches with group-level label (bounded cardinality)
	customer, uid, ok = r.VIP().Resolve(9999, "member_a", "Enterprise")
	require.True(t, ok)
	assert.Equal(t, "group:Enterprise", customer)
	assert.Equal(t, "group", uid)

	// Ordinary user does not match
	_, _, ok = r.VIP().Resolve(12345, "normal", "default")
	assert.False(t, ok)

	// Hot-reload DB option adds new user ID and overrides alias without restart
	r.VIP().SetDynamicUsers("6218:VIP-Override,8888:NewBigClient")
	customer, uid, ok = r.VIP().Resolve(6218, "mozia_54667f87-1f4d-", "default")
	require.True(t, ok)
	assert.Equal(t, "VIP-Override", customer)
	assert.Equal(t, "6218", uid)

	customer, uid, ok = r.VIP().Resolve(8888, "new_client", "default")
	require.True(t, ok)
	assert.Equal(t, "NewBigClient", customer)
	assert.Equal(t, "8888", uid)
}

func TestLabelLimiterBoundsCardinality(t *testing.T) {
	limiter := newLabelLimiter(2)
	assert.Equal(t, "gpt-4o", limiter.Normalize("gpt-4o", "unknown"))
	assert.Equal(t, "claude-3-7", limiter.Normalize("claude-3-7", "unknown"))
	assert.Equal(t, "gpt-4o", limiter.Normalize("gpt-4o", "unknown"))
	assert.Equal(t, overflowLabelValue, limiter.Normalize("random-invalid-model", "unknown"))
	assert.Equal(t, "unknown", limiter.Normalize("   ", "unknown"))
}

func TestRecorderEmitsRelayVIPAndRuntimeMetrics(t *testing.T) {
	r := NewRecorder()
	r.VIP().SetEnvConfig("6218:Client-6218", "")
	r.SetBuildInfo("mozia-api-primary", "master", "v1.2.3")
	r.SetReady(true)
	r.SetRuntimeProviders(RuntimeProviders{
		ActiveConnections: func() int64 { return 12 },
		DBPoolStats: func() map[string]sql.DBStats {
			return map[string]sql.DBStats{
				"main": {
					MaxOpenConnections: 35,
					OpenConnections:    10,
					InUse:              4,
					Idle:               6,
					WaitCount:          2,
					WaitDuration:       50 * time.Millisecond,
				},
			}
		},
		RedisPoolStats: func() (RedisPoolStats, bool) {
			return RedisPoolStats{TotalConns: 8, IdleConns: 5, StaleConns: 1, Timeouts: 3}, true
		},
		BatchUpdatePending: func() map[string]int {
			return map[string]int{"user_quota": 7}
		},
		ChannelCacheSummary: func() (ChannelCacheSnapshot, bool) {
			return ChannelCacheSnapshot{
				StatusCounts: map[string]int{"enabled": 4, "auto_disabled": 1},
				ModelHealthyChannel: map[string]map[string]int{
					"default": {"gpt-4o": 1},
				},
			}, true
		},
	})

	// Record VIP request + perf + stream + retry + billing + payment
	r.RecordRequestOutcome(RequestOutcomeSample{
		Model:      "gpt-4o",
		Group:      "default",
		UserID:     6218,
		Username:   "mozia_54667f87-1f4d-",
		Outcome:    "success",
		ErrorClass: "none",
		StatusCode: 200,
		IsStream:   true,
		Duration:   3 * time.Second,
	})
	r.RecordRelayPerf(RelayPerfSample{
		Model:            "gpt-4o",
		Group:            "default",
		UserID:           6218,
		Username:         "mozia_54667f87-1f4d-",
		IsStream:         true,
		HasTTFT:          true,
		TTFT:             600 * time.Millisecond,
		GenerationTime:   2 * time.Second,
		PromptTokens:     100,
		CompletionTokens: 80,
		QuotaConsumed:    5000,
	})
	r.RecordStreamOutcome(StreamOutcomeSample{
		Model:     "gpt-4o",
		Group:     "default",
		UserID:    6218,
		Username:  "mozia_54667f87-1f4d-",
		Status:    "interrupted",
		EndReason: "timeout",
	})
	r.RecordChannelAttempt(10, "openai-primary", "gpt-4o", "upstream_429", false)
	r.RecordChannelAttempt(11, "openai-backup", "gpt-4o", "success", true)
	r.RecordGatewayOverhead("gpt-4o", "default", 15*time.Millisecond)
	r.RecordBillingError("settle", "db_or_internal_error")
	r.RecordPaymentWebhook("stripe", "success")

	// Ordinary user should NOT emit mozia_vip_* series
	r.RecordRequestOutcome(RequestOutcomeSample{
		Model:      "gpt-4o",
		Group:      "default",
		UserID:     999,
		Username:   "ordinary",
		Outcome:    "error",
		ErrorClass: "client_4xx",
		StatusCode: 401,
		Duration:   10 * time.Millisecond,
	})

	families, err := r.Registry().Gather()
	require.NoError(t, err)
	byName := make(map[string]*dto.MetricFamily, len(families))
	for _, fam := range families {
		byName[fam.GetName()] = fam
	}

	require.Contains(t, byName, "mozia_instance_ready")
	assert.Equal(t, 1.0, byName["mozia_instance_ready"].Metric[0].GetGauge().GetValue())

	require.Contains(t, byName, "mozia_http_active_connections")
	assert.Equal(t, 12.0, byName["mozia_http_active_connections"].Metric[0].GetGauge().GetValue())

	require.Contains(t, byName, "mozia_model_healthy_channels")
	assert.Equal(t, 1.0, byName["mozia_model_healthy_channels"].Metric[0].GetGauge().GetValue())

	require.Contains(t, byName, "mozia_vip_requests_total")
	assert.Len(t, byName["mozia_vip_requests_total"].Metric, 1, "only VIP user should appear in mozia_vip_requests_total")
	assert.Equal(t, 1.0, byName["mozia_vip_requests_total"].Metric[0].GetCounter().GetValue())

	require.Contains(t, byName, "mozia_vip_quota_consumed_total")
	assert.Equal(t, 5000.0, byName["mozia_vip_quota_consumed_total"].Metric[0].GetCounter().GetValue())

	require.Contains(t, byName, "mozia_relay_stream_outcomes_total")
	assert.Equal(t, 1.0, byName["mozia_relay_stream_outcomes_total"].Metric[0].GetCounter().GetValue())

	require.Contains(t, byName, "mozia_relay_channel_attempts_total")
	assert.Len(t, byName["mozia_relay_channel_attempts_total"].Metric, 2)

	require.Contains(t, byName, "mozia_billing_errors_total")
	require.Contains(t, byName, "mozia_payment_webhook_total")
	require.Contains(t, byName, "mozia_relay_gateway_overhead_seconds")
	require.Contains(t, byName, "mozia_relay_ttft_seconds")
	require.Contains(t, byName, "mozia_relay_tps")
}

func TestWrapBearerAuth(t *testing.T) {
	okHandler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	protected := wrapBearerAuth(okHandler, "secret-token")

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	protected.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	reqWrong := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	reqWrong.Header.Set("Authorization", "Bearer wrong")
	recWrong := httptest.NewRecorder()
	protected.ServeHTTP(recWrong, reqWrong)
	assert.Equal(t, http.StatusUnauthorized, recWrong.Code)

	reqValid := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	reqValid.Header.Set("Authorization", "Bearer secret-token")
	recValid := httptest.NewRecorder()
	protected.ServeHTTP(recValid, reqValid)
	assert.Equal(t, http.StatusOK, recValid.Code)
}
