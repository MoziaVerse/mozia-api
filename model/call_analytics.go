package model

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/tidwall/gjson"
	"gorm.io/gorm"
)

const CallAnalyticsMaxLogs = 50000

var ErrCallAnalyticsLimit = errors.New("too many matching logs; narrow the time range or select a user/model (maximum 50000 logs)")

type CallAnalyticsFilter struct {
	StartTimestamp int64  `json:"start_timestamp" form:"start_timestamp"`
	EndTimestamp   int64  `json:"end_timestamp" form:"end_timestamp"`
	UserID         int    `json:"user_id" form:"user_id"`
	User           string `json:"user" form:"user"`
	ModelName      string `json:"model_name" form:"model_name"`
	Channel        int    `json:"channel" form:"channel"`
	Outcome        string `json:"outcome" form:"outcome"`
	Page           int    `json:"p" form:"p"`
	PageSize       int    `json:"page_size" form:"page_size"`
}

func (f *CallAnalyticsFilter) Normalize(now time.Time) error {
	f.User = strings.TrimSpace(f.User)
	if f.EndTimestamp == 0 {
		f.EndTimestamp = now.Unix()
	}
	if f.StartTimestamp == 0 {
		f.StartTimestamp = f.EndTimestamp - 86400
	}
	if f.StartTimestamp < 0 || f.EndTimestamp <= f.StartTimestamp || f.EndTimestamp-f.StartTimestamp > 31*86400 {
		return errors.New("time range must be positive and no longer than 31 days")
	}
	if f.UserID < 0 || f.Channel < 0 || len(f.User) > 128 || (f.UserID != 0 && f.User != "") {
		return errors.New("invalid user or channel filter")
	}
	if len(f.ModelName) > 256 {
		return errors.New("model_name is too long")
	}
	switch f.Outcome {
	case "", "success", "error", "cancelled", "unknown":
	default:
		return errors.New("invalid outcome")
	}
	if f.Page == 0 {
		f.Page = 1
	}
	if f.PageSize == 0 {
		f.PageSize = 20
	}
	if f.Page < 1 || f.Page > CallAnalyticsMaxLogs || f.PageSize < 1 || f.PageSize > 100 {
		return errors.New("invalid pagination")
	}
	return nil
}

// IsCallAnalyticsPath limits request analytics to synchronous text generation.
func IsCallAnalyticsPath(path string) bool {
	path = strings.SplitN(path, "?", 2)[0]
	switch path {
	case "/v1/chat/completions", "/v1/completions", "/v1/messages", "/v1/responses", "/v1/responses/compact", "/v1beta/openai/chat/completions", "/pg/chat/completions":
		return true
	}
	return (strings.HasPrefix(path, "/v1beta/models/") || strings.HasPrefix(path, "/v1/models/")) &&
		(strings.HasSuffix(path, ":generateContent") || strings.HasSuffix(path, ":streamGenerateContent"))
}

type CallAnalyticsSummary struct {
	Requests          int      `json:"requests"`
	Success           int      `json:"success"`
	Errors            int      `json:"errors"`
	Cancelled         int      `json:"cancelled"`
	Unknown           int      `json:"unknown"`
	SuccessRate       *float64 `json:"success_rate"`
	ErrorRate         *float64 `json:"error_rate"`
	Quota             int64    `json:"quota"`
	InputTokens       int64    `json:"input_tokens"`
	OutputTokens      int64    `json:"output_tokens"`
	CacheReadTokens   int64    `json:"cache_read_tokens"`
	CacheWriteTokens  int64    `json:"cache_write_tokens"`
	CacheHitRate      *float64 `json:"cache_hit_rate"`
	AvgRPM            float64  `json:"avg_rpm"`
	RecentRPM         *int     `json:"recent_rpm"`
	PeakRPM           int      `json:"peak_rpm"`
	AvgTPM            float64  `json:"avg_tpm"`
	RecentTPM         *int64   `json:"recent_tpm"`
	PeakTPM           int64    `json:"peak_tpm"`
	AvgDurationMs     *float64 `json:"avg_duration_ms"`
	P95DurationMs     *float64 `json:"p95_duration_ms"`
	RetriedRequests   int      `json:"retried_requests"`
	RecoveredRequests int      `json:"recovered_requests"`
}

type CallAnalyticsQuantiles struct {
	P50     *float64 `json:"p50"`
	P95     *float64 `json:"p95"`
	P99     *float64 `json:"p99"`
	Samples int      `json:"samples"`
}

type CallAnalyticsDistributions struct {
	FirstResponseMs CallAnalyticsQuantiles `json:"first_response_ms"`
	OutputTPS       CallAnalyticsQuantiles `json:"output_tps"`
	RPM             CallAnalyticsQuantiles `json:"rpm"`
	TPM             CallAnalyticsQuantiles `json:"tpm"`
	CacheShare      CallAnalyticsQuantiles `json:"cache_share"`
}

type CallAnalyticsAttempt struct {
	ChannelID  int    `json:"channel_id"`
	CreatedAt  int64  `json:"created_at"`
	StatusCode int    `json:"status_code"`
	ErrorCode  string `json:"error_code"`
	Type       string `json:"type"`
}

type CallAnalyticsRequest struct {
	RequestID          string                 `json:"request_id"`
	UserID             int                    `json:"user_id"`
	Username           string                 `json:"username"`
	CompletedAt        int64                  `json:"completed_at"`
	ModelName          string                 `json:"model_name"`
	EffectiveModel     string                 `json:"effective_model"`
	ChannelID          int                    `json:"channel_id"`
	Outcome            string                 `json:"outcome"`
	StatusCode         int                    `json:"status_code"`
	Quota              int64                  `json:"quota"`
	InputTokens        int64                  `json:"input_tokens"`
	OutputTokens       int64                  `json:"output_tokens"`
	CacheReadTokens    int64                  `json:"cache_read_tokens"`
	CacheWriteTokens   int64                  `json:"cache_write_tokens"`
	FRTMs              *float64               `json:"frt_ms"`
	OutputTPS          *float64               `json:"output_tps"`
	DurationMs         *float64               `json:"duration_ms"`
	Attempts           int                    `json:"attempts"`
	AttemptLogs        []CallAnalyticsAttempt `json:"attempt_logs"`
	RoutingRuleID      string                 `json:"routing_rule_id"`
	FinalRecorded      bool                   `json:"final_recorded"`
	ErrorCode          string                 `json:"error_code"`
	cacheInput         int64
	cacheRead          int64
	CacheUsageReported bool `json:"cache_usage_reported"`
	hasError           bool
}

type CallAnalyticsTrend struct {
	Timestamp int64 `json:"timestamp"`
	Requests  int   `json:"requests"`
	Success   int   `json:"success"`
	Errors    int   `json:"errors"`
	Tokens    int64 `json:"tokens"`
	Quota     int64 `json:"quota"`
}

type CallAnalyticsUser struct {
	UserID   int    `json:"user_id"`
	Username string `json:"username"`
	CallAnalyticsSummary
}

type CallAnalyticsChannel struct {
	ChannelID int `json:"channel_id"`
	CallAnalyticsSummary
}

type CallAnalyticsError struct {
	StatusCode int    `json:"status_code"`
	ErrorCode  string `json:"error_code"`
	Count      int    `json:"count"`
}

type CallAnalyticsCoverage struct {
	UnknownModelRequests  int `json:"unknown_model_requests"`
	FinalRecordedRequests int `json:"final_recorded_requests"`
	InferredRequests      int `json:"inferred_requests"`
}

type CallAnalyticsReport struct {
	StartTimestamp       int64                      `json:"start_timestamp"`
	EndTimestamp         int64                      `json:"end_timestamp"`
	TrendIntervalSeconds int64                      `json:"trend_interval_seconds"`
	Summary              CallAnalyticsSummary       `json:"summary"`
	Distributions        CallAnalyticsDistributions `json:"distributions"`
	Trend                []CallAnalyticsTrend       `json:"trend"`
	Users                []CallAnalyticsUser        `json:"users"`
	Channels             []CallAnalyticsChannel     `json:"channels"`
	Errors               []CallAnalyticsError       `json:"errors"`
	Requests             struct {
		Items    []CallAnalyticsRequest `json:"items"`
		Total    int                    `json:"total"`
		Page     int                    `json:"p"`
		PageSize int                    `json:"page_size"`
	} `json:"requests"`
	Coverage CallAnalyticsCoverage `json:"coverage"`
	Warnings []string              `json:"warnings"`
}

// Only these usage/result fields leave the log database; request bodies, prompts,
// token names, IPs, and free-form upstream error text are deliberately not read.
var callAnalyticsMetadataPaths = []string{
	"request_path", "request_outcome", "requested_model", "effective_model", "admin_info.requested_model", "admin_info.effective_model",
	"admin_info.routing_rule_id", "admin_info.routing_target_channel_id", "status_code", "error_code", "stream_status.status", "stream_status.end_reason",
	"violation_fee", "frt", "cache_tokens", "cache_creation_tokens", "cache_creation_tokens_5m", "cache_creation_tokens_1h",
	"cache_write_tokens", "input_tokens_total", "usage_semantic", "claude", "cache_usage_reported", "generation_ms", "task_id",
}

func callAnalyticsJSONValue(path string, dialect common.DatabaseType) string {
	parts := strings.Split(path, ".")
	switch dialect {
	case common.DatabaseTypeClickHouse:
		return "JSONExtractRaw(other, '" + strings.Join(parts, "', '") + "')"
	case common.DatabaseTypePostgreSQL:
		// PostgreSQL rejects JSON NUL escapes even in fields excluded from the
		// projection. Drop only real NUL escapes, preserving paired backslashes
		// (literal \u0000) and valid Unicode; the pattern has no GORM placeholders.
		other := `regexp_replace(COALESCE(NULLIF(other, ''), '{}'), '(\\\\)|\\u0000', '\1', 'g')`
		return "(" + other + "::jsonb #> '{" + strings.Join(parts, ",") + "}')"
	case common.DatabaseTypeMySQL:
		return "JSON_EXTRACT(IF(JSON_VALID(other), other, '{}'), '$." + path + "')"
	default:
		return "json_extract(CASE WHEN json_valid(other) THEN other ELSE '{}' END, '$." + path + "')"
	}
}

func callAnalyticsProjection(dialect common.DatabaseType) string {
	parts := make([]string, 0, len(callAnalyticsMetadataPaths)*2)
	for _, path := range callAnalyticsMetadataPaths {
		key := strings.ReplaceAll(path, ".", "_")
		value := callAnalyticsJSONValue(path, dialect)
		if dialect == common.DatabaseTypeClickHouse {
			parts = append(parts, "'\""+key+"\":'", "if(empty("+value+"), 'null', "+value+")", "','")
		} else {
			parts = append(parts, "'"+key+"'", value)
		}
	}
	switch dialect {
	case common.DatabaseTypeClickHouse:
		parts = parts[:len(parts)-1]
		return "concat('{', " + strings.Join(parts, ", ") + ", '}') AS other"
	case common.DatabaseTypePostgreSQL:
		return "jsonb_build_object(" + strings.Join(parts, ", ") + ")::text AS other"
	default:
		return "json_object(" + strings.Join(parts, ", ") + ") AS other"
	}
}

func GetCallAnalytics(ctx context.Context, filter CallAnalyticsFilter) (*CallAnalyticsReport, error) {
	if err := filter.Normalize(time.Now()); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if filter.User != "" {
		if id, err := strconv.Atoi(filter.User); err == nil && id > 0 {
			filter.UserID = id
		} else {
			var user User
			err := DB.WithContext(ctx).Unscoped().Model(&User{}).Select("id").Where("username = ?", filter.User).First(&user).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return buildCallAnalytics(nil, filter), nil
			}
			if err != nil {
				return nil, err
			}
			filter.UserID = user.Id
		}
	}
	dialect := common.LogDatabaseType()
	base := LOG_DB.WithContext(ctx).Model(&Log{}).Where("type IN ?", []int{LogTypeConsume, LogTypeError, LogTypeRequestOutcome})
	if filter.UserID != 0 {
		base = base.Where("user_id = ?", filter.UserID)
	}
	seeds := base.Session(&gorm.Session{}).Where("created_at >= ? AND created_at < ?", filter.StartTimestamp, filter.EndTimestamp)
	if filter.ModelName != "" {
		// Include redirects whose effective model differs from the requested model.
		var expr string
		switch dialect {
		case common.DatabaseTypePostgreSQL:
			expr = callAnalyticsJSONValue("requested_model", dialect) + " #>> '{}' = ? OR " + callAnalyticsJSONValue("admin_info.requested_model", dialect) + " #>> '{}' = ?"
		case common.DatabaseTypeClickHouse:
			expr = "JSONExtractString(other, 'requested_model') = ? OR JSONExtractString(other, 'admin_info', 'requested_model') = ?"
		case common.DatabaseTypeMySQL:
			expr = "JSON_UNQUOTE(" + callAnalyticsJSONValue("requested_model", dialect) + ") = ? OR JSON_UNQUOTE(" + callAnalyticsJSONValue("admin_info.requested_model", dialect) + ") = ?"
		default:
			expr = callAnalyticsJSONValue("requested_model", dialect) + " = ? OR " + callAnalyticsJSONValue("admin_info.requested_model", dialect) + " = ?"
		}
		seeds = seeds.Where("(model_name = ? OR "+expr+")", filter.ModelName, filter.ModelName, filter.ModelName)
	}
	var seedRows []struct {
		RequestID string `gorm:"column:request_id"`
	}
	if err := seeds.Select("request_id").Limit(CallAnalyticsMaxLogs + 1).Find(&seedRows).Error; err != nil {
		return nil, err
	}
	if len(seedRows) > CallAnalyticsMaxLogs {
		return nil, ErrCallAnalyticsLimit
	}
	ids := make([]string, 0, len(seedRows))
	seen := make(map[string]bool)
	hasEmpty := false
	for _, row := range seedRows {
		if row.RequestID == "" {
			hasEmpty = true
			continue
		}
		if !seen[row.RequestID] {
			seen[row.RequestID] = true
			ids = append(ids, row.RequestID)
		}
	}
	columns := "user_id, username, created_at, type, model_name, quota, prompt_tokens, completion_tokens, use_time, is_stream, channel_id, request_id, " + callAnalyticsProjection(dialect)
	logs := make([]Log, 0, len(seedRows))
	for i := 0; i < len(ids); i += 200 {
		end := min(i+200, len(ids))
		var batch []Log
		// Fetch the entire request, not just attempts inside the time window. Its
		// final completion decides the bucket, even when a retry crosses midnight.
		err := base.Session(&gorm.Session{}).Select(columns).Where("request_id IN ?", ids[i:end]).Limit(CallAnalyticsMaxLogs - len(logs) + 1).Find(&batch).Error
		if err != nil {
			return nil, err
		}
		logs = append(logs, batch...)
		if len(logs) > CallAnalyticsMaxLogs {
			return nil, ErrCallAnalyticsLimit
		}
	}
	if hasEmpty {
		var batch []Log
		if err := seeds.Session(&gorm.Session{}).Select(columns).Where("request_id = ''").Limit(CallAnalyticsMaxLogs - len(logs) + 1).Find(&batch).Error; err != nil {
			return nil, err
		}
		logs = append(logs, batch...)
		if len(logs) > CallAnalyticsMaxLogs {
			return nil, ErrCallAnalyticsLimit
		}
	}
	return buildCallAnalytics(logs, filter), nil
}

func buildCallAnalytics(logs []Log, filter CallAnalyticsFilter) *CallAnalyticsReport {
	groups := make(map[string][]Log)
	for i, log := range logs {
		key := fmt.Sprintf("%d:%s", log.UserId, log.RequestId)
		if log.RequestId == "" {
			key = fmt.Sprintf("missing:%d", i)
		}
		groups[key] = append(groups[key], log)
	}
	report := &CallAnalyticsReport{StartTimestamp: filter.StartTimestamp, EndTimestamp: filter.EndTimestamp, TrendIntervalSeconds: 60, Trend: []CallAnalyticsTrend{}, Users: []CallAnalyticsUser{}, Channels: []CallAnalyticsChannel{}, Errors: []CallAnalyticsError{}, Warnings: []string{"completion_time_basis"}}
	report.Requests.Items = []CallAnalyticsRequest{}
	report.Requests.Page, report.Requests.PageSize = filter.Page, filter.PageSize
	requests := make([]CallAnalyticsRequest, 0, len(groups))
	missingIDs := false
	for _, group := range groups {
		request, ok := summarizeCallRequest(group)
		if !ok || request.CompletedAt < filter.StartTimestamp || request.CompletedAt >= filter.EndTimestamp {
			continue
		}
		if filter.ModelName != "" && request.ModelName != filter.ModelName {
			continue
		}
		if filter.Channel != 0 && request.ChannelID != filter.Channel {
			continue
		}
		if filter.Outcome != "" && request.Outcome != filter.Outcome {
			continue
		}
		requests = append(requests, request)
		if request.RequestID == "" {
			missingIDs = true
		}
		if request.ModelName == "" {
			report.Coverage.UnknownModelRequests++
		}
		if request.FinalRecorded {
			report.Coverage.FinalRecordedRequests++
		} else {
			report.Coverage.InferredRequests++
		}
	}
	sort.Slice(requests, func(i, j int) bool {
		if requests[i].CompletedAt != requests[j].CompletedAt {
			return requests[i].CompletedAt > requests[j].CompletedAt
		}
		if requests[i].RequestID != requests[j].RequestID {
			return requests[i].RequestID > requests[j].RequestID
		}
		return requests[i].UserID < requests[j].UserID
	})
	report.Summary = summarizeCallMetrics(requests, filter)
	report.Distributions = summarizeCallDistributions(requests)
	if report.Coverage.InferredRequests > 0 {
		report.Warnings = append(report.Warnings, "historical_outcomes_incomplete")
	}
	if report.Distributions.CacheShare.Samples < len(requests) {
		report.Warnings = append(report.Warnings, "cache_usage_incomplete")
	}
	if missingIDs {
		report.Warnings = append(report.Warnings, "legacy_missing_request_ids")
	}
	if !common.LogConsumeEnabled || !constant.ErrorLogEnabled {
		report.Warnings = append(report.Warnings, "logging_disabled")
	}
	if report.Coverage.UnknownModelRequests > 0 {
		report.Warnings = append(report.Warnings, "unknown_request_models")
	}
	userGroups := make(map[int][]CallAnalyticsRequest)
	channelGroups := make(map[int][]CallAnalyticsRequest)
	errorGroups := make(map[string]CallAnalyticsError)
	for _, request := range requests {
		userGroups[request.UserID] = append(userGroups[request.UserID], request)
		channelGroups[request.ChannelID] = append(channelGroups[request.ChannelID], request)
		if request.Outcome == "error" {
			key := fmt.Sprintf("%d:%s", request.StatusCode, request.ErrorCode)
			entry := errorGroups[key]
			entry.StatusCode = request.StatusCode
			entry.ErrorCode = request.ErrorCode
			entry.Count++
			errorGroups[key] = entry
		}
	}
	for id, group := range userGroups {
		report.Users = append(report.Users, CallAnalyticsUser{UserID: id, Username: group[0].Username, CallAnalyticsSummary: summarizeCallMetrics(group, filter)})
	}
	for id, group := range channelGroups {
		report.Channels = append(report.Channels, CallAnalyticsChannel{ChannelID: id, CallAnalyticsSummary: summarizeCallMetrics(group, filter)})
	}
	for _, entry := range errorGroups {
		report.Errors = append(report.Errors, entry)
	}
	sort.Slice(report.Users, func(i, j int) bool { return report.Users[i].UserID < report.Users[j].UserID })
	sort.Slice(report.Channels, func(i, j int) bool { return report.Channels[i].ChannelID < report.Channels[j].ChannelID })
	sort.Slice(report.Errors, func(i, j int) bool {
		if report.Errors[i].Count != report.Errors[j].Count {
			return report.Errors[i].Count > report.Errors[j].Count
		}
		if report.Errors[i].StatusCode != report.Errors[j].StatusCode {
			return report.Errors[i].StatusCode < report.Errors[j].StatusCode
		}
		return report.Errors[i].ErrorCode < report.Errors[j].ErrorCode
	})
	for (filter.EndTimestamp-filter.StartTimestamp)/report.TrendIntervalSeconds > 1000 {
		report.TrendIntervalSeconds *= 2
	}
	for bucket := filter.StartTimestamp; bucket < filter.EndTimestamp; bucket += report.TrendIntervalSeconds {
		report.Trend = append(report.Trend, CallAnalyticsTrend{Timestamp: bucket})
	}
	for _, request := range requests {
		entry := &report.Trend[(request.CompletedAt-filter.StartTimestamp)/report.TrendIntervalSeconds]
		entry.Requests++
		entry.Tokens += request.InputTokens + request.OutputTokens
		entry.Quota += request.Quota
		if request.Outcome == "success" {
			entry.Success++
		}
		if request.Outcome == "error" {
			entry.Errors++
		}
	}
	report.Requests.Total = len(requests)
	start := (filter.Page - 1) * filter.PageSize
	if start < len(requests) {
		report.Requests.Items = requests[start:min(start+filter.PageSize, len(requests))]
	}
	return report
}

func summarizeCallRequest(logs []Log) (CallAnalyticsRequest, bool) {
	sort.SliceStable(logs, func(i, j int) bool {
		if logs[i].CreatedAt != logs[j].CreatedAt {
			return logs[i].CreatedAt < logs[j].CreatedAt
		}
		if logs[i].Type == logs[j].Type {
			return logs[i].ChannelId < logs[j].ChannelId
		}
		if logs[i].Type == LogTypeRequestOutcome {
			return false
		}
		if logs[j].Type == LogTypeRequestOutcome {
			return true
		}
		return logs[i].Type == LogTypeError
	})
	r := CallAnalyticsRequest{Outcome: "unknown", AttemptLogs: []CallAnalyticsAttempt{}}
	textRequest, successful, consumed, streamError, cancelled, violation := false, false, false, false, false, false
	var final *Log
	for i := range logs {
		log := &logs[i]
		meta := gjson.Parse(log.Other)
		if meta.Get("task_id").String() != "" {
			return r, false
		}
		if IsCallAnalyticsPath(meta.Get("request_path").String()) {
			textRequest = true
		}
		if log.CreatedAt >= r.CompletedAt {
			r.CompletedAt = log.CreatedAt
			r.UserID = log.UserId
			r.Username = log.Username
			r.RequestID = log.RequestId
			r.ChannelID = log.ChannelId
			if r.ChannelID == 0 {
				r.ChannelID = int(meta.Get("admin_info_routing_target_channel_id").Int())
			}
			r.ModelName = log.ModelName
			if name := meta.Get("requested_model").String(); name != "" {
				r.ModelName = name
			}
			if name := meta.Get("admin_info_requested_model").String(); name != "" {
				r.ModelName = name
			}
			r.EffectiveModel = log.ModelName
			if name := meta.Get("effective_model").String(); name != "" {
				r.EffectiveModel = name
			}
			if name := meta.Get("admin_info_effective_model").String(); name != "" {
				r.EffectiveModel = name
			}
			if rule := meta.Get("admin_info_routing_rule_id").String(); rule != "" {
				r.RoutingRuleID = rule
			}
			if log.UseTime >= 0 {
				v := float64(log.UseTime) * 1000
				r.DurationMs = &v
			}
		}
		if log.Type == LogTypeRequestOutcome {
			final = log
			continue
		}
		if log.Type == LogTypeError {
			r.hasError = true
			r.StatusCode = int(meta.Get("status_code").Int())
			r.ErrorCode = meta.Get("error_code").String()
			r.AttemptLogs = append(r.AttemptLogs, CallAnalyticsAttempt{ChannelID: log.ChannelId, CreatedAt: log.CreatedAt, StatusCode: r.StatusCode, ErrorCode: r.ErrorCode, Type: "error"})
			continue
		}
		if log.Type != LogTypeConsume {
			continue
		}
		r.Quota += int64(log.Quota)
		if meta.Get("violation_fee").Bool() || meta.Get("violation_fee").Int() == 1 {
			violation = true
			r.StatusCode = int(meta.Get("status_code").Int())
			if !r.hasError {
				r.AttemptLogs = append(r.AttemptLogs, CallAnalyticsAttempt{ChannelID: log.ChannelId, CreatedAt: log.CreatedAt, StatusCode: r.StatusCode, ErrorCode: "violation_fee", Type: "error"})
			}
			continue
		}
		r.StatusCode, r.ErrorCode = 200, ""
		consumed = true
		if !log.IsStream || meta.Get("stream_status_status").String() == "ok" {
			successful = true
		}
		r.AttemptLogs = append(r.AttemptLogs, CallAnalyticsAttempt{ChannelID: log.ChannelId, CreatedAt: log.CreatedAt, StatusCode: 200, Type: "consume"})
		read := max(meta.Get("cache_tokens").Int(), 0)
		write := max(meta.Get("cache_write_tokens").Int(), max(meta.Get("cache_creation_tokens").Int(), meta.Get("cache_creation_tokens_5m").Int()+meta.Get("cache_creation_tokens_1h").Int()))
		input := int64(max(log.PromptTokens, 0))
		if total := meta.Get("input_tokens_total"); total.Exists() && total.Int() > 0 {
			input = total.Int()
		} else if meta.Get("usage_semantic").String() == "anthropic" || (meta.Get("usage_semantic").String() == "" && (meta.Get("claude").Bool() || meta.Get("claude").Int() == 1)) {
			input += read + write
		}
		r.InputTokens += input
		r.OutputTokens += int64(max(log.CompletionTokens, 0))
		r.CacheReadTokens += read
		r.CacheWriteTokens += write
		if input > 0 && read <= input && (read > 0 || meta.Get("cache_usage_reported").Bool() || meta.Get("cache_usage_reported").Int() == 1) {
			r.CacheUsageReported = true
			r.cacheInput += input
			r.cacheRead += read
		}
		if frt := meta.Get("frt"); log.IsStream && frt.Type == gjson.Number && frt.Float() >= 0 {
			v := frt.Float()
			r.FRTMs = &v
		}
		if generation := meta.Get("generation_ms"); log.IsStream && generation.Type == gjson.Number && generation.Float() > 0 && log.CompletionTokens > 0 && meta.Get("stream_status_status").String() == "ok" {
			v := float64(log.CompletionTokens) * 1000 / generation.Float()
			r.OutputTPS = &v
		}
		if meta.Get("stream_status_status").String() == "error" {
			streamError = true
		}
		if meta.Get("stream_status_end_reason").String() == "client_gone" {
			cancelled = true
		}
	}
	if !textRequest {
		return r, false
	}
	r.Attempts = len(r.AttemptLogs)
	switch {
	case cancelled:
		r.Outcome = "cancelled"
	case streamError || violation:
		r.Outcome = "error"
		if r.ErrorCode == "" {
			if violation {
				r.ErrorCode = "violation_fee"
			} else {
				r.ErrorCode = "stream_error"
			}
		}
	case successful:
		r.Outcome = "success"
		r.StatusCode = 200
		r.ErrorCode = ""
	case consumed:
		r.Outcome = "unknown"
	case r.hasError:
		r.Outcome = "error"
	}
	if final != nil {
		meta := gjson.Parse(final.Other)
		outcome := meta.Get("request_outcome")
		switch status := outcome.Get("status").String(); status {
		case "success", "error", "cancelled", "unknown":
			r.Outcome = status
			r.FinalRecorded = true
		}
		r.StatusCode = int(outcome.Get("status_code").Int())
		if r.Outcome == "success" {
			r.ErrorCode = ""
		} else if code := meta.Get("error_code").String(); code != "" {
			r.ErrorCode = code
		}
		if duration := outcome.Get("duration_ms"); duration.Type == gjson.Number && duration.Float() >= 0 {
			value := duration.Float()
			r.DurationMs = &value
		}
	}
	if r.RequestID == "" {
		r.Outcome = "unknown"
		r.FinalRecorded = false
	}
	if r.Outcome != "success" {
		r.OutputTPS = nil
	}
	return r, true
}

func summarizeCallMetrics(requests []CallAnalyticsRequest, filter CallAnalyticsFilter) CallAnalyticsSummary {
	s := CallAnalyticsSummary{Requests: len(requests)}
	var durations []float64
	var cacheInput, cacheRead int64
	minutes := make(map[int64]*CallAnalyticsTrend)
	for _, r := range requests {
		switch r.Outcome {
		case "success":
			s.Success++
		case "error":
			s.Errors++
		case "cancelled":
			s.Cancelled++
		default:
			s.Unknown++
		}
		s.Quota += r.Quota
		s.InputTokens += r.InputTokens
		s.OutputTokens += r.OutputTokens
		s.CacheReadTokens += r.CacheReadTokens
		s.CacheWriteTokens += r.CacheWriteTokens
		cacheInput += r.cacheInput
		cacheRead += r.cacheRead
		if r.DurationMs != nil {
			durations = append(durations, *r.DurationMs)
		}
		if r.Attempts > 1 {
			s.RetriedRequests++
			if r.Outcome == "success" && r.hasError {
				s.RecoveredRequests++
			}
		}
		bucket := r.CompletedAt / 60
		if minutes[bucket] == nil {
			minutes[bucket] = &CallAnalyticsTrend{}
		}
		minutes[bucket].Requests++
		minutes[bucket].Tokens += r.InputTokens + r.OutputTokens
	}
	if s.Requests > 0 {
		success, failure := float64(s.Success)/float64(s.Requests), float64(s.Errors)/float64(s.Requests)
		s.SuccessRate = &success
		s.ErrorRate = &failure
	}
	if cacheInput > 0 {
		ratio := float64(cacheRead) / float64(cacheInput)
		s.CacheHitRate = &ratio
	}
	windowMinutes := float64(filter.EndTimestamp-filter.StartTimestamp) / 60
	if windowMinutes > 0 {
		s.AvgRPM = float64(s.Requests) / windowMinutes
		s.AvgTPM = float64(s.InputTokens+s.OutputTokens) / windowMinutes
	}
	if filter.EndTimestamp-filter.StartTimestamp >= 60 {
		recentRPM, recentTPM := 0, int64(0)
		for _, r := range requests {
			if r.CompletedAt >= filter.EndTimestamp-60 && r.CompletedAt < filter.EndTimestamp {
				recentRPM++
				recentTPM += r.InputTokens + r.OutputTokens
			}
		}
		s.RecentRPM, s.RecentTPM = &recentRPM, &recentTPM
	}
	for _, bucket := range minutes {
		s.PeakRPM = max(s.PeakRPM, bucket.Requests)
		s.PeakTPM = max(s.PeakTPM, bucket.Tokens)
	}
	if len(durations) > 0 {
		sort.Float64s(durations)
		sum := 0.0
		for _, value := range durations {
			sum += value
		}
		avg := sum / float64(len(durations))
		p95 := durations[int(math.Ceil(float64(len(durations))*0.95))-1]
		s.AvgDurationMs = &avg
		s.P95DurationMs = &p95
	}
	return s
}

// Rate distributions describe active calendar minutes; they exclude idle minutes
// and retain partial boundary buckets. First response and TPS use successful streams.
func summarizeCallDistributions(requests []CallAnalyticsRequest) CallAnalyticsDistributions {
	var firstResponses, outputTPS, cacheShares []float64
	minutes := make(map[int64]struct {
		requests int
		tokens   int64
	})
	for _, request := range requests {
		if request.Outcome == "success" && request.FRTMs != nil {
			firstResponses = append(firstResponses, *request.FRTMs)
		}
		if request.Outcome == "success" && request.OutputTPS != nil {
			outputTPS = append(outputTPS, *request.OutputTPS)
		}
		if request.CacheUsageReported && request.cacheInput > 0 {
			cacheShares = append(cacheShares, float64(request.cacheRead)/float64(request.cacheInput))
		}
		minute := request.CompletedAt / 60
		value := minutes[minute]
		value.requests++
		value.tokens += request.InputTokens + request.OutputTokens
		minutes[minute] = value
	}
	rpms, tpms := make([]float64, 0, len(minutes)), make([]float64, 0, len(minutes))
	for _, minute := range minutes {
		rpms = append(rpms, float64(minute.requests))
		tpms = append(tpms, float64(minute.tokens))
	}
	return CallAnalyticsDistributions{
		FirstResponseMs: callAnalyticsQuantiles(firstResponses),
		OutputTPS:       callAnalyticsQuantiles(outputTPS),
		RPM:             callAnalyticsQuantiles(rpms),
		TPM:             callAnalyticsQuantiles(tpms),
		CacheShare:      callAnalyticsQuantiles(cacheShares),
	}
}

func callAnalyticsQuantiles(values []float64) CallAnalyticsQuantiles {
	q := CallAnalyticsQuantiles{Samples: len(values)}
	if len(values) == 0 {
		return q
	}
	sort.Float64s(values)
	p50 := values[int(math.Ceil(float64(len(values))*0.5))-1]
	p95 := values[int(math.Ceil(float64(len(values))*0.95))-1]
	p99 := values[int(math.Ceil(float64(len(values))*0.99))-1]
	q.P50, q.P95, q.P99 = &p50, &p95, &p99
	return q
}
