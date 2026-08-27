package controller

import (
	"math"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/mozia_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
)

func attachPricingDisplay(pricing []model.Pricing, groupRatio map[string]float64, userId int) []model.Pricing {
	result := make([]model.Pricing, len(pricing))
	copy(result, pricing)
	for i := range result {
		customerRatio := math.Inf(1)
		for _, group := range result[i].EnableGroup {
			if ratio, ok := groupRatio[group]; ok && ratio >= 0 && ratio < customerRatio {
				customerRatio = ratio
			}
		}
		if math.IsInf(customerRatio, 1) {
			customerRatio = 1
		}
		if userId > 0 {
			if ratio, ok := mozia_setting.GetUserModelRatio(userId, result[i].ModelName); ok {
				customerRatio *= ratio
			}
		}
		result[i].DisplayPricing = model.BuildPricingDisplay(result[i], customerRatio)
	}
	return result
}

func filterPricingByUsableGroups(pricing []model.Pricing, usableGroup map[string]string) []model.Pricing {
	if len(pricing) == 0 {
		return pricing
	}
	if len(usableGroup) == 0 {
		return []model.Pricing{}
	}

	filtered := make([]model.Pricing, 0, len(pricing))
	for _, item := range pricing {
		if common.StringsContains(item.EnableGroup, "all") {
			filtered = append(filtered, item)
			continue
		}
		for _, group := range item.EnableGroup {
			if _, ok := usableGroup[group]; ok {
				filtered = append(filtered, item)
				break
			}
		}
	}
	return filtered
}

func GetPricing(c *gin.Context) {
	pricing := model.GetPricing()
	includeInaccessible := c.Query("include_inaccessible") == "true"
	userId, exists := c.Get("id")
	usableGroup := map[string]string{}
	groupRatio := map[string]float64{}
	for s, f := range ratio_setting.GetGroupRatioCopy() {
		groupRatio[s] = f
	}
	var group string
	if exists {
		user, err := model.GetUserCache(userId.(int))
		if err == nil {
			group = user.Group
			for g := range groupRatio {
				ratio, ok := ratio_setting.GetGroupGroupRatio(group, g)
				if ok {
					groupRatio[g] = ratio
				}
			}
		}
	}

	usableGroup = service.GetUserUsableGroups(group)
	pricing = filterPricingByUsableGroups(pricing, usableGroup)
	if exists {
		if id, ok := userId.(int); ok && id > 0 {
			if includeInaccessible {
				pricing = model.AnnotatePricingByMoziaWalletAccess(id, pricing)
			} else {
				pricing = model.FilterPricingByMoziaWalletAccess(id, pricing)
			}
			projectedPricing, _, err := model.ProjectResellerCustomerPricing(id, pricing)
			if err != nil {
				common.ApiError(c, err)
				return
			}
			pricing = projectedPricing
		}
	}
	// check groupRatio contains usableGroup
	for group := range ratio_setting.GetGroupRatioCopy() {
		if _, ok := usableGroup[group]; !ok {
			delete(groupRatio, group)
		}
	}
	displayUserId := 0
	if id, ok := userId.(int); ok {
		displayUserId = id
	}
	pricing = attachPricingDisplay(pricing, groupRatio, displayUserId)

	c.JSON(200, gin.H{
		"success":            true,
		"data":               pricing,
		"vendors":            model.GetVendors(),
		"group_ratio":        groupRatio,
		"usable_group":       usableGroup,
		"supported_endpoint": model.GetSupportedEndpointMap(),
		"auto_groups":        service.GetUserAutoGroup(group),
		"pricing_version":    "structured-pricing-v1",
	})
}

func ResetModelRatio(c *gin.Context) {
	defaultStr := ratio_setting.DefaultModelRatio2JSONString()
	err := model.UpdateOption("ModelRatio", defaultStr)
	if err != nil {
		c.JSON(200, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	err = ratio_setting.UpdateModelRatioByJSONString(defaultStr)
	if err != nil {
		c.JSON(200, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	c.JSON(200, gin.H{
		"success": true,
		"message": "重置模型倍率成功",
	})
}
