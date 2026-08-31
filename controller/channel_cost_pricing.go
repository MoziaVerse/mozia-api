package controller

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
)

type channelCostChannel struct {
	Id     int    `json:"id"`
	Name   string `json:"name"`
	Status int    `json:"status"`
	Models string `json:"models"`
}

func GetChannelCostPricing(c *gin.Context) {
	costs, err := model.ListChannelCostPricing()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	allChannels, err := model.GetAllChannels(0, 0, true, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	channels := make([]channelCostChannel, 0, len(allChannels))
	for _, channel := range allChannels {
		channels = append(channels, channelCostChannel{
			Id: channel.Id, Name: channel.Name, Status: channel.Status, Models: channel.Models,
		})
	}
	common.ApiSuccess(c, gin.H{
		"items":    costs,
		"channels": channels,
		"models":   model.GetEnabledModels(),
	})
}

func UpsertChannelCostPricing(c *gin.Context) {
	var cost model.ChannelCostPricing
	if err := common.DecodeJson(c.Request.Body, &cost); err != nil {
		common.ApiErrorMsg(c, "无效的参数")
		return
	}
	cost.Id = 0
	cost.CreatedAt = 0
	cost.UpdatedAt = 0
	cost.UpdatedBy = c.GetInt("id")
	if err := model.UpsertChannelCostPricing(&cost); err != nil {
		common.ApiErrorMsg(c, "渠道成本配置失败: "+err.Error())
		return
	}
	recordManageAudit(c, "model_pricing.channel_cost.update", map[string]interface{}{
		"channel_id": cost.ChannelId,
		"model_name": cost.ModelName,
		"mode":       cost.Mode,
	})
	common.ApiSuccess(c, cost)
}

func DeleteChannelCostPricing(c *gin.Context) {
	id, err := strconv.ParseInt(strings.TrimSpace(c.Param("id")), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的渠道成本 ID"})
		return
	}
	deleted, err := model.DeleteChannelCostPricing(id)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "model_pricing.channel_cost.delete", map[string]interface{}{
		"id":      id,
		"deleted": deleted,
	})
	common.ApiSuccess(c, gin.H{"id": id, "deleted": deleted})
}
