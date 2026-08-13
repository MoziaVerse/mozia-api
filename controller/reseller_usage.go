package controller

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

const resellerTaskDefaultPage = 1
const resellerTaskDefaultPageSize = 20
const resellerTaskMaxPageSize = 100

type resellerUsageResponse struct {
	Summary resellerUsageSummaryResponse `json:"summary"`
	Items   []resellerUsageItemResponse  `json:"items"`
}

type resellerUsageSummaryResponse struct {
	RequestCount         string `json:"request_count"`
	PromptTokens         string `json:"prompt_tokens"`
	CompletionTokens     string `json:"completion_tokens"`
	TotalTokens          string `json:"total_tokens"`
	CustomerQuota        string `json:"customer_quota"`
	CustomerQuotaDisplay string `json:"customer_quota_display"`
	ModelCount           int    `json:"model_count"`
}

type resellerUsageItemResponse struct {
	CustomerId           int    `json:"customer_id"`
	Model                string `json:"model"`
	RequestCount         string `json:"request_count"`
	PromptTokens         string `json:"prompt_tokens"`
	CompletionTokens     string `json:"completion_tokens"`
	TotalTokens          string `json:"total_tokens"`
	CustomerQuota        string `json:"customer_quota"`
	CustomerQuotaDisplay string `json:"customer_quota_display"`
}

func GetResellerManagementUsage(c *gin.Context) {
	resellerContext, ok := resellerManagementContext(c)
	if !ok {
		return
	}
	customerId, ok := optionalPositiveQueryID(c, "customer_id")
	if !ok {
		return
	}
	startTimestamp, endTimestamp, ok := resellerTimeRangeQuery(c)
	if !ok {
		return
	}
	if customerId != nil {
		if _, err := model.GetResellerCustomerRecord(resellerContext.ResellerId, *customerId, false); err != nil {
			handleResellerUsageError(c, err)
			return
		}
	}
	result, err := model.ListResellerUsage(resellerContext.ResellerId, customerId, startTimestamp, endTimestamp)
	if err != nil {
		handleResellerUsageError(c, err)
		return
	}
	writeResellerAdminSuccess(c, http.StatusOK, resellerUsageResponse{
		Summary: resellerUsageSummaryResponse{
			RequestCount:         strconv.FormatInt(result.Summary.RequestCount, 10),
			PromptTokens:         strconv.FormatInt(result.Summary.PromptTokens, 10),
			CompletionTokens:     strconv.FormatInt(result.Summary.CompletionTokens, 10),
			TotalTokens:          strconv.FormatInt(result.Summary.TotalTokens, 10),
			CustomerQuota:        strconv.FormatInt(result.Summary.CustomerQuota, 10),
			CustomerQuotaDisplay: formatResellerUsageQuota(result.Summary.CustomerQuota),
			ModelCount:           result.Summary.ModelCount,
		},
		Items: resellerUsageItemsResponse(result.Items),
	})
}

func GetResellerManagementTasks(c *gin.Context) {
	resellerContext, ok := resellerManagementContext(c)
	if !ok {
		return
	}
	customerId, ok := requiredPositiveQueryID(c, "customer_id")
	if !ok {
		return
	}
	startTimestamp, endTimestamp, ok := resellerTimeRangeQuery(c)
	if !ok {
		return
	}
	page, pageSize, ok := resellerTaskPageQuery(c)
	if !ok {
		return
	}
	customer, err := model.GetResellerCustomerRecord(resellerContext.ResellerId, customerId, false)
	if err != nil {
		handleResellerUsageError(c, err)
		return
	}
	result, err := model.ListResellerCustomerTasks(customer, page, pageSize, startTimestamp, endTimestamp)
	if err != nil {
		handleResellerUsageError(c, err)
		return
	}
	writeResellerAdminSuccess(c, http.StatusOK, result)
}

func resellerUsageItemsResponse(items []model.ResellerUsageItem) []resellerUsageItemResponse {
	response := make([]resellerUsageItemResponse, 0, len(items))
	for _, item := range items {
		response = append(response, resellerUsageItemResponse{
			CustomerId:           item.CustomerId,
			Model:                item.Model,
			RequestCount:         strconv.FormatInt(item.RequestCount, 10),
			PromptTokens:         strconv.FormatInt(item.PromptTokens, 10),
			CompletionTokens:     strconv.FormatInt(item.CompletionTokens, 10),
			TotalTokens:          strconv.FormatInt(item.TotalTokens, 10),
			CustomerQuota:        strconv.FormatInt(item.CustomerQuota, 10),
			CustomerQuotaDisplay: formatResellerUsageQuota(item.CustomerQuota),
		})
	}
	return response
}

func resellerTimeRangeQuery(c *gin.Context) (*int64, *int64, bool) {
	startRaw, hasStart := c.GetQuery("start_timestamp")
	endRaw, hasEnd := c.GetQuery("end_timestamp")
	if hasStart != hasEnd {
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid request")
		return nil, nil, false
	}
	if !hasStart {
		return nil, nil, true
	}
	startTimestamp, err := strconv.ParseInt(strings.TrimSpace(startRaw), 10, 64)
	if err != nil || startTimestamp < 1 {
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid request")
		return nil, nil, false
	}
	endTimestamp, err := strconv.ParseInt(strings.TrimSpace(endRaw), 10, 64)
	if err != nil || endTimestamp < 1 || startTimestamp > endTimestamp {
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid request")
		return nil, nil, false
	}
	return &startTimestamp, &endTimestamp, true
}

func requiredPositiveQueryID(c *gin.Context, name string) (int, bool) {
	value := strings.TrimSpace(c.Query(name))
	if value == "" {
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid request")
		return 0, false
	}
	id, err := strconv.Atoi(value)
	if err != nil || id < 1 {
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid request")
		return 0, false
	}
	return id, true
}

func resellerTaskPageQuery(c *gin.Context) (int, int, bool) {
	page := resellerTaskDefaultPage
	if raw := strings.TrimSpace(c.Query("p")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 {
			middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid request")
			return 0, 0, false
		}
		page = value
	}
	pageSize := resellerTaskDefaultPageSize
	if raw := strings.TrimSpace(c.Query("page_size")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > resellerTaskMaxPageSize {
			middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid request")
			return 0, 0, false
		}
		pageSize = value
	}
	return page, pageSize, true
}

func formatResellerUsageQuota(quota int64) string {
	if int64(int(quota)) != quota {
		return strconv.FormatInt(quota, 10)
	}
	return logger.FormatQuota(int(quota))
}

func handleResellerUsageError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, model.ErrResellerCustomerNotFound):
		middleware.AbortResellerRequest(c, http.StatusNotFound, middleware.ResellerErrorNotFound, "reseller customer not found")
	default:
		logger.LogError(c.Request.Context(), "reseller usage database error: "+err.Error())
		middleware.AbortResellerRequest(c, http.StatusInternalServerError, middleware.ResellerErrorInternal, "internal error")
	}
}
