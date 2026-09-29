package model

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestMoziaConsumptionReportSeparatesSourcesAndNetsRefunds(t *testing.T) {
	truncateTables(t)
	const start int64 = 1800000000
	const end = start + 86400
	require.NoError(t, DB.Create(&User{Id: 101, Username: "report-user", DisplayName: "Report User"}).Error)
	transactions := []MoziaWalletTransaction{
		{UserId: 101, ModelName: "model-a", Source: "paid", EventType: "consume", Delta: -100, CreatedTime: start},
		{UserId: 101, ModelName: "model-a", Source: "paid", EventType: "refund", Delta: 20, CreatedTime: start + 1},
		{UserId: 101, ModelName: "model-a", Source: "gift", EventType: "consume", Delta: -30, CreatedTime: end - 1},
		{UserId: 102, ModelName: "model-b", Source: "paid", EventType: "refund", Delta: 50, CreatedTime: start + 1},
		{UserId: 102, ModelName: "model-b", Source: "legacy", EventType: "consume", Delta: -7, CreatedTime: start + 1},
		{UserId: 101, Source: "paid", EventType: "topup", Delta: 9999, CreatedTime: start},
		{UserId: 101, Source: "gift", EventType: "adjust", Delta: -9999, CreatedTime: start},
		{UserId: 101, ModelName: "model-a", Source: "paid", EventType: "consume", Delta: -9999, CreatedTime: start - 1},
		{UserId: 101, ModelName: "model-a", Source: "paid", EventType: "consume", Delta: -9999, CreatedTime: end},
	}
	require.NoError(t, DB.Create(&transactions).Error)
	logs := []Log{
		{UserId: 101, ModelName: "model-c", Type: LogTypeConsume, Quota: 60, CreatedAt: start, Other: `{"billing_source":"subscription"}`},
		{UserId: 101, ModelName: "model-c", Type: LogTypeRefund, Quota: 10, CreatedAt: end - 1, Other: `{"billing_source":"subscription"}`},
		{UserId: 101, ModelName: "model-c", Type: LogTypeConsume, Quota: 9999, CreatedAt: end, Other: `{"billing_source":"subscription"}`},
		{UserId: 101, ModelName: "model-a", Type: LogTypeConsume, Quota: 80, CreatedAt: start, Other: `{"billing_source":"wallet","request_body":"{\"billing_source\":\"subscription\"}"}`},
	}
	require.NoError(t, LOG_DB.Create(&logs).Error)
	rows, err := GetMoziaConsumptionReport(start, end)
	require.NoError(t, err)
	assert.ElementsMatch(t, []MoziaConsumptionReportRow{
		{UserId: 101, Username: "report-user", DisplayName: "Report User", ModelName: "model-a", Source: "paid", Quota: 80},
		{UserId: 101, Username: "report-user", DisplayName: "Report User", ModelName: "model-a", Source: "gift", Quota: 30},
		{UserId: 102, ModelName: "model-b", Source: "paid", Quota: -50},
		{UserId: 102, ModelName: "model-b", Source: "legacy", Quota: 7},
		{UserId: 101, Username: "report-user", DisplayName: "Report User", ModelName: "model-c", Source: "subscription", Quota: 50},
	}, rows)
}

func TestMoziaConsumptionReportRejectsUnboundedRanges(t *testing.T) {
	for _, interval := range [][2]int64{{0, 1}, {10, 10}, {20, 10}, {1, 32*86400 + 1}} {
		_, err := GetMoziaConsumptionReport(interval[0], interval[1])
		require.Error(t, err)
	}
}

func TestMoziaConsumptionReportPostgresSubscriptionIndex(t *testing.T) {
	dsn := os.Getenv("MOZIA_USAGE_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("MOZIA_USAGE_TEST_POSTGRES_DSN is not set (PostgreSQL 12+)")
	}
	truncateTables(t)
	pg, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	connection, err := pg.DB()
	require.NoError(t, err)
	defer connection.Close()
	tx := pg.Begin()
	require.NoError(t, tx.Error)
	defer tx.Rollback()
	// Temporary logs shadow any real table, including the index DDL below.
	require.NoError(t, tx.Exec(`CREATE TEMP TABLE logs (
		user_id bigint, model_name text, created_at bigint, type bigint,
		quota bigint, other text) ON COMMIT DROP`).Error)
	require.NoError(t, tx.Exec(`INSERT INTO logs
		SELECT 101, 'wallet-model', 1800000010, 2, 9999,
		'{"billing_source":"wallet"}' FROM generate_series(1, 10000)`).Error)
	require.NoError(t, tx.Exec(`INSERT INTO logs VALUES
		(101, 'subscription-model', 1800000000, 2, 60, '{"billing_source":"subscription"}'),
		(101, 'subscription-model', 1800086399, 6, 10, '{"billing_source":"subscription"}'),
		(101, 'subscription-model', 1800086400, 2, 9999, '{"billing_source":"subscription"}')`).Error)
	indexSQL, err := os.ReadFile("../scripts/postgres-daily-usage-index.sql")
	require.NoError(t, err)
	// Concurrent DDL is for live tables, not a transaction-local test table.
	require.NoError(t, tx.Exec(strings.Replace(string(indexSQL), "CREATE INDEX CONCURRENTLY", "CREATE INDEX", 1)).Error)
	require.NoError(t, tx.Exec("ANALYZE logs").Error)
	require.NoError(t, tx.Exec("SET LOCAL plan_cache_mode = force_generic_plan").Error)
	originalLogDB := LOG_DB
	LOG_DB = tx
	defer func() { LOG_DB = originalLogDB }()
	rows, err := GetMoziaConsumptionReport(1800000000, 1800086400)
	require.NoError(t, err)
	assert.Equal(t, []MoziaConsumptionReportRow{{
		UserId: 101, ModelName: "subscription-model", Source: "subscription", Quota: 50,
	}}, rows)

	// Explain the actual prepared statement generated by the report, so a
	// regression to parameterized predicates cannot hide behind a copied query.
	var statement string
	require.NoError(t, tx.Raw(`SELECT name FROM pg_prepared_statements
		WHERE statement LIKE 'SELECT user_id, model_name, SUM(CASE WHEN type = 2%'`).Scan(&statement).Error)
	require.NotEmpty(t, statement)
	var plan string
	require.NoError(t, tx.Raw(fmt.Sprintf(`EXPLAIN (FORMAT JSON) EXECUTE "%s"(1800000000,1800086400)`,
		strings.ReplaceAll(statement, `"`, `""`))).Scan(&plan).Error)
	assert.Contains(t, plan, "idx_logs_subscription_usage", "report must use the subscription index with a generic plan")
	assert.NotContains(t, plan, `"Filter"`, "report must not scan log JSON again")
}
