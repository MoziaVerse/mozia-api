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

type moziaUserThinkingDisabledRedirectResponse struct {
	UserId   int    `json:"user_id"`
	Username string `json:"username"`
}

func GetMoziaUserThinkingDisabledRedirects(c *gin.Context) {
	userIds := mozia_setting.GetUserThinkingDisabledRedirectUserIds()
	usernames, err := model.GetUsernamesByIds(userIds)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	response := make([]moziaUserThinkingDisabledRedirectResponse, 0, len(userIds))
	for _, userId := range userIds {
		response = append(response, moziaUserThinkingDisabledRedirectResponse{
			UserId:   userId,
			Username: usernames[userId],
		})
	}
	common.ApiSuccess(c, response)
}

func UpsertMoziaUserThinkingDisabledRedirect(c *gin.Context) {
	var request struct {
		SSOSub string `json:"sso_sub"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		common.ApiError(c, err)
		return
	}
	request.SSOSub = strings.TrimSpace(request.SSOSub)
	userSSO, err := model.GetUserSSOBySub(request.SSOSub)
	if err != nil {
		common.ApiErrorMsg(c, "SSO user not found")
		return
	}
	if err := service.UpsertMoziaUserThinkingDisabledRedirect(userSSO.UserId); err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAuditFor(c, userSSO.UserId, "mozia.user_model_redirect_upsert", map[string]interface{}{
		"source_model": mozia_setting.ThinkingDisabledSourceModel,
		"target_model": mozia_setting.ThinkingDisabledTargetModel,
	})
	common.ApiSuccess(c, moziaUserThinkingDisabledRedirectResponse{UserId: userSSO.UserId})
}

func DeleteMoziaUserThinkingDisabledRedirect(c *gin.Context) {
	userId, err := strconv.Atoi(c.Param("user_id"))
	if err != nil || userId <= 0 {
		common.ApiErrorMsg(c, "无效的用户 ID")
		return
	}
	if err := service.DeleteMoziaUserThinkingDisabledRedirect(userId); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	recordManageAuditFor(c, userId, "mozia.user_model_redirect_delete", map[string]interface{}{
		"source_model": mozia_setting.ThinkingDisabledSourceModel,
	})
	common.ApiSuccess(c, nil)
}
