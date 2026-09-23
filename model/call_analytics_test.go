package model

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func callAnalyticsTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&Log{}, &User{}))
	oldDB, oldLogs, oldDialect := DB, LOG_DB, common.LogDatabaseType()
	DB, LOG_DB = db, db
	common.SetLogDatabaseType(common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		DB, LOG_DB = oldDB, oldLogs
		common.SetLogDatabaseType(oldDialect)
		require.NoError(t, sqlDB.Close())
	})
	return db
}

func TestCallAnalyticsRequestResultsAndCrossWindowRetries(t *testing.T) {
	db := callAnalyticsTestDB(t)
	start := int64(1800000000)
	logs := []Log{
		{UserId: 7, Username: "customer", RequestId: "recovered", Type: LogTypeError, CreatedAt: start + 10, ModelName: "kimi", ChannelId: 1, Other: `{"request_path":"/v1/chat/completions","status_code":502,"error_code":"bad_upstream"}`},
		{UserId: 7, Username: "customer", RequestId: "recovered", Type: LogTypeConsume, CreatedAt: start + 12, ModelName: "kimi", ChannelId: 2, PromptTokens: 100, CompletionTokens: 10, Quota: 100, IsStream: true, UseTime: 2, Other: `{"request_path":"/v1/chat/completions","cache_tokens":80,"frt":50,"stream_status":{"status":"ok"},"request_body":{"messages":[{"content":"SECRET_PROMPT"}]}}`},
		{UserId: 7, Username: "customer", RequestId: "recovered", Type: LogTypeRequestOutcome, CreatedAt: start + 12, ModelName: "kimi", ChannelId: 2, Other: `{"request_path":"/v1/chat/completions","request_outcome":{"status":"success","status_code":200,"duration_ms":2200}}`},
		{UserId: 7, Username: "customer", RequestId: "legacy-success", Type: LogTypeConsume, CreatedAt: start + 30, ModelName: "kimi", ChannelId: 2, PromptTokens: 20, CompletionTokens: 5, Quota: 25, UseTime: 1, Other: `{"request_path":"/v1/chat/completions","cache_tokens":0,"frt":-1000}`},
		{UserId: 7, Username: "customer", RequestId: "failure", Type: LogTypeError, CreatedAt: start + 35, ModelName: "kimi", ChannelId: 3, UseTime: 1, Other: `{"request_path":"/v1/chat/completions","status_code":503,"error_code":"no_capacity"}`},
		{UserId: 7, Username: "customer", RequestId: "cancelled", Type: LogTypeConsume, CreatedAt: start + 40, ModelName: "kimi", ChannelId: 2, PromptTokens: 10, CompletionTokens: 2, Quota: 10, IsStream: true, Other: `{"request_path":"/v1/chat/completions","cache_tokens":0,"cache_usage_reported":true,"frt":10,"stream_status":{"status":"error","end_reason":"client_gone"}}`},
		{UserId: 7, Username: "customer", RequestId: "cancelled", Type: LogTypeRequestOutcome, CreatedAt: start + 40, ModelName: "kimi", ChannelId: 2, Other: `{"request_path":"/v1/chat/completions","request_outcome":{"status":"cancelled","status_code":200,"duration_ms":900}}`},
		{UserId: 7, Username: "customer", RequestId: "", Type: LogTypeConsume, CreatedAt: start + 45, ModelName: "kimi", ChannelId: 2, PromptTokens: 10, Other: `{"request_path":"/v1/chat/completions"}`},
		// The first failed attempt is inside the window, but the request completed outside it.
		{UserId: 7, Username: "customer", RequestId: "cross-end", Type: LogTypeError, CreatedAt: start + 50, ModelName: "kimi", ChannelId: 1, Other: `{"request_path":"/v1/chat/completions","status_code":500}`},
		{UserId: 7, Username: "customer", RequestId: "cross-end", Type: LogTypeConsume, CreatedAt: start + 3601, ModelName: "kimi", ChannelId: 2, PromptTokens: 99999, Other: `{"request_path":"/v1/chat/completions"}`},
		// This request completed inside; its earlier error must still count as a retry.
		{UserId: 7, Username: "customer", RequestId: "cross-start", Type: LogTypeError, CreatedAt: start - 1, ModelName: "kimi", ChannelId: 1, Other: `{"request_path":"/v1/chat/completions","status_code":500}`},
		{UserId: 7, Username: "customer", RequestId: "cross-start", Type: LogTypeConsume, CreatedAt: start + 60, ModelName: "kimi", ChannelId: 2, PromptTokens: 50, CompletionTokens: 10, Quota: 50, UseTime: 3, Other: `{"request_path":"/v1/chat/completions","cache_tokens":0,"cache_usage_reported":true}`},
		{UserId: 8, Username: "other", RequestId: "other-user", Type: LogTypeConsume, CreatedAt: start + 60, ModelName: "kimi", ChannelId: 2, PromptTokens: 99999, Other: `{"request_path":"/v1/chat/completions"}`},
		{UserId: 7, Username: "customer", RequestId: "task", Type: LogTypeConsume, CreatedAt: start + 60, ModelName: "kimi", ChannelId: 2, PromptTokens: 99999, Other: `{"request_path":"/v1/video/generations","task_id":"task_123"}`},
	}
	require.NoError(t, db.Create(&logs).Error)
	filter := CallAnalyticsFilter{StartTimestamp: start, EndTimestamp: start + 3600, UserID: 7, ModelName: "kimi", Page: 1, PageSize: 2}
	report, err := GetCallAnalytics(context.Background(), filter)
	require.NoError(t, err)
	assert.Equal(t, 6, report.Summary.Requests)
	assert.Equal(t, 3, report.Summary.Success)
	assert.Equal(t, 1, report.Summary.Errors)
	assert.Equal(t, 1, report.Summary.Cancelled)
	assert.Equal(t, 1, report.Summary.Unknown)
	assert.Equal(t, int64(185), report.Summary.Quota)
	assert.Equal(t, int64(190), report.Summary.InputTokens)
	assert.Equal(t, int64(27), report.Summary.OutputTokens)
	assert.Equal(t, 2, report.Summary.RetriedRequests)
	assert.Equal(t, 2, report.Summary.RecoveredRequests)
	require.NotNil(t, report.Summary.CacheHitRate)
	assert.InDelta(t, 0.5, *report.Summary.CacheHitRate, 0.00001) // 80 / (100+10+50); missing zero usage excluded.
	assert.Equal(t, 3, report.Coverage.CacheSamples)
	assert.Equal(t, 2, report.Coverage.FinalRecordedRequests)
	assert.Equal(t, 4, report.Coverage.InferredRequests)
	require.NotNil(t, report.Summary.AvgFRTMs)
	assert.Equal(t, 30.0, *report.Summary.AvgFRTMs)
	assert.Equal(t, 50.0, *report.Summary.P95FRTMs)
	assert.InDelta(t, 0.1, report.Summary.AvgRPM, 0.00001)
	assert.Equal(t, 5, report.Summary.PeakRPM)
	assert.Len(t, report.Requests.Items, 2)
	assert.Equal(t, 6, report.Requests.Total)
	assert.Contains(t, report.Warnings, "historical_outcomes_incomplete")
	assert.Contains(t, report.Warnings, "cache_usage_incomplete")
	serialized, err := common.Marshal(report)
	require.NoError(t, err)
	assert.NotContains(t, string(serialized), "SECRET_PROMPT")
	assert.NotContains(t, string(serialized), "request_body")
	filter.Page = 2
	next, err := GetCallAnalytics(context.Background(), filter)
	require.NoError(t, err)
	assert.Equal(t, report.Summary, next.Summary)
	assert.NotEqual(t, report.Requests.Items[0].RequestID, next.Requests.Items[0].RequestID)
	filter.Outcome, filter.Page = "error", 1
	failures, err := GetCallAnalytics(context.Background(), filter)
	require.NoError(t, err)
	assert.Equal(t, 1, failures.Summary.Requests)
	assert.Equal(t, 503, failures.Errors[0].StatusCode)
	assert.Equal(t, "failure", failures.Requests.Items[0].RequestID)
}

func TestCallAnalyticsTrendPreservesEmptyBucketsAndTimeBoundaries(t *testing.T) {
	db := callAnalyticsTestDB(t)
	start := int64(1800000007)
	logs := []Log{
		{UserId: 7, RequestId: "before", Type: LogTypeConsume, CreatedAt: start - 1, Quota: 100, Other: `{"request_path":"/v1/chat/completions"}`},
		{UserId: 7, RequestId: "first", Type: LogTypeConsume, CreatedAt: start, PromptTokens: 4, CompletionTokens: 1, Quota: 7, Other: `{"request_path":"/v1/chat/completions"}`},
		{UserId: 7, RequestId: "last", Type: LogTypeConsume, CreatedAt: start + 124, PromptTokens: 6, CompletionTokens: 3, Quota: 11, Other: `{"request_path":"/v1/chat/completions"}`},
		{UserId: 7, RequestId: "end", Type: LogTypeConsume, CreatedAt: start + 125, Quota: 100, Other: `{"request_path":"/v1/chat/completions"}`},
	}
	require.NoError(t, db.Create(&logs).Error)
	report, err := GetCallAnalytics(context.Background(), CallAnalyticsFilter{StartTimestamp: start, EndTimestamp: start + 125, UserID: 7})
	require.NoError(t, err)
	assert.Equal(t, 2, report.Summary.Requests)
	assert.Equal(t, []CallAnalyticsTrend{
		{Timestamp: start, Requests: 1, Success: 1, Tokens: 5, Quota: 7},
		{Timestamp: start + 60},
		{Timestamp: start + 120, Requests: 1, Success: 1, Tokens: 9, Quota: 11},
	}, report.Trend)
}

func TestCallAnalyticsRedirectCacheNormalizationAndStreamFailures(t *testing.T) {
	db := callAnalyticsTestDB(t)
	start := int64(1800000000)
	logs := []Log{
		{UserId: 7, RequestId: "redirect", Type: LogTypeConsume, CreatedAt: start + 10, ModelName: "actual-model", ChannelId: 9, PromptTokens: 10, CompletionTokens: 5, Quota: 100, IsStream: true, Other: `{"request_path":"/v1/messages","admin_info":{"requested_model":"public-model","effective_model":"actual-model","routing_rule_id":"route-1"},"usage_semantic":"anthropic","cache_tokens":60,"cache_creation_tokens":10,"cache_creation_tokens_5m":4,"cache_creation_tokens_1h":6,"frt":15,"stream_status":{"status":"ok"}}`},
		{UserId: 7, RequestId: "truncated", Type: LogTypeConsume, CreatedAt: start + 15, ModelName: "public-model", ChannelId: 9, PromptTokens: 20, CompletionTokens: 5, Quota: 20, IsStream: true, Other: `{"request_path":"/v1/chat/completions","stream_status":{"status":"error","end_reason":"timeout"}}`},
		{UserId: 7, RequestId: "violation", Type: LogTypeConsume, CreatedAt: start + 16, ModelName: "public-model", ChannelId: 9, Quota: 10, Other: `{"request_path":"/v1/chat/completions","violation_fee":true,"status_code":400}`},
		{UserId: 7, RequestId: "early-error", Type: LogTypeRequestOutcome, CreatedAt: start + 20, ModelName: "public-model", Other: `{"request_path":"/v1/chat/completions","request_outcome":{"status":"error","status_code":503,"duration_ms":4},"error_code":"model_not_found"}`},
	}
	require.NoError(t, db.Create(&logs).Error)
	report, err := GetCallAnalytics(context.Background(), CallAnalyticsFilter{StartTimestamp: start, EndTimestamp: start + 60, UserID: 7, ModelName: "public-model"})
	require.NoError(t, err)
	assert.Equal(t, 4, report.Summary.Requests)
	assert.Equal(t, 1, report.Summary.Success)
	assert.Equal(t, 3, report.Summary.Errors)
	assert.Equal(t, int64(100), report.Summary.InputTokens)
	assert.Equal(t, int64(130), report.Summary.Quota)
	assert.Equal(t, int64(60), report.Summary.CacheReadTokens)
	assert.Equal(t, int64(10), report.Summary.CacheWriteTokens)
	require.NotNil(t, report.Summary.CacheHitRate)
	assert.Equal(t, 0.75, *report.Summary.CacheHitRate)
	assert.Equal(t, 1, report.Coverage.FRTSamples)
	redirect := report.Requests.Items[3]
	assert.Equal(t, "public-model", redirect.ModelName)
	assert.Equal(t, "actual-model", redirect.EffectiveModel)
	assert.Equal(t, "route-1", redirect.RoutingRuleID)
	assert.Nil(t, report.Requests.Items[2].FRTMs) // Missing first packet is not a 0ms sample.
}

func TestCallAnalyticsTimeRangeAndPathValidation(t *testing.T) {
	now := time.Unix(1800000000, 0)
	filter := CallAnalyticsFilter{}
	require.NoError(t, filter.Normalize(now))
	assert.Equal(t, now.Unix()-86400, filter.StartTimestamp)
	assert.Equal(t, 20, filter.PageSize)
	for _, invalid := range []CallAnalyticsFilter{
		{StartTimestamp: 10, EndTimestamp: 10}, {StartTimestamp: 1, EndTimestamp: 32 * 86400}, {UserID: -1}, {PageSize: 101}, {Outcome: "anything"}, {Page: -1},
	} {
		assert.Error(t, invalid.Normalize(now), "%+v", invalid)
	}
	for _, path := range []string{"/v1/chat/completions", "/v1/messages", "/v1/responses/compact", "/v1beta/models/gemini:generateContent", "/v1beta/models/gemini:streamGenerateContent?alt=sse"} {
		assert.True(t, IsCallAnalyticsPath(path), path)
	}
	for _, path := range []string{"/v1/video/generations", "/v1/images/generations", "/v1/embeddings", "/v1beta/models/gemini:countTokens", "/v1/models"} {
		assert.False(t, IsCallAnalyticsPath(path), path)
	}
}

func TestCallAnalyticsUserSearchReturnsOnlyIdentifiers(t *testing.T) {
	db := callAnalyticsTestDB(t)
	users := []User{{Id: 70, Username: "customer_7", AffCode: "aff70", Password: "secret-password", Email: "private@example.org"}, {Id: 71, Username: "customerX7", AffCode: "aff71"}}
	require.NoError(t, db.Create(&users).Error)
	matches, err := SearchCallAnalyticsUsers(context.Background(), "customer_7")
	require.NoError(t, err)
	require.Len(t, matches, 1)
	assert.Equal(t, 70, matches[0].ID)
	matches, err = SearchCallAnalyticsUsers(context.Background(), "70")
	require.NoError(t, err)
	require.Len(t, matches, 1)
	bytes, err := common.Marshal(matches)
	require.NoError(t, err)
	assert.JSONEq(t, `[{"id":70,"username":"customer_7"}]`, string(bytes))
}

func TestCallAnalyticsProjectsOnlyWhitelistedFields(t *testing.T) {
	db := callAnalyticsTestDB(t)
	log := Log{Other: `{"frt":17,"cache_usage_reported":true,"request_outcome":{"status":"success"},"request_body":{"prompt":"private"},"admin_info":{"requested_model":"kimi","private_debug":"sensitive"}}`}
	require.NoError(t, db.Create(&log).Error)
	var projected Log
	require.NoError(t, db.Model(&Log{}).Select(callAnalyticsProjection(common.DatabaseTypeSQLite)).First(&projected).Error)
	assert.Contains(t, projected.Other, `"frt":17`)
	assert.Contains(t, projected.Other, `"admin_info_requested_model":"kimi"`)
	assert.NotContains(t, projected.Other, "private")
}

func TestCallAnalyticsUnknownStreamsAndUnavailableRouteTargets(t *testing.T) {
	db := callAnalyticsTestDB(t)
	start := int64(1800000000)
	logs := []Log{
		{UserId: 7, RequestId: "legacy-stream", Type: LogTypeConsume, CreatedAt: start + 1, ModelName: "kimi", IsStream: true, PromptTokens: 10, Other: `{"request_path":"/v1/chat/completions"}`},
		{UserId: 7, RequestId: "disabled-target", Type: LogTypeRequestOutcome, CreatedAt: start + 2, ModelName: "kimi", Other: `{"request_path":"/v1/chat/completions","request_outcome":{"status":"error","status_code":503,"duration_ms":2},"admin_info":{"routing_target_channel_id":419},"error_code":"model_not_found"}`},
		{UserId: 7, RequestId: "unknown-model", Type: LogTypeRequestOutcome, CreatedAt: start + 3, Other: `{"request_path":"/v1/chat/completions","request_outcome":{"status":"error","status_code":429,"duration_ms":1}}`},
	}
	require.NoError(t, db.Create(&logs).Error)
	filter := CallAnalyticsFilter{StartTimestamp: start, EndTimestamp: start + 60, UserID: 7}
	report, err := GetCallAnalytics(context.Background(), filter)
	require.NoError(t, err)
	assert.Equal(t, 1, report.Summary.Unknown)
	assert.Equal(t, 0, report.Summary.Success)
	assert.Nil(t, report.Summary.CacheHitRate)
	assert.Nil(t, report.Summary.AvgFRTMs)
	assert.Equal(t, 1, report.Coverage.UnknownModelRequests)
	assert.Contains(t, report.Warnings, "unknown_request_models")
	filter.Channel = 419
	report, err = GetCallAnalytics(context.Background(), filter)
	require.NoError(t, err)
	require.Len(t, report.Requests.Items, 1)
	assert.Equal(t, 0, report.Requests.Items[0].Attempts)
	assert.Equal(t, "error", report.Requests.Items[0].Outcome)
	assert.Equal(t, 419, report.Requests.Items[0].ChannelID)
	assert.False(t, report.Requests.Items[0].CacheUsageReported)
}
