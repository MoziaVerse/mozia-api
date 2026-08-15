package controller

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/authz"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type managedAdmin struct {
	Id          int                  `json:"id"`
	Username    string               `json:"username"`
	DisplayName string               `json:"display_name"`
	Status      int                  `json:"status"`
	Permissions authz.PermissionsMap `json:"permissions"`
}

type managedAdminPermissionsRequest struct {
	Permissions authz.PermissionsMap `json:"permissions"`
}

// GetPermissionCatalog returns the permission schema used by the client to
// render the permission editor: the registry of resources with their actions
// and display label keys, plus the roles with their baseline grant matrices.
// Defining it in the authz package keeps the schema in a single place.
func GetPermissionCatalog(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"resources": authz.Catalog(),
			"roles":     authz.Roles(),
		},
	})
}

func GetManagedAdmins(c *gin.Context) {
	pageInfo := common.GetPageQuery(c)
	var users []*model.User
	query := model.DB.Model(&model.User{}).Where("role = ?", common.RoleAdminUser)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		common.ApiError(c, err)
		return
	}
	if err := query.Order("id desc").Limit(pageInfo.GetPageSize()).Offset(pageInfo.GetStartIdx()).Omit("password").Find(&users).Error; err != nil {
		common.ApiError(c, err)
		return
	}

	admins := make([]managedAdmin, 0, len(users))
	for _, user := range users {
		admins = append(admins, managedAdmin{
			Id:          user.Id,
			Username:    user.Username,
			DisplayName: user.DisplayName,
			Status:      user.Status,
			Permissions: authz.Capabilities(user.Id, user.Role),
		})
	}

	pageInfo.SetTotal(int(total))
	pageInfo.SetItems(admins)
	common.ApiSuccess(c, pageInfo)
}

func UpdateManagedAdminPermissions(c *gin.Context) {
	userId, err := strconv.Atoi(c.Param("id"))
	if err != nil || userId <= 0 {
		common.ApiErrorMsg(c, "无效的管理员 ID")
		return
	}
	user, err := model.GetUserById(userId, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if user.Role != common.RoleAdminUser {
		common.ApiErrorMsg(c, "只能为普通管理员分配运营权限")
		return
	}

	var req managedAdminPermissionsRequest
	if err := common.DecodeJson(c.Request.Body, &req); err != nil || req.Permissions == nil {
		common.ApiErrorMsg(c, "无效的权限配置")
		return
	}
	if err := model.DB.Transaction(func(tx *gorm.DB) error {
		return authz.SetUserPermissionsInTx(tx, userId, req.Permissions)
	}); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := authz.ReloadPolicy(); err != nil {
		common.ApiError(c, err)
		return
	}

	recordManageAuditFor(c, userId, "authz.permissions_update", map[string]interface{}{
		"target_user_id": userId,
		"username":       user.Username,
	})
	common.ApiSuccess(c, nil)
}
