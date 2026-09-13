package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSupplierRealtimeActualCallsAndCooldown(t *testing.T) {
	client, _ := supplierRedisFixture(t)
	ctx := context.Background()
	oldClient := common.RDB
	common.RDB = client
	t.Cleanup(func() { common.RDB = oldClient })
	candidates := []SupplierCandidate{{ChannelID: 1, PoolID: 1, SupplierID: 1, Tokens: 1, Measure: true, Streaming: true, ModelVersion: "v1", RuleID: "test-rule", PerformanceKey: "live:test", OutputScope: "live:output"}}
	require.NotNil(t, adaptiveAdmission(t, client, "preview", "preview", candidates))
	rows, _, err := ReadSupplierRealtime(ctx)
	require.NoError(t, err)
	assert.Empty(t, rows, "recommendations cannot enter real-time rankings")
	candidates[0].Measure = false
	require.NotNil(t, adaptiveAdmission(t, client, "probe", "acquire", candidates))
	supplierFinishForTest(t, client, "probe", false, true, 1)
	rows, _, err = ReadSupplierRealtime(ctx)
	require.NoError(t, err)
	assert.Empty(t, rows, "probes cannot enter real-time rankings")
	candidates[0].Measure = true
	require.NotNil(t, adaptiveAdmission(t, client, "actual", "acquire", candidates))
	rows, _, err = ReadSupplierRealtime(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Zero(t, rows[0].Rank, "in-flight calls have no completed quality samples")
	assert.Equal(t, "v1", rows[0].ModelVersion)
	assert.Equal(t, "test-rule", rows[0].RuleID)
	assert.True(t, rows[0].IsStream)
	raw, err := common.Marshal(map[string]any{"id": "actual", "cancel": false, "release": true, "tokens": 1, "class": "overload", "ttft": 0, "latency": 10, "output": 0})
	require.NoError(t, err)
	for i := 0; i < 2; i++ {
		require.NoError(t, supplierAdmission.Run(ctx, client, []string{supplierRuntimeKey}, "finish", string(raw)).Err())
	}
	rows, _, err = ReadSupplierRealtime(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, int64(1), rows[0].Samples, "duplicate completion is not counted twice")
	assert.Equal(t, float64(100), rows[0].OverloadRate)
	assert.Zero(t, rows[0].SuccessRate)
	assert.Equal(t, "overloaded", rows[0].State, "generation reset must retain the recent failure")
}

func TestSupplierRealtimeWindowAndComparableRanks(t *testing.T) {
	client, _ := supplierRedisFixture(t)
	ctx := context.Background()
	oldClient := common.RDB
	common.RDB = client
	t.Cleanup(func() { common.RDB = oldClient })
	now, err := client.Time(ctx).Result()
	require.NoError(t, err)
	fixtures := []struct {
		row                                    SupplierRealtimeRow
		success, failure, overload, throughput int
	}{
		{SupplierRealtimeRow{PoolID: 1}, 8, 2, 0, 100},
		{SupplierRealtimeRow{PoolID: 2}, 9, 0, 1, 100},
		{SupplierRealtimeRow{PoolID: 3}, 10, 0, 0, 50},
		{SupplierRealtimeRow{PoolID: 4}, 10, 0, 0, 100},
		{SupplierRealtimeRow{PoolID: 5}, 1, 0, 0, 1000},
		{SupplierRealtimeRow{PoolID: 6, IsStream: true}, 10, 0, 0, 100},
		{SupplierRealtimeRow{PoolID: 7, ModelVersion: "v2"}, 10, 0, 0, 100},
		{SupplierRealtimeRow{PoolID: 8, GroupName: "vip"}, 10, 0, 0, 100},
		{SupplierRealtimeRow{PoolID: 9, Model: "other"}, 10, 0, 0, 100},
		{SupplierRealtimeRow{PoolID: 10, RuleID: "other-rule"}, 10, 0, 0, 100},
	}
	for _, fixture := range fixtures {
		row := fixture.row
		row.MinSamples = 10
		if row.Model == "" {
			row.Model = "test"
		}
		key := fmt.Sprintf("live:%d", row.PoolID)
		raw, err := common.Marshal(row)
		require.NoError(t, err)
		require.NoError(t, client.HSet(ctx, key, "metadata", string(raw)).Err())
		require.NoError(t, client.ZAdd(ctx, "supplier-routing:live", &redis.Z{Score: float64(now.UnixMilli()), Member: key}).Err())
		// Previous complete minute avoids a boundary race in the fixture clock.
		require.NoError(t, client.HSet(ctx, fmt.Sprintf("%s:load:%d", key, now.Unix()/60-1), "success", fixture.success, "failure", fixture.failure, "overload", fixture.overload, "tps", fixture.throughput*fixture.success, "tps_n", fixture.success, "ttft", fixture.success*100, "ttft_n", fixture.success).Err())
		require.NoError(t, client.HSet(ctx, fmt.Sprintf("%s:load:%d", key, now.Unix()/60-6), "failure", 999).Err())
	}
	require.NoError(t, client.HSet(ctx, "expired", "metadata", `{"pool_id":99}`).Err())
	require.NoError(t, client.ZAdd(ctx, "supplier-routing:live", &redis.Z{Score: float64(now.UnixMilli() - 600000), Member: "expired"}).Err())
	rows, _, err := ReadSupplierRealtime(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 10)
	ranks := map[int64]int{}
	for _, row := range rows {
		ranks[row.PoolID] = row.Rank
		if row.PoolID != 5 {
			assert.Equal(t, int64(10), row.Samples, "expired buckets must be excluded")
		}
		if row.PoolID == 2 {
			assert.Equal(t, float64(90), row.SuccessRate)
			assert.Equal(t, float64(10), row.OverloadRate)
		}
	}
	assert.Equal(t, map[int64]int{1: 4, 2: 3, 3: 2, 4: 1, 5: 0, 6: 1, 7: 1, 8: 1, 9: 1, 10: 1}, ranks)
}
