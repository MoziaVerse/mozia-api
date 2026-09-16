package controller

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
)

func getAdminTargetToken(c *gin.Context) (*model.User, *model.Token) {
	userID, userErr := strconv.Atoi(c.Param("id"))
	tokenID, tokenErr := strconv.Atoi(c.Param("token_id"))
	if userErr != nil || tokenErr != nil || userID <= 0 || tokenID <= 0 {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return nil, nil
	}
	user, err := model.GetUserById(userID, false)
	if err != nil {
		common.ApiError(c, err)
		return nil, nil
	}
	if !canManageTargetRole(c.GetInt("role"), user.Role) {
		common.ApiErrorI18n(c, i18n.MsgUserNoPermissionHigherLevel)
		return nil, nil
	}
	token, err := model.GetTokenByIds(tokenID, userID)
	if err != nil {
		common.ApiError(c, err)
		return nil, nil
	}
	return user, token
}

func GetUserTokenGroup(c *gin.Context) {
	_, token := getAdminTargetToken(c)
	if token == nil {
		return
	}
	common.ApiSuccess(c, gin.H{
		"id": token.Id, "user_id": token.UserId, "name": token.Name,
		"group": token.Group, "status": token.Status,
	})
}

func UpdateUserTokenGroup(c *gin.Context) {
	var request struct {
		Group         string  `json:"group"`
		ExpectedGroup *string `json:"expected_group"`
	}
	if err := common.DecodeJson(c.Request.Body, &request); err != nil || request.ExpectedGroup == nil || strings.TrimSpace(request.Group) == "" {
		common.ApiErrorI18n(c, i18n.MsgInvalidParams)
		return
	}
	user, token := getAdminTargetToken(c)
	if token == nil {
		return
	}
	group := strings.TrimSpace(request.Group)
	if !service.GroupInUserUsableGroups(user.Group, group) || (group != "auto" && !ratio_setting.ContainsGroupRatio(group)) {
		common.ApiErrorI18n(c, i18n.MsgDistributorGroupAccessDenied)
		return
	}
	if token.Group != *request.ExpectedGroup {
		c.JSON(http.StatusConflict, gin.H{"success": false, "message": "Token group changed; reload before updating"})
		return
	}
	oldGroup := token.Group
	cacheInvalidated, err := token.UpdateGroup(group)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	recordManageAuditFor(c, user.Id, "token.group_update", map[string]interface{}{
		"token_id": token.Id, "token_name": token.Name, "from": oldGroup, "to": group,
		"cache_invalidated": cacheInvalidated,
	})
	common.ApiSuccess(c, gin.H{
		"id": token.Id, "user_id": token.UserId, "group": token.Group,
		"cache_invalidated": cacheInvalidated,
	})
}
