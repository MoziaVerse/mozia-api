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
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
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
	cfg.Prices = nil // Procurement quotes retain their separate permission boundary.
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

// The same range and filters drive tables, request summaries and procurement totals.
func supplierHistoryQuery(c *gin.Context, defaultStart int64) (*gorm.DB, int64, int64, error) {
	start, end := defaultStart, time.Now().Unix()+1
	for name, target := range map[string]*int64{"start_timestamp": &start, "end_timestamp": &end} {
		if raw, exists := c.GetQuery(name); exists {
			value, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || value < 0 {
				return nil, 0, 0, fmt.Errorf("invalid %s", name)
			}
			*target = value
		}
	}
	if start >= end {
		return nil, 0, 0, fmt.Errorf("start_timestamp must precede end_timestamp")
	}
	query := model.DB.WithContext(c.Request.Context()).Model(&model.SupplierAttempt{}).Where("created_at >= ? AND created_at < ?", start, end)
	for _, name := range []string{"supplier_id", "pool_id"} {
		if raw, exists := c.GetQuery(name); exists {
			id, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || id <= 0 {
				return nil, 0, 0, fmt.Errorf("invalid %s", name)
			}
			query = query.Where(name+" = ?", id)
		}
	}
	for _, name := range []string{"model", "group_name"} {
		if value := c.Query(name); value != "" {
			query = query.Where(name+" = ?", value)
		}
	}
	return query, start, end, nil
}

func GetSupplierAttempts(c *gin.Context) {
	query, _, _, err := supplierHistoryQuery(c, 0)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	attempts := []model.SupplierAttempt{}
	// Preserve the existing latest-records response for clients without pagination.
	if c.Query("p") == "" {
		if err := query.Order("id DESC").Limit(100).Find(&attempts).Error; err != nil {
			common.ApiError(c, err)
			return
		}
		common.ApiSuccess(c, attempts)
		return
	}
	page, err := strconv.Atoi(c.Query("p"))
	size, sizeErr := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if err != nil || sizeErr != nil || page < 1 || page > 1000000 || size < 1 || size > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "Invalid pagination"})
		return
	}
	query = query.Where("kind <> ?", "shadow")
	var total int64
	if err := query.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	if err := query.Order("id DESC").Offset((page - 1) * size).Limit(size).Find(&attempts).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, common.PageInfo{Page: page, PageSize: size, Total: int(total), Items: attempts})
}

func GetSupplierRealtime(c *gin.Context) {
	if !common.RedisEnabled || common.RDB == nil {
		common.ApiSuccess(c, gin.H{"available": false, "rows": []service.SupplierRealtimeRow{}})
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	rows, now, err := service.ReadSupplierRealtime(ctx)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"available": true, "window_start": (now.Unix()/60 - 4) * 60, "window_end": now.Unix(), "rows": rows})
}

func GetSupplierRoutingStats(c *gin.Context) {
	rows := []struct {
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
		PriorityFallback bool    `json:"priority_fallback"`
		OutcomeClass     string  `json:"outcome_class"`
	}{}
	base, start, end, err := supplierHistoryQuery(c, time.Now().Truncate(time.Hour).Unix())
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	err = base.Session(&gorm.Session{}).Select("supplier_id, pool_id, model, group_name, kind, status, priority_fallback, outcome_class, COUNT(*) AS requests, COALESCE(AVG(CASE WHEN ttft_ms > 0 THEN ttft_ms ELSE NULL END), 0) AS avg_ttft_ms, AVG(latency_ms) AS avg_latency_ms, SUM(output_tokens) AS output_tokens").Where("status <> ?", "cancelled").Group("supplier_id, pool_id, model, group_name, kind, status, priority_fallback, outcome_class").Find(&rows).Error
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
	for i := range rows {
		row := &rows[i]
		scope := fmt.Sprintf("%q:%q", row.Model, row.GroupName)
		if total := denominators[scope]; total > 0 && row.Kind == "first" {
			row.FirstShare = 100 * float64(numerators[fmt.Sprintf("%d:%s", row.SupplierID, scope)]) / float64(total)
		}
	}

	// Aggregate in SQL so the dashboard does not load one row per user request.
	outcomes := base.Session(&gorm.Session{}).Select("request_id, MAX(CASE WHEN kind = 'first' AND status = 'success' THEN 1 ELSE 0 END) AS first_success, MAX(CASE WHEN status = 'success' THEN 1 ELSE 0 END) AS final_success, MAX(CASE WHEN status IN ('pending','unknown') THEN 1 ELSE 0 END) AS unresolved").Where("kind IN ? AND status <> ?", []string{"first", "retry"}, "cancelled").Group("request_id")
	var summary struct {
		Requests     int64 `json:"requests"`
		FirstSuccess int64 `json:"first_success"`
		FinalSuccess int64 `json:"final_success"`
		Unresolved   int64 `json:"unresolved"`
	}
	err = model.DB.Table("(?) AS request_outcomes", outcomes).Select("COUNT(*) AS requests, COALESCE(SUM(first_success),0) AS first_success, COALESCE(SUM(final_success),0) AS final_success, COALESCE(SUM(CASE WHEN final_success = 0 AND unresolved > 0 THEN 1 ELSE 0 END),0) AS unresolved").Scan(&summary).Error
	if err != nil {
		common.ApiError(c, err)
		return
	}
	var costs []struct {
		Currency           string `json:"currency"`
		Total              string `json:"total"`
		RetryCost          string `json:"retry_cost"`
		FailedCost         string `json:"failed_cost"`
		Pending            int64  `json:"pending"`
		SuccessfulRequests int64  `json:"successful_requests"`
	}
	if authz.Can(c.GetInt("id"), c.GetInt("role"), authz.ModelPricingRead) {
		known := "cost_status IN ('calculated','reconciled')"
		err = base.Session(&gorm.Session{}).Select("currency, COALESCE(SUM(CASE WHEN "+known+" THEN CAST(cost AS DECIMAL(30,8)) ELSE 0 END),0) AS total, COALESCE(SUM(CASE WHEN kind = 'retry' AND "+known+" THEN CAST(cost AS DECIMAL(30,8)) ELSE 0 END),0) AS retry_cost, COALESCE(SUM(CASE WHEN status <> 'success' AND "+known+" THEN CAST(cost AS DECIMAL(30,8)) ELSE 0 END),0) AS failed_cost, SUM(CASE WHEN cost_status = 'pending' THEN 1 ELSE 0 END) AS pending, COUNT(DISTINCT CASE WHEN status = 'success' THEN request_id END) AS successful_requests").Where("kind IN ? AND status <> ?", []string{"first", "retry"}, "cancelled").Group("currency").Scan(&costs).Error
		if err != nil {
			common.ApiError(c, err)
			return
		}
	}
	common.ApiSuccess(c, gin.H{"hour_start": start, "start_timestamp": start, "end_timestamp": end, "rows": rows, "summary": summary, "costs": costs})
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
