package controller

import (
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func GetCallAnalytics(c *gin.Context) {
	filter, ok := bindCallAnalyticsFilter(c)
	if !ok {
		return
	}
	report, err := model.GetCallAnalytics(c.Request.Context(), filter)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, report)
}

func GetCallAnalyticsRequestPage(c *gin.Context) {
	filter, ok := bindCallAnalyticsFilter(c)
	if !ok {
		return
	}
	page, err := model.GetCallAnalyticsRequestPage(c.Request.Context(), filter)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, page)
}

func bindCallAnalyticsFilter(c *gin.Context) (model.CallAnalyticsFilter, bool) {
	var filter model.CallAnalyticsFilter
	if err := c.ShouldBindQuery(&filter); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid analytics query"})
		return filter, false
	}
	if err := filter.Normalize(time.Now()); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return filter, false
	}
	return filter, true
}
