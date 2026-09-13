package service

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/go-redis/redis/v8"
)

type SupplierRealtimeRow struct {
	Scope               string  `json:"scope"`
	SupplierID          int64   `json:"supplier_id"`
	PoolID              int64   `json:"pool_id"`
	Model               string  `json:"model"`
	ModelVersion        string  `json:"model_version"`
	GroupName           string  `json:"group_name"`
	IsStream            bool    `json:"is_stream"`
	RuleID              string  `json:"rule_id"`
	MinSamples          int64   `json:"min_samples"`
	Rank                int     `json:"rank"`
	Samples             int64   `json:"samples"`
	SuccessRate         float64 `json:"success_rate"`
	OverloadRate        float64 `json:"overload_rate"`
	Throughput          float64 `json:"throughput"`
	ThroughputSamples   int64   `json:"throughput_samples"`
	TTFTMs              float64 `json:"ttft_ms"`
	TTFTSamples         int64   `json:"ttft_samples"`
	State               string  `json:"state"`
	PerformanceState    string  `json:"performance_state"`
	ObservationMinutes  int64   `json:"observation_minutes"`
	AvailabilityRate    float64 `json:"availability_rate"`
	AvailabilitySamples int64   `json:"availability_samples"`
	RoutingReason       string  `json:"routing_reason"`
	DecisionAt          int64   `json:"decision_at"`
	TTFTPassRate        float64 `json:"ttft_pass_rate"`
	ThroughputPassRate  float64 `json:"throughput_pass_rate"`
}

// ReadSupplierRealtime is independent of SQL history and routing generations:
// opening a circuit must not erase the failures that an operator needs to see.
func ReadSupplierRealtime(ctx context.Context) ([]SupplierRealtimeRow, time.Time, error) {
	rows := []SupplierRealtimeRow{}
	now, err := common.RDB.Time(ctx).Result()
	if err != nil {
		return rows, now, err
	}
	keys, err := common.RDB.ZRangeByScore(ctx, "supplier-routing:live", &redis.ZRangeBy{Min: strconv.FormatInt(now.UnixMilli()-300000, 10), Max: "+inf"}).Result()
	if err != nil || len(keys) == 0 {
		return rows, now, err
	}
	pipe := common.RDB.Pipeline()
	for _, key := range keys {
		pipe.HGetAll(ctx, key)
		for i := int64(0); i < 5; i++ {
			pipe.HGetAll(ctx, fmt.Sprintf("%s:load:%d", key, now.Unix()/60-i))
		}
	}
	commands, err := pipe.Exec(ctx)
	if err != nil {
		return rows, now, err
	}
	for i, key := range keys {
		health := commands[i*6].(*redis.StringStringMapCmd).Val()
		if health["metadata"] == "" {
			continue
		}
		var row SupplierRealtimeRow
		if err := common.UnmarshalJsonStr(health["metadata"], &row); err != nil {
			return rows, now, err
		}
		row.Scope, row.State = key, health["state"]
		if row.State == "" {
			row.State = "trial"
		}
		row.MinSamples = max(1, row.MinSamples)
		sums := map[string]float64{}
		for _, command := range commands[i*6+1 : i*6+6] {
			for name, value := range command.(*redis.StringStringMapCmd).Val() {
				n, err := strconv.ParseFloat(value, 64)
				if err != nil {
					return rows, now, err
				}
				sums[name] += n
			}
		}
		row.Samples = int64(sums["success"] + sums["failure"] + sums["overload"])
		if row.Samples > 0 {
			row.SuccessRate = 100 * sums["success"] / float64(row.Samples)
			row.OverloadRate = 100 * sums["overload"] / float64(row.Samples)
		}
		row.ThroughputSamples, row.TTFTSamples = int64(sums["tps_n"]), int64(sums["ttft_n"])
		if row.ThroughputSamples > 0 {
			row.Throughput = sums["tps"] / sums["tps_n"]
			row.ThroughputPassRate = 100 * sums["tps_pass"] / sums["tps_n"]
		}
		if row.TTFTSamples > 0 {
			row.TTFTMs = sums["ttft"] / sums["ttft_n"]
			row.TTFTPassRate = 100 * sums["ttft_pass"] / sums["ttft_n"]
		}
		rows = append(rows, row)
	}
	// Lexicographic ranking is a display order, not a new routing score.
	// Compare only like-for-like model versions, groups, stream modes and rules.
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.Model != b.Model {
			return a.Model < b.Model
		}
		if a.ModelVersion != b.ModelVersion {
			return a.ModelVersion < b.ModelVersion
		}
		if a.GroupName != b.GroupName {
			return a.GroupName < b.GroupName
		}
		if a.IsStream != b.IsStream {
			return !a.IsStream
		}
		if a.RuleID != b.RuleID {
			return a.RuleID < b.RuleID
		}
		if (a.Samples >= a.MinSamples) != (b.Samples >= b.MinSamples) {
			return a.Samples >= a.MinSamples
		}
		if a.SuccessRate != b.SuccessRate {
			return a.SuccessRate > b.SuccessRate
		}
		if a.OverloadRate != b.OverloadRate {
			return a.OverloadRate < b.OverloadRate
		}
		if a.Throughput != b.Throughput {
			return a.Throughput > b.Throughput
		}
		if (a.TTFTSamples > 0) != (b.TTFTSamples > 0) {
			return a.TTFTSamples > 0
		}
		if a.TTFTMs != b.TTFTMs {
			return a.TTFTMs < b.TTFTMs
		}
		return a.PoolID < b.PoolID
	})
	rank := 0
	for i := range rows {
		a := &rows[i]
		if i > 0 {
			b := rows[i-1]
			if a.Model != b.Model || a.ModelVersion != b.ModelVersion || a.GroupName != b.GroupName || a.IsStream != b.IsStream || a.RuleID != b.RuleID {
				rank = 0
			}
		}
		if a.Samples >= a.MinSamples {
			rank++
			a.Rank = rank
		}
	}
	return rows, now, nil
}
