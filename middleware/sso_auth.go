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

func SSOAuth() func(c *gin.Context) {
	return func(c *gin.Context) {
		session := sessions.Default(c)
		var role interface{} = session.Get("role")
		var id interface{} = session.Get("id")

		if role == nil {
			accessToken := c.Request.Header.Get("Authorization")
			if accessToken == "" {
				c.JSON(http.StatusUnauthorized, gin.H{
					"success": false,
					"message": "无权进行此操作，未登录且未提供 access token",
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
			role = user.Role
			id = user.Id
		}

		if role.(int) < common.RoleRootUser {
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"message": "无权进行此操作，仅限超级管理员",
			})
			c.Abort()
			return
		}

		apiUserIdStr := c.Request.Header.Get("New-Api-User")
		if apiUserIdStr == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"message": "无权进行此操作，未提供 New-Api-User",
			})
			c.Abort()
			return
		}
		apiUserId, err := strconv.Atoi(apiUserIdStr)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"message": "无权进行此操作，New-Api-User 格式错误",
			})
			c.Abort()
			return
		}
		if id != apiUserId {
			c.JSON(http.StatusUnauthorized, gin.H{
				"success": false,
				"message": "无权进行此操作，New-Api-User 与登录用户不匹配",
			})
			c.Abort()
			return
		}

		ssoSub := c.Request.Header.Get("X-SSO-SUB")
		if ssoSub == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"success": false,
				"message": "缺少 X-SSO-SUB 请求头",
			})
			c.Abort()
			return
		}

		userSSO, err := model.GetOrCreateSSOUser(ssoSub)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"message": "SSO 用户创建失败: " + err.Error(),
			})
			c.Abort()
			return
		}

		ssoUser, err := model.GetUserById(userSSO.UserId, true)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"success": false,
				"message": "SSO 用户查询失败",
			})
			c.Abort()
			return
		}

		c.Set("id", ssoUser.Id)
		c.Set("username", ssoUser.Username)
		c.Set("role", ssoUser.Role)
		c.Set("status", ssoUser.Status)
		c.Set("group", ssoUser.Group)
		c.Set("user_group", ssoUser.Group)
		c.Set("sso_sub", userSSO.SSOSub)
		c.Set("use_access_token", strings.HasPrefix(c.Request.Header.Get("Authorization"), "Bearer "))
		c.Next()
	}
}
