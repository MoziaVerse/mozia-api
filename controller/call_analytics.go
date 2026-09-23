package controller

import (
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func GetCallAnalytics(c *gin.Context) {
	var query struct {
		StartTimestamp int64  `form:"start_timestamp"`
		EndTimestamp   int64  `form:"end_timestamp"`
		UserID         int    `form:"user_id"`
		ModelName      string `form:"model_name"`
		Channel        int    `form:"channel"`
		Outcome        string `form:"outcome"`
		Page           int    `form:"p"`
		PageSize       int    `form:"page_size"`
	}
	if err := c.ShouldBindQuery(&query); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "invalid analytics query"})
		return
	}
	filter := model.CallAnalyticsFilter{StartTimestamp: query.StartTimestamp, EndTimestamp: query.EndTimestamp, UserID: query.UserID, ModelName: query.ModelName, Channel: query.Channel, Outcome: query.Outcome, Page: query.Page, PageSize: query.PageSize}
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
