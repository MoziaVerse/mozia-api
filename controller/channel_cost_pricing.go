package controller

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

type channelCostChannel struct {
	Id             int    `json:"id"`
	Name           string `json:"name"`
	Models         string `json:"models"`
	DeploymentType string `json:"deployment_type"`
	Status         int    `json:"status"`
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
			Id: channel.Id, Name: channel.Name, Models: channel.Models,
			DeploymentType: channel.DeploymentType, Status: channel.Status,
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
	result, err := service.MutateSupplierCost(c.Request.Context(), &cost, 0, c.GetInt("id"))
	if err != nil {
		common.ApiErrorMsg(c, "渠道成本配置失败: "+err.Error())
		return
	}
	recordManageAudit(c, "model_pricing.channel_cost.update", map[string]interface{}{
		"channel_id": cost.ChannelId,
		"model_name": cost.ModelName,
		"mode":       cost.Mode,
	})
	c.JSON(service.SupplierMutationHTTPStatus(result, "patch"), gin.H{"success": true, "data": cost, "application": result.Application, "revision": result.Revision})
}

func DeleteChannelCostPricing(c *gin.Context) {
	id, err := strconv.ParseInt(strings.TrimSpace(c.Param("id")), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的渠道成本 ID"})
		return
	}
	result, err := service.MutateSupplierCost(c.Request.Context(), nil, id, c.GetInt("id"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAudit(c, "model_pricing.channel_cost.delete", map[string]interface{}{
		"id":      id,
		"deleted": result.Resource,
	})
	c.JSON(service.SupplierMutationHTTPStatus(result, "delete"), gin.H{"success": true, "data": gin.H{"id": id, "deleted": result.Resource}, "application": result.Application, "revision": result.Revision})
}
