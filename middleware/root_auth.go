package middleware

import (
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// RootOnlyAuth 仅基于 Authorization access token 校验超级管理员身份。
// 用于仅供内部管理平台调用的管理接口。
func RootOnlyAuth() func(c *gin.Context) {
	return func(c *gin.Context) {
		accessToken := c.Request.Header.Get("Authorization")
		if accessToken == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"message": "无权进行此操作，未提供 access token",
			})
			c.Abort()
			return
		}
		user := model.ValidateAccessToken(accessToken)
		if user == nil || user.Username == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"message": "无权进行此操作，access token 无效",
			})
			c.Abort()
			return
		}
		if !common.IsValidateRole(user.Role) {
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"message": "无权进行此操作，用户信息无效",
			})
			c.Abort()
			return
		}
		if user.Role < common.RoleRootUser {
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"message": "无权进行此操作，仅限超级管理员",
			})
			c.Abort()
			return
		}

		c.Set("id", user.Id)
		c.Set("username", user.Username)
		c.Set("role", user.Role)
		c.Set("status", user.Status)
		c.Set("group", user.Group)
		c.Set("user_group", user.Group)
		c.Set("use_access_token", strings.HasPrefix(accessToken, "Bearer "))
		c.Next()
	}
}
