package model

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/tidwall/gjson"
	"gorm.io/gorm"
)

const CallAnalyticsMaxPage = 50000

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
	if f.Page < 1 || f.Page > CallAnalyticsMaxPage || f.PageSize < 1 || f.PageSize > 100 {
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
	Requests            int      `json:"requests"`
	Success             int      `json:"success"`
	Errors              int      `json:"errors"`
	Cancelled           int      `json:"cancelled"`
	Unknown             int      `json:"unknown"`
	SuccessRate         *float64 `json:"success_rate"`
	ErrorRate           *float64 `json:"error_rate"`
	Quota               int64    `json:"quota"`
	InputTokens         int64    `json:"input_tokens"`
	OutputTokens        int64    `json:"output_tokens"`
	CacheReadTokens     int64    `json:"cache_read_tokens"`
	CacheWriteTokens    int64    `json:"cache_write_tokens"`
	CacheHitRate        *float64 `json:"cache_hit_rate"`
	AvgRPM              float64  `json:"avg_rpm"`
	RecentRPM           *int     `json:"recent_rpm"`
	PeakRPM             int      `json:"peak_rpm"`
	AvgTPM              float64  `json:"avg_tpm"`
	RecentTPM           *int64   `json:"recent_tpm"`
	PeakTPM             int64    `json:"peak_tpm"`
	AvgDurationMs       *float64 `json:"avg_duration_ms"`
	P95DurationMs       *float64 `json:"p95_duration_ms"`
	DurationApproximate bool     `json:"duration_approximate"`
	RetriedRequests     int      `json:"retried_requests"`
	RecoveredRequests   int      `json:"recovered_requests"`
}

type CallAnalyticsQuantiles struct {
	P50         *float64 `json:"p50"`
	P95         *float64 `json:"p95"`
	P99         *float64 `json:"p99"`
	Samples     int      `json:"samples"`
	Sampled     int      `json:"sampled"`
	Approximate bool     `json:"approximate"`
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

type CallAnalyticsRequestPage struct {
	Items    []CallAnalyticsRequest `json:"items"`
	Page     int                    `json:"p"`
	PageSize int                    `json:"page_size"`
	HasMore  bool                   `json:"has_more"`
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

type callAnalyticsRequestKey struct {
	UserID    int
	RequestID string
}

type callAnalyticsCandidate struct {
	UserID      int
	RequestID   string
	CompletedAt int64
}

func resolveCallAnalyticsUser(ctx context.Context, filter *CallAnalyticsFilter) (bool, error) {
	if filter.User == "" {
		return true, nil
	}
	if id, err := strconv.Atoi(filter.User); err == nil && id > 0 {
		filter.UserID = id
		return true, nil
	}
	var user User
	err := DB.WithContext(ctx).Unscoped().Model(&User{}).Select("id").Where("username = ?", filter.User).First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	filter.UserID = user.Id
	return true, nil
}

func callAnalyticsQueries(ctx context.Context, filter CallAnalyticsFilter) (*gorm.DB, *gorm.DB) {
	base := LOG_DB.WithContext(ctx).Model(&Log{}).Where("type IN ?", []int{LogTypeConsume, LogTypeError, LogTypeRequestOutcome})
	if filter.UserID != 0 {
		base = base.Where("user_id = ?", filter.UserID)
	}
	seeds := base.Session(&gorm.Session{}).Where("created_at >= ? AND created_at < ?", filter.StartTimestamp, filter.EndTimestamp)
	return base, seeds
}

func callAnalyticsColumns() string {
	return "id, user_id, username, created_at, type, model_name, quota, prompt_tokens, completion_tokens, use_time, is_stream, channel_id, request_id, " + callAnalyticsProjection(common.LogDatabaseType())
}

func callAnalyticsModelSeedCondition(dialect common.DatabaseType) string {
	var requested string
	switch dialect {
	case common.DatabaseTypePostgreSQL:
		requested = "(" + callAnalyticsJSONValue("requested_model", dialect) + " #>> '{}') = ? OR (" + callAnalyticsJSONValue("admin_info.requested_model", dialect) + " #>> '{}') = ?"
	case common.DatabaseTypeClickHouse:
		requested = "JSONExtractString(other, 'requested_model') = ? OR JSONExtractString(other, 'admin_info', 'requested_model') = ?"
	case common.DatabaseTypeMySQL:
		requested = "JSON_UNQUOTE(" + callAnalyticsJSONValue("requested_model", dialect) + ") = ? OR JSON_UNQUOTE(" + callAnalyticsJSONValue("admin_info.requested_model", dialect) + ") = ?"
	default:
		requested = callAnalyticsJSONValue("requested_model", dialect) + " = ? OR " + callAnalyticsJSONValue("admin_info.requested_model", dialect) + " = ?"
	}
	return "(model_name = ? OR " + requested + ")"
}

func scanCallAnalyticsKeys(seeds *gorm.DB, batchSize int, visit func([]callAnalyticsRequestKey) error) error {
	idSelect, idOrder, idAfter := "request_id", "request_id", "request_id > ?"
	if common.UsingLogDatabase(common.DatabaseTypeMySQL) {
		// MySQL's default case-insensitive collation can collapse distinct IDs.
		idSelect, idOrder, idAfter = "BINARY request_id AS request_id", "BINARY request_id", "BINARY request_id > BINARY ?"
	}
	query := seeds.Session(&gorm.Session{}).Where("request_id <> ''").
		Distinct("user_id", idSelect).Order("user_id ASC, " + idOrder + " ASC")
	// An open cursor and a batch lookup need two connections. SQLite may use
	// one connection, so it uses short keyset queries instead.
	if db, err := seeds.DB(); err == nil && !common.UsingLogDatabase(common.DatabaseTypeSQLite) && db.Stats().MaxOpenConnections != 1 {
		rows, err := query.Rows()
		if err != nil {
			return err
		}
		defer rows.Close()
		keys := make([]callAnalyticsRequestKey, 0, batchSize)
		for rows.Next() {
			var key callAnalyticsRequestKey
			if err := rows.Scan(&key.UserID, &key.RequestID); err != nil {
				return err
			}
			keys = append(keys, key)
			if len(keys) == batchSize {
				if err := visit(keys); err != nil {
					return err
				}
				keys = make([]callAnalyticsRequestKey, 0, batchSize)
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if len(keys) > 0 {
			return visit(keys)
		}
		return nil
	}
	var last callAnalyticsRequestKey
	hasLast := false
	for {
		page := query.Session(&gorm.Session{}).Limit(batchSize)
		if hasLast {
			page = page.Where("(user_id > ? OR (user_id = ? AND "+idAfter+"))", last.UserID, last.UserID, last.RequestID)
		}
		var keys []callAnalyticsRequestKey
		if err := page.Find(&keys).Error; err != nil {
			return err
		}
		if len(keys) == 0 {
			return nil
		}
		if err := visit(keys); err != nil {
			return err
		}
		last, hasLast = keys[len(keys)-1], true
		if len(keys) < batchSize {
			return nil
		}
	}
}

func loadCallAnalyticsGroups(base *gorm.DB, keys []callAnalyticsRequestKey) (map[callAnalyticsRequestKey][]Log, error) {
	groups := make(map[callAnalyticsRequestKey][]Log, len(keys))
	ids := make([]string, 0, len(keys))
	seenIDs := make(map[string]bool, len(keys))
	for _, key := range keys {
		groups[key] = nil
		if !seenIDs[key.RequestID] {
			seenIDs[key.RequestID] = true
			ids = append(ids, key.RequestID)
		}
	}
	if len(ids) == 0 {
		return groups, nil
	}
	var logs []Log
	if err := base.Session(&gorm.Session{}).Select(callAnalyticsColumns()).Where("request_id IN ?", ids).Find(&logs).Error; err != nil {
		return nil, err
	}
	for _, log := range logs {
		key := callAnalyticsRequestKey{UserID: log.UserId, RequestID: log.RequestId}
		if _, wanted := groups[key]; wanted {
			groups[key] = append(groups[key], log)
		}
	}
	return groups, nil
}

func callAnalyticsMatches(request CallAnalyticsRequest, filter CallAnalyticsFilter) bool {
	return request.CompletedAt >= filter.StartTimestamp && request.CompletedAt < filter.EndTimestamp &&
		(filter.ModelName == "" || request.ModelName == filter.ModelName) &&
		(filter.Channel == 0 || request.ChannelID == filter.Channel) &&
		(filter.Outcome == "" || request.Outcome == filter.Outcome)
}

func callAnalyticsHasModelSeed(logs []Log, filter CallAnalyticsFilter) bool {
	if filter.ModelName == "" {
		return true
	}
	for _, log := range logs {
		if log.CreatedAt < filter.StartTimestamp || log.CreatedAt >= filter.EndTimestamp {
			continue
		}
		meta := gjson.Parse(log.Other)
		if log.ModelName == filter.ModelName || meta.Get("requested_model").String() == filter.ModelName ||
			meta.Get("admin_info_requested_model").String() == filter.ModelName {
			return true
		}
	}
	return false
}

func GetCallAnalytics(ctx context.Context, filter CallAnalyticsFilter) (*CallAnalyticsReport, error) {
	if err := filter.Normalize(time.Now()); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	found, err := resolveCallAnalyticsUser(ctx, &filter)
	if err != nil {
		return nil, err
	}
	aggregate := newCallAnalyticsAggregate(filter)
	if !found {
		return aggregate.finish(), nil
	}

	base, seeds := callAnalyticsQueries(ctx, filter)
	if filter.ModelName != "" {
		seeds = seeds.Where(callAnalyticsModelSeedCondition(common.LogDatabaseType()), filter.ModelName, filter.ModelName, filter.ModelName)
	}

	// SQLite uses short keyset pages; databases with spare connections stream
	// keys through one cursor while each batch's attempts are reduced.
	err = scanCallAnalyticsKeys(seeds, 200, func(keys []callAnalyticsRequestKey) error {
		groups, err := loadCallAnalyticsGroups(base, keys)
		if err != nil {
			return err
		}
		for _, key := range keys {
			request, ok := summarizeCallRequest(groups[key])
			if ok && callAnalyticsMatches(request, filter) {
				aggregate.add(request)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Historical rows without request IDs cannot safely be grouped. Scan them
	// once as separate requests; no candidate IDs or complete logs are retained.
	rows, err := seeds.Session(&gorm.Session{}).Where("request_id = ''").Select(callAnalyticsColumns()).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var log Log
		if err := LOG_DB.ScanRows(rows, &log); err != nil {
			return nil, err
		}
		request, ok := summarizeCallRequest([]Log{log})
		if ok && callAnalyticsMatches(request, filter) {
			aggregate.add(request)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return aggregate.finish(), nil
}

func GetCallAnalyticsRequestPage(ctx context.Context, filter CallAnalyticsFilter) (*CallAnalyticsRequestPage, error) {
	if err := filter.Normalize(time.Now()); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	page := &CallAnalyticsRequestPage{Items: []CallAnalyticsRequest{}, Page: filter.Page, PageSize: filter.PageSize}
	found, err := resolveCallAnalyticsUser(ctx, &filter)
	if err != nil || !found {
		return page, err
	}

	base, seeds := callAnalyticsQueries(ctx, filter)
	if filter.ModelName == "" && filter.Page == 1 {
		return getCallAnalyticsRecentPage(base, seeds, filter, page)
	}
	// Nonempty request IDs are grouped before LIMIT/OFFSET, so retries never
	// split across pages. Model/channel/outcome are checked after hydration.
	const batchSize = 100
	idSelect, idGroup := "request_id", "request_id"
	if common.UsingLogDatabase(common.DatabaseTypeMySQL) {
		idSelect, idGroup = "BINARY request_id AS request_id", "BINARY request_id"
	}
	var candidates []callAnalyticsCandidate
	var candidateGroups map[callAnalyticsRequestKey][]Log
	var legacy []Log
	candidateOffset, candidatePos, legacyOffset, legacyPos := 0, 0, 0, 0
	candidateDone, legacyDone := false, false
	skip := (filter.Page - 1) * filter.PageSize
	matched := 0
	for !page.HasMore {
		if candidatePos >= len(candidates) && !candidateDone {
			candidates = nil
			query := seeds.Session(&gorm.Session{}).
				Select("user_id, " + idSelect + ", MAX(created_at) AS completed_at").
				Where("request_id <> ''").
				Group("user_id, " + idGroup).
				Order("completed_at DESC, request_id DESC, user_id ASC").
				Limit(batchSize).Offset(candidateOffset)
			if filter.ModelName != "" {
				// HAVING preserves the final in-window completion time when
				// only an earlier retry row carries the requested model.
				query = query.Having("MAX(CASE WHEN "+callAnalyticsModelSeedCondition(common.LogDatabaseType())+" THEN 1 ELSE 0 END) > 0", filter.ModelName, filter.ModelName, filter.ModelName)
			}
			err := query.Scan(&candidates).Error
			if err != nil {
				return nil, err
			}
			candidateOffset += len(candidates)
			candidatePos = 0
			candidateDone = len(candidates) < batchSize
			keys := make([]callAnalyticsRequestKey, 0, len(candidates))
			for _, candidate := range candidates {
				keys = append(keys, callAnalyticsRequestKey{UserID: candidate.UserID, RequestID: candidate.RequestID})
			}
			candidateGroups, err = loadCallAnalyticsGroups(base, keys)
			if err != nil {
				return nil, err
			}
		}
		if legacyPos >= len(legacy) && !legacyDone {
			legacy = nil
			// ClickHouse stores old rows with id=0. Identical empty-ID rows
			// therefore have no stable cross-page tie breaker.
			query := seeds.Session(&gorm.Session{}).Select(callAnalyticsColumns()).
				Where("request_id = ''").
				Order("created_at DESC, user_id ASC, id DESC").
				Limit(batchSize).Offset(legacyOffset)
			if filter.ModelName != "" {
				query = query.Where(callAnalyticsModelSeedCondition(common.LogDatabaseType()), filter.ModelName, filter.ModelName, filter.ModelName)
			}
			err := query.Find(&legacy).Error
			if err != nil {
				return nil, err
			}
			legacyOffset += len(legacy)
			legacyPos = 0
			legacyDone = len(legacy) < batchSize
		}

		hasCandidate, hasLegacy := candidatePos < len(candidates), legacyPos < len(legacy)
		if !hasCandidate && !hasLegacy {
			break
		}
		var group []Log
		if hasCandidate && (!hasLegacy ||
			candidates[candidatePos].CompletedAt > legacy[legacyPos].CreatedAt ||
			candidates[candidatePos].CompletedAt == legacy[legacyPos].CreatedAt &&
				(candidates[candidatePos].RequestID > "" || candidates[candidatePos].UserID < legacy[legacyPos].UserId)) {
			candidate := candidates[candidatePos]
			candidatePos++
			group = candidateGroups[callAnalyticsRequestKey{UserID: candidate.UserID, RequestID: candidate.RequestID}]
		} else {
			group = []Log{legacy[legacyPos]}
			legacyPos++
		}
		if !callAnalyticsHasModelSeed(group, filter) {
			continue
		}
		request, ok := summarizeCallRequest(group)
		if !ok || !callAnalyticsMatches(request, filter) {
			continue
		}
		if matched >= skip {
			if len(page.Items) == filter.PageSize {
				page.HasMore = true
				break
			}
			page.Items = append(page.Items, request)
		}
		matched++
	}
	return page, nil
}

// The common first page follows the time-leading log index and stops after
// one extra matching request. Its first row for an ID is the in-window anchor;
// full attempts are still fetched before the result is accepted.
func getCallAnalyticsRecentPage(base, seeds *gorm.DB, filter CallAnalyticsFilter, page *CallAnalyticsRequestPage) (*CallAnalyticsRequestPage, error) {
	batchSize := max(16, min(100, filter.PageSize+1))
	requestIDOrder := "request_id"
	if common.UsingLogDatabase(common.DatabaseTypeMySQL) {
		requestIDOrder = "BINARY request_id"
	}
	seen := make(map[callAnalyticsRequestKey]bool)
	offset := 0
	for !page.HasMore {
		var rows []Log
		err := seeds.Session(&gorm.Session{}).Select(callAnalyticsColumns()).
			Order("created_at DESC, " + requestIDOrder + " DESC, user_id ASC, id DESC").
			Limit(batchSize).Offset(offset).Find(&rows).Error
		if err != nil {
			return nil, err
		}
		if len(rows) == 0 {
			break
		}
		offset += len(rows)
		keys := make([]callAnalyticsRequestKey, 0, len(rows))
		for _, row := range rows {
			if row.RequestId == "" {
				continue
			}
			key := callAnalyticsRequestKey{UserID: row.UserId, RequestID: row.RequestId}
			if !seen[key] {
				seen[key] = true
				keys = append(keys, key)
			}
		}
		groups, err := loadCallAnalyticsGroups(base, keys)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			group := []Log{row}
			if row.RequestId != "" {
				key := callAnalyticsRequestKey{UserID: row.UserId, RequestID: row.RequestId}
				if _, ok := groups[key]; !ok {
					continue
				}
				group = groups[key]
				delete(groups, key)
			}
			request, ok := summarizeCallRequest(group)
			if !ok || !callAnalyticsMatches(request, filter) {
				continue
			}
			if len(page.Items) == filter.PageSize {
				page.HasMore = true
				break
			}
			page.Items = append(page.Items, request)
		}
		if len(rows) < batchSize {
			break
		}
	}
	return page, nil
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

const (
	callAnalyticsSampleLimit          = 100000
	callAnalyticsBreakdownSampleLimit = 2048
)

// Keep exact nearest-rank values for ordinary reports and bounded, clearly
// marked estimates for very large windows. Samples counts every observation.
type callAnalyticsSample struct {
	values []float64
	count  int
	limit  int
	rng    *rand.Rand
}

func (s *callAnalyticsSample) add(value float64) {
	s.count++
	if len(s.values) < s.limit {
		s.values = append(s.values, value)
		return
	}
	if s.rng == nil {
		s.rng = rand.New(rand.NewSource(1))
	}
	if i := s.rng.Intn(s.count); i < s.limit {
		s.values[i] = value
	}
}

func (s *callAnalyticsSample) quantiles() CallAnalyticsQuantiles {
	q := callAnalyticsQuantiles(s.values)
	q.Samples = s.count
	q.Approximate = s.count > len(s.values)
	return q
}

type callAnalyticsMetrics struct {
	summary     CallAnalyticsSummary
	cacheInput  int64
	cacheRead   int64
	durations   callAnalyticsSample
	durationSum float64
	minutes     map[int64]CallAnalyticsTrend
	recentRPM   int
	recentTPM   int64
}

func (m *callAnalyticsMetrics) add(request CallAnalyticsRequest, filter CallAnalyticsFilter) {
	s := &m.summary
	s.Requests++
	switch request.Outcome {
	case "success":
		s.Success++
	case "error":
		s.Errors++
	case "cancelled":
		s.Cancelled++
	default:
		s.Unknown++
	}
	s.Quota += request.Quota
	s.InputTokens += request.InputTokens
	s.OutputTokens += request.OutputTokens
	s.CacheReadTokens += request.CacheReadTokens
	s.CacheWriteTokens += request.CacheWriteTokens
	m.cacheInput += request.cacheInput
	m.cacheRead += request.cacheRead
	if request.DurationMs != nil {
		m.durationSum += *request.DurationMs
		m.durations.add(*request.DurationMs)
	}
	if request.Attempts > 1 {
		s.RetriedRequests++
		if request.Outcome == "success" && request.hasError {
			s.RecoveredRequests++
		}
	}
	if m.minutes == nil {
		m.minutes = make(map[int64]CallAnalyticsTrend)
	}
	bucket := request.CompletedAt / 60
	minute := m.minutes[bucket]
	minute.Requests++
	minute.Tokens += request.InputTokens + request.OutputTokens
	m.minutes[bucket] = minute
	if request.CompletedAt >= filter.EndTimestamp-60 && request.CompletedAt < filter.EndTimestamp {
		m.recentRPM++
		m.recentTPM += request.InputTokens + request.OutputTokens
	}
}

func (m *callAnalyticsMetrics) finish(filter CallAnalyticsFilter) CallAnalyticsSummary {
	s := m.summary
	if s.Requests > 0 {
		success, failure := float64(s.Success)/float64(s.Requests), float64(s.Errors)/float64(s.Requests)
		s.SuccessRate, s.ErrorRate = &success, &failure
	}
	if m.cacheInput > 0 {
		ratio := float64(m.cacheRead) / float64(m.cacheInput)
		s.CacheHitRate = &ratio
	}
	windowMinutes := float64(filter.EndTimestamp-filter.StartTimestamp) / 60
	s.AvgRPM = float64(s.Requests) / windowMinutes
	s.AvgTPM = float64(s.InputTokens+s.OutputTokens) / windowMinutes
	if filter.EndTimestamp-filter.StartTimestamp >= 60 {
		s.RecentRPM, s.RecentTPM = &m.recentRPM, &m.recentTPM
	}
	for _, minute := range m.minutes {
		s.PeakRPM = max(s.PeakRPM, minute.Requests)
		s.PeakTPM = max(s.PeakTPM, minute.Tokens)
	}
	if m.durations.count > 0 {
		sort.Float64s(m.durations.values)
		avg := m.durationSum / float64(m.durations.count)
		p95 := m.durations.values[int(math.Ceil(float64(len(m.durations.values))*0.95))-1]
		s.AvgDurationMs, s.P95DurationMs = &avg, &p95
		s.DurationApproximate = m.durations.count > len(m.durations.values)
	}
	return s
}

type callAnalyticsUserMetrics struct {
	username    string
	completedAt int64
	requestID   string
	metrics     callAnalyticsMetrics
}

type callAnalyticsAggregate struct {
	filter         CallAnalyticsFilter
	report         *CallAnalyticsReport
	summary        callAnalyticsMetrics
	users          map[int]*callAnalyticsUserMetrics
	channels       map[int]*callAnalyticsMetrics
	errors         map[string]CallAnalyticsError
	trend          map[int64]CallAnalyticsTrend
	firstResponses callAnalyticsSample
	outputTPS      callAnalyticsSample
	cacheShares    callAnalyticsSample
	missingIDs     bool
}

func newCallAnalyticsAggregate(filter CallAnalyticsFilter) *callAnalyticsAggregate {
	report := &CallAnalyticsReport{
		StartTimestamp:       filter.StartTimestamp,
		EndTimestamp:         filter.EndTimestamp,
		TrendIntervalSeconds: 60,
		Trend:                []CallAnalyticsTrend{},
		Users:                []CallAnalyticsUser{},
		Channels:             []CallAnalyticsChannel{},
		Errors:               []CallAnalyticsError{},
		Warnings:             []string{"completion_time_basis"},
	}
	for (filter.EndTimestamp-filter.StartTimestamp)/report.TrendIntervalSeconds > 1000 {
		report.TrendIntervalSeconds *= 2
	}
	report.Requests.Items = []CallAnalyticsRequest{}
	report.Requests.Page, report.Requests.PageSize = filter.Page, filter.PageSize
	return &callAnalyticsAggregate{
		filter: filter, report: report,
		summary:        callAnalyticsMetrics{durations: callAnalyticsSample{limit: callAnalyticsSampleLimit}},
		users:          make(map[int]*callAnalyticsUserMetrics),
		channels:       make(map[int]*callAnalyticsMetrics),
		errors:         make(map[string]CallAnalyticsError),
		trend:          make(map[int64]CallAnalyticsTrend),
		firstResponses: callAnalyticsSample{limit: callAnalyticsSampleLimit},
		outputTPS:      callAnalyticsSample{limit: callAnalyticsSampleLimit},
		cacheShares:    callAnalyticsSample{limit: callAnalyticsSampleLimit},
	}
}

func (a *callAnalyticsAggregate) add(request CallAnalyticsRequest) {
	a.summary.add(request, a.filter)
	user := a.users[request.UserID]
	if user == nil {
		user = &callAnalyticsUserMetrics{metrics: callAnalyticsMetrics{durations: callAnalyticsSample{limit: callAnalyticsBreakdownSampleLimit}}}
		a.users[request.UserID] = user
	}
	if request.CompletedAt > user.completedAt ||
		request.CompletedAt == user.completedAt && request.RequestID > user.requestID {
		user.username = request.Username
		user.completedAt = request.CompletedAt
		user.requestID = request.RequestID
	}
	user.metrics.add(request, a.filter)
	channel := a.channels[request.ChannelID]
	if channel == nil {
		channel = &callAnalyticsMetrics{durations: callAnalyticsSample{limit: callAnalyticsBreakdownSampleLimit}}
		a.channels[request.ChannelID] = channel
	}
	channel.add(request, a.filter)
	if request.Outcome == "error" {
		key := fmt.Sprintf("%d:%s", request.StatusCode, request.ErrorCode)
		entry := a.errors[key]
		entry.StatusCode, entry.ErrorCode = request.StatusCode, request.ErrorCode
		entry.Count++
		a.errors[key] = entry
	}
	if request.RequestID == "" {
		a.missingIDs = true
	}
	if request.ModelName == "" {
		a.report.Coverage.UnknownModelRequests++
	}
	if request.FinalRecorded {
		a.report.Coverage.FinalRecordedRequests++
	} else {
		a.report.Coverage.InferredRequests++
	}
	if request.Outcome == "success" && request.FRTMs != nil {
		a.firstResponses.add(*request.FRTMs)
	}
	if request.Outcome == "success" && request.OutputTPS != nil {
		a.outputTPS.add(*request.OutputTPS)
	}
	if request.CacheUsageReported && request.cacheInput > 0 {
		a.cacheShares.add(float64(request.cacheRead) / float64(request.cacheInput))
	}
	bucket := a.filter.StartTimestamp +
		(request.CompletedAt-a.filter.StartTimestamp)/a.report.TrendIntervalSeconds*a.report.TrendIntervalSeconds
	entry := a.trend[bucket]
	entry.Requests++
	entry.Tokens += request.InputTokens + request.OutputTokens
	entry.Quota += request.Quota
	if request.Outcome == "success" {
		entry.Success++
	}
	if request.Outcome == "error" {
		entry.Errors++
	}
	a.trend[bucket] = entry
}

func (a *callAnalyticsAggregate) finish() *CallAnalyticsReport {
	report := a.report
	report.Summary = a.summary.finish(a.filter)
	report.Requests.Total = report.Summary.Requests
	rpms, tpms := make([]float64, 0, len(a.summary.minutes)), make([]float64, 0, len(a.summary.minutes))
	for _, minute := range a.summary.minutes {
		rpms = append(rpms, float64(minute.Requests))
		tpms = append(tpms, float64(minute.Tokens))
	}
	report.Distributions = CallAnalyticsDistributions{
		FirstResponseMs: a.firstResponses.quantiles(),
		OutputTPS:       a.outputTPS.quantiles(),
		RPM:             callAnalyticsQuantiles(rpms),
		TPM:             callAnalyticsQuantiles(tpms),
		CacheShare:      a.cacheShares.quantiles(),
	}
	for id, user := range a.users {
		report.Users = append(report.Users, CallAnalyticsUser{
			UserID: id, Username: user.username,
			CallAnalyticsSummary: user.metrics.finish(a.filter),
		})
	}
	for id, channel := range a.channels {
		report.Channels = append(report.Channels, CallAnalyticsChannel{
			ChannelID: id, CallAnalyticsSummary: channel.finish(a.filter),
		})
	}
	for _, entry := range a.errors {
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
	for bucket := a.filter.StartTimestamp; bucket < a.filter.EndTimestamp; bucket += report.TrendIntervalSeconds {
		entry := a.trend[bucket]
		entry.Timestamp = bucket
		report.Trend = append(report.Trend, entry)
	}
	if report.Coverage.InferredRequests > 0 {
		report.Warnings = append(report.Warnings, "historical_outcomes_incomplete")
	}
	if report.Distributions.CacheShare.Samples < report.Summary.Requests {
		report.Warnings = append(report.Warnings, "cache_usage_incomplete")
	}
	if a.missingIDs {
		report.Warnings = append(report.Warnings, "legacy_missing_request_ids")
	}
	if !common.LogConsumeEnabled || !constant.ErrorLogEnabled {
		report.Warnings = append(report.Warnings, "logging_disabled")
	}
	if report.Coverage.UnknownModelRequests > 0 {
		report.Warnings = append(report.Warnings, "unknown_request_models")
	}
	return report
}

func callAnalyticsQuantiles(values []float64) CallAnalyticsQuantiles {
	q := CallAnalyticsQuantiles{Samples: len(values), Sampled: len(values)}
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
