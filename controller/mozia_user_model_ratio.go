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

func GetMoziaUserModelRatios(c *gin.Context) {
	common.ApiSuccess(c, mozia_setting.GetUserModelRatios())
}

func UpsertMoziaUserModelRatio(c *gin.Context) {
	var rule mozia_setting.UserModelRatio
	if err := c.ShouldBindJSON(&rule); err != nil {
		common.ApiError(c, err)
		return
	}
	rule.Model = strings.TrimSpace(rule.Model)
	if err := mozia_setting.ValidateUserModelRatio(rule); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	if _, err := model.GetUserById(rule.UserId, false); err != nil {
		common.ApiErrorMsg(c, "用户不存在")
		return
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
	if modelName == "" {
		common.ApiErrorMsg(c, "model 不能为空")
		return
	}
	if err := service.DeleteMoziaUserModelRatio(userId, modelName); err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	common.ApiSuccess(c, nil)
}
