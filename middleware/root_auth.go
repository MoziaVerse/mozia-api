package middleware

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"
)

// RootOnlyAuth validates root access for internal management endpoints.
// It accepts the dashboard session used by the default frontend, and keeps the
// legacy Authorization access-token path for service-to-service callers.
func RootOnlyAuth() func(c *gin.Context) {
	return func(c *gin.Context) {
		if handled := rootSessionAuth(c); handled {
			return
		}
		accessToken := c.Request.Header.Get("Authorization")
		if accessToken == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"message": "无权进行此操作，未提供 access token",
			})
			c.Abort()
			return
		}
		user, err := model.ValidateAccessToken(accessToken)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"message": "无权进行此操作，access token 校验失败",
			})
			c.Abort()
			return
		}
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
			c.JSON(http.StatusForbidden, gin.H{
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

func rootSessionAuth(c *gin.Context) bool {
	session := sessions.Default(c)
	username := session.Get("username")
	role := session.Get("role")
	id := session.Get("id")
	status := session.Get("status")

	if username == nil && role == nil && id == nil && status == nil {
		return false
	}

	usernameStr, usernameOk := username.(string)
	roleInt, roleOk := role.(int)
	idInt, idOk := id.(int)
	statusInt, statusOk := status.(int)
	if !usernameOk || !roleOk || !idOk || !statusOk || !common.IsValidateRole(roleInt) || strings.TrimSpace(usernameStr) == "" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "无权进行此操作，用户信息无效",
		})
		c.Abort()
		return true
	}

	apiUserIdStr := c.Request.Header.Get("New-Api-User")
	if apiUserIdStr == "" {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "无权进行此操作，未提供用户 ID",
		})
		c.Abort()
		return true
	}
	apiUserId, err := strconv.Atoi(apiUserIdStr)
	if err != nil || idInt != apiUserId {
		c.JSON(http.StatusUnauthorized, gin.H{
			"success": false,
			"message": "无权进行此操作，用户 ID 不匹配",
		})
		c.Abort()
		return true
	}
	if statusInt == common.UserStatusDisabled {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"message": "无权进行此操作，用户已被禁用",
		})
		c.Abort()
		return true
	}
	if roleInt < common.RoleRootUser {
		c.JSON(http.StatusForbidden, gin.H{
			"success": false,
			"message": "无权进行此操作，仅限超级管理员",
		})
		c.Abort()
		return true
	}

	c.Set("id", idInt)
	c.Set("username", usernameStr)
	c.Set("role", roleInt)
	c.Set("status", statusInt)
	c.Set("group", session.Get("group"))
	c.Set("user_group", session.Get("group"))
	c.Set("use_access_token", false)
	c.Next()
	return true
}
