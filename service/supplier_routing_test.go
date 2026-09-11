package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSupplierRulePrecedenceAndCost(t *testing.T) {
	cfg := model.SupplierRoutingConfig{Rules: []model.SupplierRoutingRule{
		{ID: "default", Model: "test"}, {ID: "group", Model: "test", Group: "premium"}, {ID: "customer", Model: "test", UserID: 7},
	}}
	assert.Equal(t, "customer", MatchSupplierRule(&cfg, 7, "premium", "test").ID)
	assert.Equal(t, "group", MatchSupplierRule(&cfg, 8, "premium", "test").ID)
	assert.Nil(t, MatchSupplierRule(&cfg, 8, "premium", "other"))
	price := model.ChannelCostPricing{Mode: model.ChannelCostModePerToken, Currency: "CNY", Config: model.ChannelCostConfig{Items: map[string]float64{"input": 2, "output": 4, "cache_read": 0.5}}}
	raw, err := common.Marshal(price)
	require.NoError(t, err)
	usage := &dto.Usage{PromptTokens: 1000, CompletionTokens: 500}
	usage.PromptTokensDetails.CachedTokens = 200
	amount, err := SupplierCost(string(raw), usage)
	require.NoError(t, err)
	assert.Equal(t, "0.00370000", amount)
	_, err = SupplierCost(string(raw), nil)
	assert.ErrorContains(t, err, "unavailable")
}

// This integration fixture uses a dedicated disposable Redis database. The
// explicit environment variable keeps ordinary unit tests independent of Redis.
func supplierRedisFixture(t *testing.T) (*redis.Client, *SupplierRuntime) {
	t.Helper()
	address := os.Getenv("SUPPLIER_TEST_REDIS")
	if address == "" {
		t.Skip("set SUPPLIER_TEST_REDIS to a disposable Redis instance")
	}
	client := redis.NewClient(&redis.Options{Addr: address, DB: 15})
	require.NoError(t, client.FlushDB(context.Background()).Err())
	t.Cleanup(func() {
		require.NoError(t, client.FlushDB(context.Background()).Err())
		require.NoError(t, client.Close())
	})
	cfg := model.SupplierRoutingConfig{Suppliers: []model.Supplier{{ID: 1, Enabled: true}, {ID: 2, Enabled: true}, {ID: 3, Enabled: true}}}
	for i := int64(1); i <= 3; i++ {
		cfg.Pools = append(cfg.Pools, model.SupplierPool{ID: i, SupplierID: i, Enabled: true, Limits: model.SupplierLimits{Concurrency: 100, RPM: 1000, TPM: 100000}, MaxExecutionSeconds: 60, Models: []model.SupplierModelSpec{{Name: "test", Limits: model.SupplierLimits{}}}})
		cfg.Bindings = append(cfg.Bindings, model.SupplierBinding{ChannelID: int(i), PoolID: i, Model: "test"})
	}
	r := BuildSupplierRuntime(&cfg)
	raw, err := common.Marshal(r)
	require.NoError(t, err)
	require.NoError(t, client.Set(context.Background(), supplierRuntimeKey, string(raw), 0).Err())
	return client, r
}

func supplierReserveForTest(t *testing.T, client *redis.Client, id string, candidates []SupplierCandidate, mode string) *SupplierCandidate {
	t.Helper()
	health := DefaultSupplierHealth()
	health.TrialPercent = 100
	raw, err := common.Marshal(map[string]any{"id": id, "candidates": candidates, "model": "test", "group": "default", "health": health, "mode": mode, "schedule": "test", "timeout_seconds": 30})
	require.NoError(t, err)
	result, err := supplierAdmission.Run(context.Background(), client, []string{supplierRuntimeKey}, "acquire", string(raw)).Text()
	require.NoError(t, err)
	if result == "" {
		return nil
	}
	var selected SupplierCandidate
	require.NoError(t, common.UnmarshalJsonStr(result, &selected))
	return &selected
}

func supplierFinishForTest(t *testing.T, client *redis.Client, id string, cancel, release bool, tokens int) {
	t.Helper()
	raw, err := common.Marshal(map[string]any{"id": id, "cancel": cancel, "release": release, "tokens": tokens, "class": "success", "ttft": 100})
	require.NoError(t, err)
	require.NoError(t, supplierAdmission.Run(context.Background(), client, []string{supplierRuntimeKey}, "finish", string(raw)).Err())
}

func TestSupplierSharedCapacityAndIdempotency(t *testing.T) {
	client, r := supplierRedisFixture(t)
	pool := r.Pools["1"]
	pool.Limits = model.SupplierLimits{Concurrency: 1, RPM: 2, TPM: 1000}
	r.Pools["1"] = pool
	r.Bindings["10:test"] = 1
	raw, err := common.Marshal(r)
	require.NoError(t, err)
	require.NoError(t, client.Set(context.Background(), supplierRuntimeKey, string(raw), 0).Err())
	second := redis.NewClient(client.Options())
	defer second.Close()
	candidates := []SupplierCandidate{{ChannelID: 1, PoolID: 1, SupplierID: 1, Priority: 0, Weight: 1, Tokens: 600}}
	require.NotNil(t, supplierReserveForTest(t, client, "first", candidates, "capacity"))
	require.NotNil(t, supplierReserveForTest(t, second, "first", candidates, "capacity"))
	candidates[0].ChannelID = 10
	assert.Nil(t, supplierReserveForTest(t, second, "second", candidates, "capacity"), "another channel and gateway share the same concurrency")
	supplierFinishForTest(t, client, "first", false, true, 100)
	require.NotNil(t, supplierReserveForTest(t, second, "second", candidates, "capacity"))
	supplierFinishForTest(t, second, "second", false, true, 200)
	supplierFinishForTest(t, second, "second", true, true, 0)
	assert.Nil(t, supplierReserveForTest(t, client, "third", candidates, "capacity"), "completion does not refund RPM; duplicate finish cannot refund a real call")
}

func TestSupplierTokenReservationsCancelAndUnknownExecution(t *testing.T) {
	client, r := supplierRedisFixture(t)
	pool := r.Pools["1"]
	pool.Limits = model.SupplierLimits{Concurrency: 10, RPM: 10, TPM: 1000}
	r.Pools["1"] = pool
	raw, _ := common.Marshal(r)
	require.NoError(t, client.Set(context.Background(), supplierRuntimeKey, string(raw), 0).Err())
	c := []SupplierCandidate{{ChannelID: 1, PoolID: 1, SupplierID: 1, Weight: 1, Tokens: 600}}
	require.NotNil(t, supplierReserveForTest(t, client, "a", c, "capacity"))
	assert.Nil(t, supplierReserveForTest(t, client, "b", c, "capacity"))
	supplierFinishForTest(t, client, "a", true, true, 0)
	require.NotNil(t, supplierReserveForTest(t, client, "b", c, "capacity"))
	supplierFinishForTest(t, client, "b", false, false, -1)
	assert.Nil(t, supplierReserveForTest(t, client, "c", c, "capacity"), "unknown usage retains the token reservation")
}

func TestSupplierSharesAndPriorityFallback(t *testing.T) {
	client, r := supplierRedisFixture(t)
	r.Bindings["10:test"] = 1
	raw, _ := common.Marshal(r)
	require.NoError(t, client.Set(context.Background(), supplierRuntimeKey, string(raw), 0).Err())
	candidates := []SupplierCandidate{{ChannelID: 1, PoolID: 1, SupplierID: 1, Weight: 50, Tokens: 10}, {ChannelID: 10, PoolID: 1, SupplierID: 1, Weight: 50, Tokens: 10}, {ChannelID: 2, PoolID: 2, SupplierID: 2, Weight: 30, Tokens: 10}, {ChannelID: 3, PoolID: 3, SupplierID: 3, Weight: 20, Tokens: 10}}
	counts := map[int64]int{}
	for i := 0; i < 10; i++ {
		c := supplierReserveForTest(t, client, fmt.Sprint(i), candidates, "share")
		require.NotNil(t, c)
		counts[c.SupplierID]++
	}
	assert.Equal(t, map[int64]int{1: 5, 2: 3, 3: 2}, counts, "extra channels must not increase supplier share")
	candidates[0].Priority = 10
	candidates[1].Priority = 10
	pool := r.Pools["1"]
	pool.Enabled = false
	r.Pools["1"] = pool
	raw, _ = common.Marshal(r)
	require.NoError(t, client.Set(context.Background(), supplierRuntimeKey, string(raw), 0).Err())
	selected := supplierReserveForTest(t, client, "fallback", candidates, "failover")
	require.NotNil(t, selected)
	assert.NotEqual(t, int64(1), selected.SupplierID)
}

func TestSupplierPreviewAndConcentrationDoNotResetOnRevision(t *testing.T) {
	client, _ := supplierRedisFixture(t)
	ctx := context.Background()
	health := DefaultSupplierHealth()
	health.TrialPercent = 100
	input := map[string]any{"id": "first", "model": "test", "group": "default", "health": health, "mode": "share", "schedule": "revision-1", "share_scope": "same-policy", "first": true, "max_supplier_percent": 50, "timeout_seconds": 30, "candidates": []SupplierCandidate{{ChannelID: 1, PoolID: 1, SupplierID: 1, Weight: 1, Tokens: 10}}}
	raw, err := common.Marshal(input)
	require.NoError(t, err)
	preview, err := supplierAdmission.Run(ctx, client, []string{supplierRuntimeKey}, "preview", string(raw)).Text()
	require.NoError(t, err)
	require.NotEmpty(t, preview)
	assert.Equal(t, int64(0), client.ZCard(ctx, "supplier-routing:pool:1:active").Val())
	assert.Equal(t, int64(0), client.Exists(ctx, "supplier-routing:attempt:first").Val())
	require.NoError(t, supplierAdmission.Run(ctx, client, []string{supplierRuntimeKey}, "acquire", string(raw)).Err())
	input["id"], input["schedule"] = "second", "revision-2"
	raw, err = common.Marshal(input)
	require.NoError(t, err)
	result, err := supplierAdmission.Run(ctx, client, []string{supplierRuntimeKey}, "acquire", string(raw)).Text()
	require.NoError(t, err)
	assert.Empty(t, result, "a new routing revision cannot reset this hour's concentration cap")
	supplierFinishForTest(t, client, "first", true, true, 0)
	result, err = supplierAdmission.Run(ctx, client, []string{supplierRuntimeKey}, "acquire", string(raw)).Text()
	require.NoError(t, err)
	assert.NotEmpty(t, result, "an unsent request does not consume the first-dispatch share")
}

func TestSupplierHealthQuarantineIgnoresOldInflightSuccess(t *testing.T) {
	client, _ := supplierRedisFixture(t)
	ctx := context.Background()
	health := DefaultSupplierHealth()
	health.MinSamples, health.TrialPercent = 1, 10
	input := map[string]any{"id": "bad", "model": "test", "group": "default", "health": health, "mode": "capacity", "schedule": "test", "timeout_seconds": 30, "candidates": []SupplierCandidate{{ChannelID: 1, PoolID: 1, SupplierID: 1, Weight: 1, Tokens: 50000}}}
	for _, id := range []string{"bad", "old-success"} {
		input["id"] = id
		raw, err := common.Marshal(input)
		require.NoError(t, err)
		result, err := supplierAdmission.Run(ctx, client, []string{supplierRuntimeKey}, "acquire", string(raw)).Text()
		require.NoError(t, err)
		require.NotEmpty(t, result, "trial must admit a valid request within hard TPM")
	}
	finish, err := common.Marshal(map[string]any{"id": "bad", "cancel": false, "release": true, "tokens": 10, "class": "failure", "ttft": 0})
	require.NoError(t, err)
	require.NoError(t, supplierAdmission.Run(ctx, client, []string{supplierRuntimeKey}, "finish", string(finish)).Err())
	key := "supplier-routing:health:1:4:test:default"
	assert.Equal(t, "paused", client.HGet(ctx, key, "state").Val())
	candidates := []SupplierCandidate{{ChannelID: 1, PoolID: 1, SupplierID: 1, Weight: 1, Tokens: 10}}
	assert.Nil(t, supplierReserveForTest(t, client, "blocked", candidates, "capacity"))
	// Advance the persisted cooldown deadline without sleeps or wall-clock races.
	require.NoError(t, client.HSet(ctx, key, "until", 0).Err())
	require.NotNil(t, supplierReserveForTest(t, client, "trial", candidates, "capacity"))
	supplierFinishForTest(t, client, "old-success", false, true, 10)
	assert.Equal(t, "trial", client.HGet(ctx, key, "state").Val(), "a result from before quarantine must not restore normal routing")
	assert.Equal(t, "0", client.HGet(ctx, key, "samples").Val())
}

func TestSupplierConcurrentGatewaysAndImmediateCapacityReduction(t *testing.T) {
	client, runtime := supplierRedisFixture(t)
	ctx := context.Background()
	pool := runtime.Pools["1"]
	pool.Limits.Concurrency = 1
	runtime.Pools["1"] = pool
	raw, err := common.Marshal(runtime)
	require.NoError(t, err)
	require.NoError(t, client.Set(ctx, supplierRuntimeKey, raw, 0).Err())
	second := redis.NewClient(client.Options())
	t.Cleanup(func() { require.NoError(t, second.Close()) })
	start := make(chan struct{})
	type response struct {
		value string
		err   error
	}
	results := make(chan response, 2)
	for i, gateway := range []*redis.Client{client, second} {
		health := DefaultSupplierHealth()
		health.TrialPercent = 100
		input, err := common.Marshal(map[string]any{"id": fmt.Sprint(i), "model": "test", "group": "default", "health": health, "mode": "capacity", "schedule": "test", "timeout_seconds": 30, "candidates": []SupplierCandidate{{ChannelID: 1, PoolID: 1, SupplierID: 1, Weight: 1, Tokens: 10}}})
		require.NoError(t, err)
		go func(client *redis.Client, data []byte) {
			<-start
			value, err := supplierAdmission.Run(ctx, client, []string{supplierRuntimeKey}, "acquire", string(data)).Text()
			results <- response{value, err}
		}(gateway, input)
	}
	close(start)
	admitted := 0
	for i := 0; i < 2; i++ {
		result := <-results
		require.NoError(t, result.err)
		if result.value != "" {
			admitted++
		}
	}
	assert.Equal(t, 1, admitted, "two gateway processes cannot sell the final slot twice")
	pool.Limits.Concurrency = 2
	runtime.Pools["1"] = pool
	raw, err = common.Marshal(runtime)
	require.NoError(t, err)
	require.NoError(t, client.Set(ctx, supplierRuntimeKey, raw, 0).Err())
	candidates := []SupplierCandidate{{ChannelID: 1, PoolID: 1, SupplierID: 1, Weight: 1, Tokens: 10}}
	require.NotNil(t, supplierReserveForTest(t, client, "expanded", candidates, "capacity"))
	pool.Limits.Concurrency = 1
	runtime.Pools["1"] = pool
	raw, err = common.Marshal(runtime)
	require.NoError(t, err)
	require.NoError(t, client.Set(ctx, supplierRuntimeKey, raw, 0).Err())
	assert.Nil(t, supplierReserveForTest(t, second, "after-reduction", candidates, "capacity"))
	assert.Equal(t, int64(2), client.ZCard(ctx, "supplier-routing:pool:1:active").Val(), "lowering capacity preserves in-flight reservations")
}

func TestSupplierFinalGuardAndProcurementReconciliation(t *testing.T) {
	client, runtime := supplierRedisFixture(t)
	db := setupMaterialServiceTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.SupplierAttempt{}, &model.ChannelCostPricing{}))
	oldRedis, oldEnabled, oldCache := common.RDB, common.RedisEnabled, common.MemoryCacheEnabled
	common.RDB, common.RedisEnabled, common.MemoryCacheEnabled = client, true, false
	t.Cleanup(func() { common.RDB, common.RedisEnabled, common.MemoryCacheEnabled = oldRedis, oldEnabled, oldCache })
	ch := model.Channel{Id: 1, SupplierID: 1, Type: constant.ChannelTypeOpenAI, Name: "managed", Models: "test", Status: common.ChannelStatusEnabled}
	require.NoError(t, db.Create(&ch).Error)
	pool := runtime.Config.Pools[0]
	pool.InputSafetyPercent = 110
	pool.Models = []model.SupplierModelSpec{{Name: "test", Version: "v1", ContextTokens: 1000, MaxOutputTokens: 100}}
	runtime.Config.Pools[0] = pool
	runtime = BuildSupplierRuntime(&runtime.Config)
	raw, err := common.Marshal(runtime)
	require.NoError(t, err)
	require.NoError(t, client.Set(context.Background(), supplierRuntimeKey, raw, 0).Err())
	price := model.ChannelCostPricing{ChannelId: 1, ModelName: "test", Mode: model.ChannelCostModePerToken, Currency: "CNY", ConfigJson: `{"items":{"input":2,"output":4}}`}
	require.NoError(t, db.Create(&price).Error)
	for _, test := range []struct {
		name, body string
		complete   bool
		reject     bool
	}{
		{"success", `{"model":"test","messages":[{"role":"user","content":"hello"}],"stream":true,"temperature":0}`, true, false},
		{"unknown", `{"model":"test","messages":[{"role":"user","content":"hello"}]}`, false, false},
		{"override-rejected", `{"model":"unaccepted","messages":[{"role":"user","content":"hello"}]}`, false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			c.Set("channel_id", 1)
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 1, ChannelType: constant.ChannelTypeOpenAI, UpstreamModelName: "test"}, OriginModelName: "test", UsingGroup: "default", RelayFormat: types.RelayFormatOpenAI, RelayMode: relayconstant.RelayModeChatCompletions}
			prepared, err := PrepareSupplierRequest(c, info, strings.NewReader(test.body))
			if test.reject {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			body, err := io.ReadAll(prepared)
			require.NoError(t, err)
			var decoded map[string]any
			require.NoError(t, common.Unmarshal(body, &decoded))
			assert.Equal(t, float64(100), decoded["max_tokens"], "a missing client maximum must be bounded on the wire")
			if test.complete {
				assert.Equal(t, float64(0), decoded["temperature"])
				assert.Equal(t, map[string]any{"include_usage": true}, decoded["stream_options"])
				ObserveSupplierResponse(c, info, []byte(`{"id":"upstream-1","choices":[{"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`), false)
			}
			state := SupplierRoutingState(c)
			t.Cleanup(state.Cancel)
			_, bounded := c.Request.Context().Deadline()
			assert.True(t, bounded, "direct probes need the same total deadline")
			require.NotNil(t, state.Current)
			FinishSupplierAttempt(c, info, nil, false)
			FinishSupplierAttempt(c, info, nil, true)
			var attempt model.SupplierAttempt
			require.NoError(t, db.First(&attempt, state.Current.ID).Error)
			if test.complete {
				assert.Equal(t, "success", attempt.Status)
				assert.Equal(t, "calculated", attempt.CostStatus)
				assert.Equal(t, "0.00004000", attempt.Cost)
				assert.Equal(t, "upstream-1", attempt.UpstreamRequestID)
			} else {
				assert.Equal(t, "unknown", attempt.Status)
				assert.Equal(t, "pending", attempt.CostStatus)
				assert.Empty(t, attempt.Cost)
				assert.Equal(t, int64(1), client.ZCard(context.Background(), "supplier-routing:pool:1:active").Val(), "unknown execution must retain capacity")
			}
		})
	}
}
