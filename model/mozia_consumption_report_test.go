package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
