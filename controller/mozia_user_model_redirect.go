package controller

import (
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/mozia_setting"

	"github.com/gin-gonic/gin"
)

type moziaUserModelRedirectResponse struct {
	mozia_setting.UserModelRedirect
	Username string `json:"username"`
}

type upsertMoziaUserModelRedirectRequest struct {
	mozia_setting.UserModelRedirect
	SSOSub string `json:"sso_sub"`
}

func GetMoziaUserModelRedirects(c *gin.Context) {
	rules := mozia_setting.GetUserModelRedirects()
	userIds := make([]int, 0, len(rules))
	seen := make(map[int]struct{}, len(rules))
	for _, rule := range rules {
		if _, ok := seen[rule.UserId]; ok || rule.AllUsers {
			continue
		}
		seen[rule.UserId] = struct{}{}
		userIds = append(userIds, rule.UserId)
	}
	usernames, err := model.GetUsernamesByIds(userIds)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	response := make([]moziaUserModelRedirectResponse, 0, len(rules))
	for _, rule := range rules {
		response = append(response, moziaUserModelRedirectResponse{
			UserModelRedirect: rule,
			Username:          usernames[rule.UserId],
		})
	}
	common.ApiSuccess(c, response)
}

func UpsertMoziaUserModelRedirect(c *gin.Context) {
	var request upsertMoziaUserModelRedirectRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		common.ApiError(c, err)
		return
	}

	request.SSOSub = strings.TrimSpace(request.SSOSub)
	if request.SSOSub != "" {
		userSSO, err := model.GetUserSSOBySub(request.SSOSub)
		if err != nil {
			common.ApiErrorMsg(c, "SSO user not found")
			return
		}
		if request.UserId > 0 && request.UserId != userSSO.UserId {
			common.ApiErrorMsg(c, "user_id does not match sso_sub")
			return
		}
		request.UserId = userSSO.UserId
	}

	rule := mozia_setting.NormalizeUserModelRedirect(request.UserModelRedirect)
	if err := mozia_setting.ValidateUserModelRedirect(rule); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	if !rule.AllUsers {
		if _, err := model.GetUserById(rule.UserId, false); err != nil {
			common.ApiErrorMsg(c, "用户不存在")
			return
		}
	}
	if err := service.UpsertMoziaUserModelRedirect(rule); err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAuditFor(c, rule.UserId, "mozia.user_model_redirect_upsert", map[string]interface{}{
		"rule_id":                rule.ID,
		"target_channel_id":      rule.TargetChannelId,
		"all_users":              rule.AllUsers,
		"priority":               rule.Priority,
		"disabled":               rule.Disabled,
		"source_model":           rule.SourceModel,
		"target_model":           rule.TargetModel,
		"only_thinking_disabled": rule.OnlyThinkingDisabled,
		"seamless":               rule.Seamless,
	})
	common.ApiSuccess(c, rule)
}

func DeleteMoziaUserModelRedirect(c *gin.Context) {
	userId, err := strconv.Atoi(c.Param("user_id"))
	if err != nil || userId < 0 {
		common.ApiErrorMsg(c, "无效的用户 ID")
		return
	}
	sourceModel := strings.TrimSpace(c.Query("source_model"))
	if sourceModel == "" {
		common.ApiErrorMsg(c, "source_model must not be empty")
		return
	}
	if err := service.DeleteMoziaUserModelRedirect(userId, sourceModel, c.Query("rule_id")); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	recordManageAuditFor(c, userId, "mozia.user_model_redirect_delete", map[string]interface{}{
		"rule_id":      c.Query("rule_id"),
		"source_model": sourceModel,
	})
	common.ApiSuccess(c, nil)
}

// Return only routing metadata; never expose channel credentials or overrides.
func GetMoziaRoutingTargets(c *gin.Context) {
	channels, err := model.GetAllChannels(0, -1, false, true)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	type target struct {
		ID     int      `json:"id"`
		Name   string   `json:"name"`
		Models []string `json:"models"`
		Status int      `json:"status"`
	}
	result := make([]target, 0, len(channels))
	for _, channel := range channels {
		result = append(result, target{channel.Id, channel.Name, channel.GetModels(), channel.Status})
	}
	common.ApiSuccess(c, result)
}
