package controller

import (
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func GetCallAnalytics(c *gin.Context) {
	var filter model.CallAnalyticsFilter
	if err := c.ShouldBindQuery(&filter); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid analytics query"})
		return
	}
	if err := filter.Normalize(time.Now()); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}
	report, err := model.GetCallAnalytics(c.Request.Context(), filter)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, report)
}

func GetCallAnalyticsUsers(c *gin.Context) {
	users, err := model.SearchCallAnalyticsUsers(c.Request.Context(), c.Query("keyword"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, users)
}
