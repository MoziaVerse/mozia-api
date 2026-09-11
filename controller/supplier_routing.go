package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

func GetSupplierRouting(c *gin.Context) {
	cfg, err := model.ReadSupplierResources(model.DB)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var revisions []model.RoutingRevision
	if err := model.DB.Select("id", "created_by", "created_at").Order("id DESC").Limit(30).Find(&revisions).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	var channels []struct {
		ID     int    `json:"id"`
		Name   string `json:"name"`
		Models string `json:"models"`
		Type   int    `json:"type"`
		Status int    `json:"status"`
	}
	if err := model.DB.Model(&model.Channel{}).Select("id", "name", "models", "type", "status").Order("id").Find(&channels).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	var settings model.SupplierRoutingSettings
	if err := model.ReadSupplierOption(model.DB, model.SupplierRoutingSettingsKey, &settings); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"config": cfg, "settings": settings, "revisions": revisions, "channels": channels})
}

type supplierPublicationRequest struct {
	ExpectedRevision int64                       `json:"expected_revision"`
	RollbackRevision int64                       `json:"rollback_revision"`
	Config           model.SupplierRoutingConfig `json:"config"`
}

func ValidateSupplierRouting(c *gin.Context) {
	var request supplierPublicationRequest
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 2*1024*1024)
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorMsg(c, "Invalid supplier routing configuration")
		return
	}
	if err := model.ValidateSupplierRoutingConfig(&request.Config); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, request.Config)
}

func PublishSupplierRouting(c *gin.Context) {
	c.JSON(http.StatusGone, gin.H{"success": false, "code": "whole_configuration_write_retired", "message": "Whole configuration writes are retired. Refresh the page and save each supplier, pool, binding or rule individually."})
}

func GetSupplierAttempts(c *gin.Context) {
	var attempts []model.SupplierAttempt
	query := model.DB.Order("id DESC").Limit(100)
	if poolID, err := strconv.ParseInt(c.Query("pool_id"), 10, 64); err == nil && poolID > 0 {
		query = query.Where("pool_id = ?", poolID)
	}
	if err := query.Find(&attempts).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, attempts)
}

func GetSupplierRoutingStats(c *gin.Context) {
	var rows []struct {
		SupplierID       int64   `json:"supplier_id"`
		PoolID           int64   `json:"pool_id"`
		Model            string  `json:"model"`
		GroupName        string  `json:"group_name"`
		Kind             string  `json:"kind"`
		Status           string  `json:"status"`
		Requests         int64   `json:"requests"`
		AvgTTFTMs        float64 `json:"avg_ttft_ms"`
		AvgLatencyMs     float64 `json:"avg_latency_ms"`
		OutputTokens     int64   `json:"output_tokens"`
		FirstShare       float64 `json:"first_share_percent"`
		HealthState      string  `json:"health_state"`
		HealthScale      int64   `json:"health_scale"`
		PriorityFallback bool    `json:"priority_fallback"`
		OutcomeClass     string  `json:"outcome_class"`
	}
	start := time.Now().Truncate(time.Hour).Unix()
	err := model.DB.Model(&model.SupplierAttempt{}).Select("supplier_id, pool_id, model, group_name, kind, status, priority_fallback, outcome_class, COUNT(*) AS requests, COALESCE(AVG(CASE WHEN ttft_ms > 0 THEN ttft_ms ELSE NULL END), 0) AS avg_ttft_ms, AVG(latency_ms) AS avg_latency_ms, SUM(output_tokens) AS output_tokens").Where("created_at >= ? AND status <> ?", start, "cancelled").Group("supplier_id, pool_id, model, group_name, kind, status, priority_fallback, outcome_class").Find(&rows).Error
	if err != nil {
		common.ApiError(c, err)
		return
	}
	// Aggregate first dispatches by supplier, not by its number of channels/pools.
	denominators := map[string]int64{}
	numerators := map[string]int64{}
	for _, row := range rows {
		if row.Kind == "first" {
			scope := fmt.Sprintf("%q:%q", row.Model, row.GroupName)
			denominators[scope] += row.Requests
			numerators[fmt.Sprintf("%d:%s", row.SupplierID, scope)] += row.Requests
		}
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()
	for i := range rows {
		row := &rows[i]
		scope := fmt.Sprintf("%q:%q", row.Model, row.GroupName)
		if total := denominators[scope]; total > 0 && row.Kind == "first" {
			row.FirstShare = 100 * float64(numerators[fmt.Sprintf("%d:%s", row.SupplierID, scope)]) / float64(total)
		}
		row.HealthState = "unavailable"
		if common.RedisEnabled && common.RDB != nil {
			key := fmt.Sprintf("supplier-routing:health:%d:%d:%s:%s", row.PoolID, len(row.Model), row.Model, row.GroupName)
			health, err := common.RDB.HGetAll(ctx, key).Result()
			if err == nil {
				row.HealthState = health["state"]
				row.HealthScale, _ = strconv.ParseInt(health["scale"], 10, 64)
				if row.HealthState == "" {
					row.HealthState = "trial"
				}
			}
		}
	}
	// Aggregate in SQL so the dashboard does not load one row per user request.
	outcomes := model.DB.Model(&model.SupplierAttempt{}).Select("request_id, MAX(CASE WHEN kind = 'first' THEN 1 ELSE 0 END) AS first_count, MAX(CASE WHEN kind = 'first' AND status = 'success' THEN 1 ELSE 0 END) AS first_success, MAX(CASE WHEN status = 'success' THEN 1 ELSE 0 END) AS final_success, MAX(CASE WHEN status IN ('pending','unknown') THEN 1 ELSE 0 END) AS unresolved").Where("created_at >= ? AND kind IN ? AND status <> ?", start, []string{"first", "retry"}, "cancelled").Group("request_id")
	var summary struct {
		Requests     int64 `json:"requests"`
		FirstSuccess int64 `json:"first_success"`
		FinalSuccess int64 `json:"final_success"`
		Unresolved   int64 `json:"unresolved"`
	}
	err = model.DB.Table("(?) AS request_outcomes", outcomes).Select("COUNT(*) AS requests, COALESCE(SUM(first_success),0) AS first_success, COALESCE(SUM(final_success),0) AS final_success, COALESCE(SUM(CASE WHEN final_success = 0 AND unresolved > 0 THEN 1 ELSE 0 END),0) AS unresolved").Where("first_count > 0").Scan(&summary).Error
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"hour_start": start, "rows": rows, "summary": summary})
}

func ReconcileSupplierAttempt(c *gin.Context) {
	var request struct {
		Status   string `json:"status"`
		Cost     string `json:"cost"`
		Currency string `json:"currency"`
		Note     string `json:"note"`
	}
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiError(c, err)
		return
	}
	if request.Note == "" || len(request.Note) > 2000 || (request.Status != "failed" && request.Status != "success" && request.Status != "cancelled") {
		common.ApiErrorMsg(c, "A verified result and reconciliation note are required")
		return
	}
	var attempt model.SupplierAttempt
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		common.ApiErrorMsg(c, "Invalid attempt ID")
		return
	}
	if err := model.DB.First(&attempt, id).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	if attempt.CostStatus != "pending" {
		common.ApiErrorMsg(c, "This call has already been settled")
		return
	}
	if len(request.Cost) > 64 {
		common.ApiErrorMsg(c, "Invalid procurement cost")
		return
	}
	amount, costErr := decimal.NewFromString(request.Cost)
	if costErr != nil || len(request.Cost) > 64 || amount.Exponent() < -8 || amount.Exponent() > 8 || amount.IsNegative() || amount.GreaterThan(decimal.NewFromInt(1000000000000)) || (request.Currency != "CNY" && request.Currency != "USD") {
		common.ApiErrorMsg(c, "A non-negative procurement cost and CNY/USD currency are required")
		return
	}
	if request.Status == "cancelled" && !amount.IsZero() {
		common.ApiErrorMsg(c, "An unsent call must have zero cost")
		return
	}
	if (attempt.Status == "pending" || attempt.Status == "unknown") && time.Now().UnixMilli() < attempt.CapacityUntil {
		common.ApiErrorMsg(c, "Wait for the accepted upstream execution deadline before reconciling an uncertain call")
		return
	}
	// Reconciliation never releases Redis capacity early. Its conservative
	// execution lease remains in force until the accepted provider timeout.
	result := model.DB.Model(&attempt).Where("cost_status = ?", "pending").Updates(map[string]any{"status": request.Status, "cost_status": "reconciled", "cost": amount.StringFixed(8), "currency": request.Currency, "finished_at": time.Now().Unix()})
	if result.Error != nil || result.RowsAffected != 1 {
		common.ApiErrorMsg(c, "Call settlement changed; reload before reconciling")
		return
	}
	recordManageAudit(c, "supplier_attempt.reconcile", map[string]interface{}{"attempt_id": id, "status": request.Status, "note": request.Note})
	common.ApiSuccess(c, gin.H{"id": id})
}

// PreviewSupplierModels reuses the channel credentials and existing HTTP helper.
// The returned declaration is a proposal; publication still validates acceptance.
func PreviewSupplierModels(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		common.ApiErrorMsg(c, "Invalid channel ID")
		return
	}
	channel, err := model.GetChannelById(id, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if channel.Type != constant.ChannelTypeOpenAI {
		common.ApiErrorMsg(c, "Supplier model preview requires an OpenAI-compatible channel")
		return
	}
	key, _, apiErr := channel.GetNextEnabledKey()
	if apiErr != nil {
		common.ApiError(c, apiErr)
		return
	}
	headers, err := buildFetchModelsHeaders(channel, key)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	base := channel.GetBaseURL()
	if base == "" {
		base = constant.ChannelBaseURLs[channel.Type]
	}
	body, err := GetResponseBody(http.MethodGet, strings.TrimRight(base, "/")+"/v1/models", channel, headers)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if len(body) > 2*1024*1024 {
		common.ApiErrorMsg(c, "Supplier model manifest exceeds 2 MiB")
		return
	}
	var manifest struct {
		Data []struct {
			ID              string               `json:"id"`
			Version         string               `json:"version"`
			ContextLength   int64                `json:"context_length"`
			MaxOutputTokens int64                `json:"max_output_tokens"`
			Tools           bool                 `json:"tools"`
			JSON            bool                 `json:"json"`
			Pricing         json.RawMessage      `json:"pricing,omitempty"`
			Capacity        model.SupplierLimits `json:"capacity"`
		} `json:"data"`
	}
	if err := common.Unmarshal(body, &manifest); err != nil {
		common.ApiError(c, err)
		return
	}
	if len(manifest.Data) > 128 {
		common.ApiErrorMsg(c, "Supplier model manifest exceeds 128 models")
		return
	}
	if !authz.Can(c.GetInt("id"), c.GetInt("role"), authz.ModelPricingRead) {
		for i := range manifest.Data {
			manifest.Data[i].Pricing = nil
		}
	}
	recordManageAudit(c, "supplier_routing.models.preview", map[string]interface{}{"channel_id": id, "models": len(manifest.Data)})
	common.ApiSuccess(c, manifest.Data)
}
