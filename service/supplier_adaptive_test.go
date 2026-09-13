package service

import (
	"context"
	"fmt"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func adaptiveAdmission(t *testing.T, client *redis.Client, id, op string, candidates []SupplierCandidate) *SupplierCandidate {
	t.Helper()
	data, err := common.Marshal(map[string]any{"id": id, "revision": 0, "candidates": candidates, "model": "test", "group": "default", "health": model.DefaultSupplierAdaptiveHealth(), "mode": "adaptive", "schedule": "adaptive-test", "timeout_seconds": 30, "first": true})
	require.NoError(t, err)
	raw, err := supplierAdmission.Run(context.Background(), client, []string{supplierRuntimeKey}, op, string(data)).Text()
	require.NoError(t, err)
	if raw == "" {
		return nil
	}
	var selected SupplierCandidate
	require.NoError(t, common.UnmarshalJsonStr(raw, &selected))
	return &selected
}

func TestAdaptiveCostRoutingCapacityAndPreview(t *testing.T) {
	client, runtime := supplierRedisFixture(t)
	ctx := context.Background()
	now, err := client.Time(ctx).Result()
	require.NoError(t, err)
	for id, pool := range runtime.Pools {
		pool.Limits = model.SupplierLimits{}
		runtime.Pools[id] = pool
		require.NoError(t, client.HSet(ctx, "performance:"+id, "state", "normal").Err())
		require.NoError(t, client.HSet(ctx, fmt.Sprintf("performance:%s:g0:%d", id, now.Unix()/60), "success", 20, "ttft", 20000, "ttft_n", 20, "tps", 2000, "tps_n", 20).Err())
	}
	runtime.Bindings["11:test"] = 1
	runtime.Bindings["12:test"] = 1
	raw, err := common.Marshal(runtime)
	require.NoError(t, err)
	require.NoError(t, client.Set(ctx, supplierRuntimeKey, string(raw), 0).Err())
	candidates := []SupplierCandidate{
		{ChannelID: 1, PoolID: 1, SupplierID: 1, Cost: 1, Tokens: 20, PerformanceKey: "performance:1"},
		{ChannelID: 2, PoolID: 2, SupplierID: 2, Cost: 2, Tokens: 20, Priority: 1000, PerformanceKey: "performance:2"},
		{ChannelID: 11, PoolID: 1, SupplierID: 1, Cost: 1, Tokens: 20, PerformanceKey: "performance:1"},
		{ChannelID: 12, PoolID: 1, SupplierID: 1, Cost: 3, Tokens: 20, PerformanceKey: "performance:1"},
	}
	preview := adaptiveAdmission(t, client, "preview", "preview", candidates)
	require.NotNil(t, preview)
	assert.Equal(t, 1, preview.ChannelID)
	assert.Zero(t, client.Exists(ctx, "supplier-routing:attempt:preview", "supplier-routing:schedule:adaptive-test", "supplier-routing:schedule:adaptive-test:exploration").Val())
	counts := map[int]int{}
	for i := 0; i < 20; i++ {
		id := fmt.Sprint(i)
		c := adaptiveAdmission(t, client, id, "acquire", candidates)
		require.NotNil(t, c)
		counts[c.ChannelID]++
		supplierFinishForTest(t, client, id, false, true, 20)
	}
	assert.Equal(t, map[int]int{1: 8, 2: 4, 11: 8}, counts, "inverse-square pool weights ignore channel priority and duplicates; cheapest tied channels rotate evenly")
	// Speed is a gate: a cheaper but slow candidate cannot displace a healthy one.
	require.NoError(t, client.HSet(ctx, fmt.Sprintf("performance:1:g0:%d", now.Unix()/60), "tps", 20).Err())
	assert.Equal(t, 2, adaptiveAdmission(t, client, "slow", "preview", candidates).ChannelID)
	require.NoError(t, client.HSet(ctx, fmt.Sprintf("performance:1:g0:%d", now.Unix()/60), "tps", 2000).Err())
	// A declared cap still wins over lower cost; undeclared dimensions remain usable.
	pool := runtime.Pools["1"]
	pool.Limits.RPM = 20
	runtime.Pools["1"] = pool
	raw, err = common.Marshal(runtime)
	require.NoError(t, err)
	require.NoError(t, client.Set(ctx, supplierRuntimeKey, string(raw), 0).Err())
	for i := 20; i < 28; i++ {
		id := fmt.Sprint(i)
		c := adaptiveAdmission(t, client, id, "acquire", candidates)
		require.NotNil(t, c)
		supplierFinishForTest(t, client, id, false, true, 20)
	}
	assert.Equal(t, 2, adaptiveAdmission(t, client, "capped", "acquire", candidates).ChannelID)

}

func TestAdaptiveColdStartOverloadAndZeroPrice(t *testing.T) {
	client, _ := supplierRedisFixture(t)
	ctx := context.Background()
	candidates := []SupplierCandidate{{ChannelID: 1, PoolID: 1, SupplierID: 1, Cost: 0, Tokens: 1, PerformanceKey: "performance:cold", OutputScope: "supplier-routing:output:test", Measure: true}}
	selected := adaptiveAdmission(t, client, "first", "acquire", candidates)
	require.NotNil(t, selected)
	assert.Equal(t, "trial", selected.HealthState)
	assert.Nil(t, adaptiveAdmission(t, client, "second", "acquire", candidates), "trial concurrency protects an unmeasured pool")
	data, err := common.Marshal(map[string]any{"id": "first", "cancel": false, "release": true, "tokens": 1, "class": "overload", "ttft": 0, "latency": 10, "output": 0, "retry_after": 60})
	require.NoError(t, err)
	require.NoError(t, supplierAdmission.Run(ctx, client, []string{supplierRuntimeKey}, "finish", string(data)).Err())
	assert.Nil(t, adaptiveAdmission(t, client, "overloaded", "preview", candidates))
	oldClient := common.RDB
	common.RDB = client
	t.Cleanup(func() { common.RDB = oldClient })
	views, _, err := ReadSupplierRealtime(ctx)
	require.NoError(t, err)
	require.Len(t, views, 1)
	view := views[0]
	assert.Equal(t, "overloaded", view.State)
	assert.Equal(t, float64(100), view.OverloadRate, "cooldown must not erase the visible overload rate")
	// Move the fixture's cooldown into the past; no sleeps or timing races.
	require.NoError(t, client.HSet(ctx, "performance:cold", "until", 0).Err())
	assert.Equal(t, "trial", adaptiveAdmission(t, client, "recovered", "acquire", candidates).HealthState)
	supplierFinishForTest(t, client, "recovered", true, true, 0)
	now, err := client.Time(ctx).Result()
	require.NoError(t, err)
	require.NoError(t, client.HSet(ctx, fmt.Sprintf("performance:cold:g1:%d", now.Unix()/60), "success", 20, "tps", 1000, "tps_n", 20).Err())
	assert.Equal(t, "normal", adaptiveAdmission(t, client, "zero", "acquire", candidates).HealthState)
}

func TestAdaptivePricePublicationAndOptionalPoolLimits(t *testing.T) {
	db := supplierResourceFixture(t)
	t.Setenv("LOG_SQL_DSN", "")
	oldLog, oldLogType := model.LOG_DB, common.LogDatabaseType()
	require.NoError(t, model.InitLogDB())
	t.Cleanup(func() { model.LOG_DB = oldLog; common.SetLogDatabaseType(oldLogType) })
	require.NoError(t, db.AutoMigrate(&model.ChannelCostPricing{}))
	supplier := createSupplierResourceForTest(t, "supplier", "adaptive-owner", `{"name":"Adaptive","enabled":true}`, 0).Resource.(*model.Supplier)
	ch := model.Channel{Name: "Quoted", Models: "test", Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled}
	require.NoError(t, db.Create(&ch).Error)
	other := model.Channel{Name: "Cheap input, expensive output", Models: "test", Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled}
	require.NoError(t, db.Create(&other).Error)
	require.NoError(t, db.AutoMigrate(&model.Ability{}))
	require.NoError(t, db.Create(&[]model.Ability{{Group: "default", Model: "test", ChannelId: ch.Id, Enabled: true}, {Group: "default", Model: "test", ChannelId: other.Id, Enabled: true}}).Error)
	body := fmt.Sprintf(`{"name":"Optional limits","failure_domain":"dc-a","enabled":true,"max_execution_seconds":60,"input_safety_percent":110,"acceptance":"manual verification","models":[{"name":"test","version":"v1","context_tokens":1000,"max_output_tokens":100,"limits":{"rpm":10}}],"bindings":[{"channel_id":%d,"model":"test"},{"channel_id":%d,"model":"test"}]}`, ch.Id, other.Id)
	pool := createSupplierResourceForTest(t, "pool", "adaptive-pool", body, supplier.ID).Resource.(*model.SupplierPool)
	assert.Zero(t, pool.Limits.RPM)
	assert.Equal(t, int64(10), pool.Models[0].Limits.RPM)
	cost := model.ChannelCostPricing{ChannelId: ch.Id, ModelName: "test", Currency: "CNY", Mode: model.ChannelCostModePerToken, Config: model.ChannelCostConfig{Items: map[string]float64{"input": 1, "output": 2}}}
	_, err := MutateSupplierCost(context.Background(), &cost, 0, 7)
	require.NoError(t, err)
	otherCost := model.ChannelCostPricing{ChannelId: other.Id, ModelName: "test", Currency: "CNY", Mode: model.ChannelCostModePerToken, Config: model.ChannelCostConfig{Items: map[string]float64{"input": 0, "output": 20}}}
	_, err = MutateSupplierCost(context.Background(), &otherCost, 0, 7)
	require.NoError(t, err)
	rule := model.SupplierRoutingRule{Model: "test", Mode: "adaptive", Targets: []model.SupplierTarget{{SupplierID: supplier.ID, Weight: 100}}, MaxAttempts: 2, TimeoutSeconds: 120, Health: model.DefaultSupplierAdaptiveHealth()}
	// Resource APIs do not accept internal metadata or read-only fields.
	patch, err := common.Marshal(map[string]any{"model": rule.Model, "mode": rule.Mode, "targets": rule.Targets, "max_attempts": rule.MaxAttempts, "timeout_seconds": rule.TimeoutSeconds, "health": rule.Health})
	require.NoError(t, err)
	created := createSupplierResourceForTest(t, "rule", "adaptive-rule", string(patch), 0)
	runtime, err := ReadSupplierRuntime(context.Background())
	require.NoError(t, err)
	require.Len(t, runtime.Config.Prices, 2)
	assert.Equal(t, float64(2), runtime.Config.Prices[0].Config.Items["output"])
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	state := &SupplierRouteState{Runtime: runtime, Rule: runtime.Config.Rules[0], Managed: true, Group: "default", RequestID: "snapshot-call", Excluded: map[int]bool{}, ExcludedPools: map[int64]bool{}, ExcludedDomains: map[string]bool{}}
	c.Set(supplierStateKey, state)
	info := &relaycommon.RelayInfo{OriginModelName: "test", Request: &dto.GeneralOpenAIRequest{Model: "test", Messages: []dto.Message{{Role: "user", Content: "hello"}}}}
	selected, err := SelectSupplierChannel(c, info, nil)
	require.NoError(t, err)
	assert.Equal(t, ch.Id, selected.Id, "compare shared input AND output usage; a cheaper input price is insufficient")
	assert.Contains(t, state.Current.PriceJSON, `"output":2`)
	cost.Config.Items["output"] = 4
	saved, err := MutateSupplierCost(context.Background(), &cost, 0, 7)
	require.NoError(t, err)
	assert.Greater(t, saved.Revision, created.Revision)
	assert.Equal(t, "applied", saved.Application)
	runtime, err = ReadSupplierRuntime(context.Background())
	require.NoError(t, err)
	assert.Equal(t, float64(4), runtime.Config.Prices[0].Config.Items["output"])
	state.Sent = true
	ObserveSupplierResponse(c, info, []byte(`{"choices":[{"finish_reason":"stop"}],"usage":{"prompt_tokens":100,"completion_tokens":50,"total_tokens":150}}`), false)
	FinishSupplierAttempt(c, info, nil, false)
	assert.Equal(t, "0.00020000", state.Current.Cost, "in-flight cost keeps the selected quote after a price edit")
	assert.Less(t, state.Current.Revision, saved.Revision)
	_, err = MutateSupplierCost(context.Background(), nil, otherCost.Id, 7)
	require.NoError(t, err)
	_, err = MutateSupplierCost(context.Background(), nil, cost.Id, 7)
	assert.ErrorContains(t, err, "procurement quote", "cannot delete the only usable quote under an active adaptive rule")
	var persisted model.ChannelCostPricing
	require.NoError(t, db.First(&persisted, cost.Id).Error)
}

func TestAdaptiveTrialsAreSharedAndFaultsRecover(t *testing.T) {
	client, _ := supplierRedisFixture(t)
	ctx := context.Background()
	now, err := client.Time(ctx).Result()
	require.NoError(t, err)
	key := "performance:healthy"
	bucket := fmt.Sprintf("%s:g0:%d", key, now.Unix()/60)
	require.NoError(t, client.HSet(ctx, bucket, "success", 20, "tps", 2000, "tps_n", 20).Err())
	candidates := []SupplierCandidate{
		{ChannelID: 1, PoolID: 1, SupplierID: 1, Cost: 1, Tokens: 1, PerformanceKey: key},
		{ChannelID: 2, PoolID: 2, SupplierID: 2, Cost: 0, Tokens: 1, PerformanceKey: "performance:new-2"},
		{ChannelID: 3, PoolID: 3, SupplierID: 3, Cost: 0, Tokens: 1, PerformanceKey: "performance:new-3"},
	}
	counts := map[int64]int{}
	for i := 0; i < 40; i++ {
		id := fmt.Sprintf("trial-budget-%d", i)
		selected := adaptiveAdmission(t, client, id, "acquire", candidates)
		require.NotNil(t, selected)
		counts[selected.PoolID]++
		supplierFinishForTest(t, client, id, false, true, 1)
	}
	assert.Equal(t, map[int64]int{1: 38, 2: 1, 3: 1}, counts, "all new pools share one 5% trial budget, even free pools")
	candidates[0].Measure = true
	for i := 0; i < 3; i++ {
		id := fmt.Sprintf("failure-%d", i)
		require.NotNil(t, adaptiveAdmission(t, client, id, "acquire", candidates[:1]))
		data, err := common.Marshal(map[string]any{"id": id, "class": "failure", "release": true, "tokens": -1, "ttft": 0})
		require.NoError(t, err)
		require.NoError(t, supplierAdmission.Run(ctx, client, []string{supplierRuntimeKey}, "finish", string(data)).Err())
	}
	assert.Nil(t, adaptiveAdmission(t, client, "fault-paused", "preview", candidates[:1]))
	require.NoError(t, client.HSet(ctx, key, "until", 0).Err())
	assert.Equal(t, "trial", adaptiveAdmission(t, client, "fault-recovery", "preview", candidates[:1]).HealthState)
	// Old healthy samples cannot bypass a new recovery generation.
	assert.Equal(t, "1", client.HGet(ctx, key, "generation").Val())
	// Stale publications must not reserve capacity using an outdated quote.
	data, err := common.Marshal(map[string]any{"id": "stale", "revision": 99, "mode": "adaptive"})
	require.NoError(t, err)
	err = supplierAdmission.Run(ctx, client, []string{supplierRuntimeKey}, "acquire", string(data)).Err()
	assert.ErrorContains(t, err, "revision changed")
	assert.Zero(t, client.Exists(ctx, "supplier-routing:attempt:stale").Val())
}

func TestAdaptiveOutcomeAttribution(t *testing.T) {
	client, _ := supplierRedisFixture(t)
	oldClient := common.RDB
	common.RDB = client
	t.Cleanup(func() { common.RDB = oldClient })
	for _, tc := range []struct {
		name      string
		code      int
		cancelled bool
		class     string
	}{
		{"auth", 401, false, "failure"}, {"billing", 402, false, "failure"}, {"model", 404, false, "failure"},
		{"input", 400, false, "excluded"}, {"overload", 429, false, "overload"}, {"timeout", 504, false, "failure"},
		{"platform-budget", 504, true, "excluded"},
		{"completed-client-disconnect", 0, true, "success"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			if tc.cancelled {
				ctx, cancel := context.WithDeadline(c.Request.Context(), time.Unix(0, 0))
				defer cancel()
				c.Request = c.Request.WithContext(ctx)
			}
			state := &SupplierRouteState{ReservationID: tc.name, Sent: true, Started: time.Now(), Current: &model.SupplierAttempt{}, Excluded: map[int]bool{}, ExcludedPools: map[int64]bool{}, ExcludedDomains: map[string]bool{}, Runtime: &SupplierRuntime{Pools: map[string]model.SupplierPool{}}}
			c.Set(supplierStateKey, state)
			var apiErr *types.NewAPIError
			if tc.code != 0 {
				apiErr = types.NewErrorWithStatusCode(assert.AnError, types.ErrorCodeBadResponseBody, tc.code)
			} else {
				state.Complete = true
			}
			FinishSupplierAttempt(c, &relaycommon.RelayInfo{}, apiErr, false)
			assert.Equal(t, tc.class, state.Current.OutcomeClass)
		})
	}
}
