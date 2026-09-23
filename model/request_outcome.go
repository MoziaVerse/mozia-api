package model

import (
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// Request outcomes are private analytics records, never billing records.
const LogTypeRequestOutcome = 8

// ObserveCallConsumption keeps only outcome metadata, even when billing logs are disabled.
func ObserveCallConsumption(c *gin.Context, params RecordConsumeLogParams) {
	if c == nil || !c.GetBool("call_analytics_enabled") {
		return
	}
	c.Set("call_analytics_consumed", true)
	c.Set("call_analytics_stream", params.IsStream)
	if stream, ok := params.Other["stream_status"].(map[string]interface{}); ok {
		c.Set("call_analytics_stream_status", stream["status"])
		c.Set("call_analytics_stream_end", stream["end_reason"])
	}
	if violation, ok := params.Other["violation_fee"].(bool); ok && violation {
		c.Set("call_analytics_error_code", "violation_fee")
	}
}

// RecordRequestOutcome runs after the relay completes. It does not retain request
// bodies, credentials or upstream error messages, and never changes accounting.
func RecordRequestOutcome(c *gin.Context, started time.Time, panicked bool) {
	if c.GetInt("id") <= 0 {
		return
	}
	statusCode := c.Writer.Status()
	outcome := "unknown"
	errorCode := c.GetString("call_analytics_error_code")
	streamStatus := c.GetString("call_analytics_stream_status")
	switch {
	case panicked:
		outcome, statusCode, errorCode = "error", http.StatusInternalServerError, "internal_panic"
	case c.GetString("call_analytics_stream_end") == "client_gone":
		outcome = "cancelled"
	case errorCode != "" || c.GetInt("call_analytics_error_status") >= 400 || streamStatus == "error" || statusCode >= 400:
		outcome = "error"
	case c.GetBool("call_analytics_consumed") && statusCode >= 200 && statusCode < 300 && (!c.GetBool("call_analytics_stream") || streamStatus == "ok"):
		outcome = "success"
	case c.Request.Context().Err() != nil:
		outcome = "cancelled"
	}
	if (outcome == "success" && !common.LogConsumeEnabled) || (outcome != "success" && !constant.ErrorLogEnabled) {
		return
	}
	requested := c.GetString("call_analytics_requested_model")
	if requested == "" && strings.Contains(c.Request.URL.Path, "/models/") {
		_, name, _ := strings.Cut(c.Request.URL.Path, "/models/")
		requested, _, _ = strings.Cut(name, ":")
	}
	if requested == "" {
		// Early authentication/rate-limit rejection may precede model parsing.
		// Only inspect a small body already cached by the request pipeline.
		if value, ok := c.Get(common.KeyBodyStorage); ok {
			if storage, ok := value.(common.BodyStorage); ok && storage.Size() <= 64*1024 {
				if body, err := storage.Bytes(); err == nil {
					requested = gjson.GetBytes(body, "model").String()
				}
			}
		}
	}
	effective := c.GetString("call_analytics_effective_model")
	if effective == "" {
		effective = requested
	}
	finished := time.Now()
	other := map[string]interface{}{
		"request_path":    c.Request.URL.Path,
		"requested_model": requested,
		"effective_model": effective,
		"error_code":      errorCode,
		"request_outcome": map[string]interface{}{
			"status": outcome, "status_code": statusCode,
			"started_at": started.Unix(), "duration_ms": finished.Sub(started).Milliseconds(),
		},
		"admin_info": map[string]interface{}{
			"routing_rule_id":           common.GetContextKeyString(c, constant.ContextKeyConditionalRouteID),
			"routing_target_channel_id": common.GetContextKeyInt(c, constant.ContextKeyRouteChannelID),
			"use_channel":               c.GetStringSlice("use_channel"),
		},
	}
	if errorStatus := c.GetInt("call_analytics_error_status"); errorStatus != 0 {
		other["status_code"] = errorStatus
	}
	log := &Log{
		Type: LogTypeRequestOutcome, UserId: c.GetInt("id"), Username: c.GetString("username"),
		CreatedAt: finished.Unix(), RequestId: c.GetString(common.RequestIdKey),
		ModelName: requested, ChannelId: common.GetContextKeyInt(c, constant.ContextKeyChannelId),
		TokenId:  common.GetContextKeyInt(c, constant.ContextKeyTokenId),
		Group:    common.GetContextKeyString(c, constant.ContextKeyUsingGroup),
		IsStream: c.GetBool("call_analytics_stream"), Other: common.MapToJsonStr(other),
	}
	if err := createLog(log); err != nil {
		logger.LogError(c, "failed to record request outcome: "+err.Error())
	}
}
