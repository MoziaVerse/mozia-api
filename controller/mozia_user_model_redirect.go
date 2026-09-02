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
	UserId               int    `json:"user_id"`
	SSOSub               string `json:"sso_sub"`
	SourceModel          string `json:"source_model"`
	TargetModel          string `json:"target_model"`
	OnlyThinkingDisabled bool   `json:"only_thinking_disabled"`
	Seamless             bool   `json:"seamless"`
}

func GetMoziaUserModelRedirects(c *gin.Context) {
	rules := mozia_setting.GetUserModelRedirects()
	userIds := make([]int, 0, len(rules))
	seen := make(map[int]struct{}, len(rules))
	for _, rule := range rules {
		if _, ok := seen[rule.UserId]; ok {
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

	rule := mozia_setting.NormalizeUserModelRedirect(mozia_setting.UserModelRedirect{
		UserId:               request.UserId,
		SourceModel:          request.SourceModel,
		TargetModel:          request.TargetModel,
		OnlyThinkingDisabled: request.OnlyThinkingDisabled,
		Seamless:             request.Seamless,
	})
	if err := mozia_setting.ValidateUserModelRedirect(rule); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	if _, err := model.GetUserById(rule.UserId, false); err != nil {
		common.ApiErrorMsg(c, "用户不存在")
		return
	}
	if err := service.UpsertMoziaUserModelRedirect(rule); err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAuditFor(c, rule.UserId, "mozia.user_model_redirect_upsert", map[string]interface{}{
		"source_model":           rule.SourceModel,
		"target_model":           rule.TargetModel,
		"only_thinking_disabled": rule.OnlyThinkingDisabled,
		"seamless":               rule.Seamless,
	})
	common.ApiSuccess(c, rule)
}

func DeleteMoziaUserModelRedirect(c *gin.Context) {
	userId, err := strconv.Atoi(c.Param("user_id"))
	if err != nil || userId <= 0 {
		common.ApiErrorMsg(c, "无效的用户 ID")
		return
	}
	sourceModel := strings.TrimSpace(c.Query("source_model"))
	if sourceModel == "" {
		common.ApiErrorMsg(c, "source_model must not be empty")
		return
	}
	if err := service.DeleteMoziaUserModelRedirect(userId, sourceModel); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	recordManageAuditFor(c, userId, "mozia.user_model_redirect_delete", map[string]interface{}{
		"source_model": sourceModel,
	})
	common.ApiSuccess(c, nil)
}
