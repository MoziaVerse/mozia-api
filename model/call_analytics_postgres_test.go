package model

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestCallAnalyticsPostgresUnicode(t *testing.T) {
	dsn := os.Getenv("CALL_ANALYTICS_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("CALL_ANALYTICS_TEST_POSTGRES_DSN is not set")
	}
	pg, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	connection, err := pg.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = connection.Close() })
	originalLogDB := LOG_DB
	originalMainType, originalLogType := common.MainDatabaseType(), common.LogDatabaseType()
	common.SetDatabaseTypes(originalMainType, common.DatabaseTypePostgreSQL)
	t.Cleanup(func() {
		LOG_DB = originalLogDB
		common.SetDatabaseTypes(originalMainType, originalLogType)
	})

	for _, tc := range []struct {
		name, metadata, requested string
	}{
		{"excluded NUL", `"requested_model":"public-model","prompt":"private_marker\u0000"`, "public-model"},
		{"consecutive NUL", `"requested_model":"public-model","prompt":"private_marker\u0000\u0000"`, "public-model"},
		{"literal NUL spelling", `"requested_model":"literal\\u0000","prompt":"private_marker\u0000"`, `literal\u0000`},
		{"three backslashes", `"requested_model":"literal\\\u0000","prompt":"private_marker"`, "literal\\"},
		{"four backslashes", `"requested_model":"literal\\\\u0000","prompt":"private_marker\u0000"`, `literal\\u0000`},
		{"valid Unicode pair", `"requested_model":"模型-\ud83d\ude00","prompt":"private_marker\u0000"`, "模型-😀"},
		{"nested redirect", `"admin_info":{"requested_model":"public-model","debug":"private_marker\u0000"}`, "public-model"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			other := fmt.Sprintf(`{"request_path":"/v1/messages","cache_tokens":2,"cache_usage_reported":true,%s}`, tc.metadata)
			// Every query, including GetCallAnalytics' seed/model filter and retry
			// lookup, reads this synthetic VALUES table. No real logs or schema writes.
			LOG_DB = pg.Table(`(VALUES (7, 'synthetic', 1800000010::bigint, ?::integer,
				'effective-model', 11, 5, 3, 1, false, 2, 'unicode-request', ?::text))
				AS logs(user_id, username, created_at, type, model_name, quota, prompt_tokens,
				completion_tokens, use_time, is_stream, channel_id, request_id, other)`, LogTypeConsume, other)
			var projected struct{ Other string }
			require.NoError(t, LOG_DB.Select(callAnalyticsProjection(common.DatabaseTypePostgreSQL)).Scan(&projected).Error)
			assert.NotContains(t, projected.Other, "private_marker")
			assert.NotContains(t, projected.Other, `"prompt"`)
			assert.NotContains(t, projected.Other, `"debug"`)

			filter := CallAnalyticsFilter{StartTimestamp: 1800000000, EndTimestamp: 1800000060}
			for _, model := range []string{"", tc.requested, "no-match"} {
				filter.ModelName = model
				report, err := GetCallAnalytics(context.Background(), filter)
				require.NoError(t, err)
				if model == "no-match" {
					assert.Zero(t, report.Summary.Requests)
					continue
				}
				require.Len(t, report.Requests.Items, 1)
				assert.Equal(t, 1, report.Summary.Success)
				assert.Equal(t, int64(11), report.Summary.Quota)
				assert.Equal(t, int64(5), report.Summary.InputTokens)
				assert.Equal(t, int64(2), report.Summary.CacheReadTokens)
				assert.Equal(t, tc.requested, report.Requests.Items[0].ModelName)
				assert.Equal(t, "effective-model", report.Requests.Items[0].EffectiveModel)
			}
		})
	}
}
