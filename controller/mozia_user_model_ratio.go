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

type moziaUserModelRatioWithUsername struct {
	mozia_setting.UserModelRatio
	Username string `json:"username"`
}

func GetMoziaUserModelRatios(c *gin.Context) {
	rules := mozia_setting.GetUserModelRatios()
	userIds := make([]int, 0, len(rules))
	seenUserIds := make(map[int]struct{}, len(rules))
	for _, rule := range rules {
		if _, ok := seenUserIds[rule.UserId]; ok {
			continue
		}
		seenUserIds[rule.UserId] = struct{}{}
		userIds = append(userIds, rule.UserId)
	}
	usernames, err := model.GetUsernamesByIds(userIds)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	response := make([]moziaUserModelRatioWithUsername, 0, len(rules))
	for _, rule := range rules {
		response = append(response, moziaUserModelRatioWithUsername{
			UserModelRatio: rule,
			Username:       usernames[rule.UserId],
		})
	}
	common.ApiSuccess(c, response)
}

func UpsertMoziaUserModelRatio(c *gin.Context) {
	var rule mozia_setting.UserModelRatio
	if err := c.ShouldBindJSON(&rule); err != nil {
		common.ApiError(c, err)
		return
	}
	rule = mozia_setting.NormalizeUserModelRatio(rule)
	if err := mozia_setting.ValidateUserModelRatio(rule); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	if _, err := model.GetUserById(rule.UserId, false); err != nil {
		common.ApiErrorMsg(c, "用户不存在")
		return
	}
	if rule.Scope == mozia_setting.UserRatioScopeChannel {
		if _, err := model.GetChannelById(rule.ChannelId, false); err != nil {
			common.ApiErrorMsg(c, "渠道不存在")
			return
		}
	}
	if err := service.UpsertMoziaUserModelRatio(rule); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, rule)
}

func DeleteMoziaUserModelRatio(c *gin.Context) {
	userId, err := strconv.Atoi(c.Param("user_id"))
	if err != nil || userId <= 0 {
		common.ApiErrorMsg(c, "无效的用户 ID")
		return
	}
	modelName := strings.TrimSpace(c.Query("model"))
	scope := strings.ToLower(strings.TrimSpace(c.Query("scope")))
	if scope == "" {
		scope = mozia_setting.UserRatioScopeModel
	}
	channelId, _ := strconv.Atoi(c.Query("channel_id"))
	rule := mozia_setting.NormalizeUserModelRatio(mozia_setting.UserModelRatio{
		UserId:    userId,
		Scope:     scope,
		Model:     modelName,
		ChannelId: channelId,
		Ratio:     1,
	})
	if err := mozia_setting.ValidateUserModelRatio(rule); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	if err := service.DeleteMoziaUserModelRatio(rule); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	common.ApiSuccess(c, nil)
}
