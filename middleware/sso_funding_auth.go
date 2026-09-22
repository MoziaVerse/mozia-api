package middleware

import (
	"crypto/subtle"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
)

// SSOFundingKeyHeader 是资金令牌管理接口的独立凭据头。
const SSOFundingKeyHeader = "X-SSO-Funding-Key"

// SSOFundingAuth 保护「发钱」类接口（签发 / 调整 / 撤销自带资金的令牌）。
// SSO 桥的 admin token + X-SSO-SUB 只证明调用链身份，matrix 普通用户管理 key 也走同一条链，
// 所以这里要求额外的独立密钥 SSO_FUNDING_KEY，只发给运营发放服务，用户侧代码路径拿不到。
// 未配置时接口整体关闭。
func SSOFundingAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		expected := strings.TrimSpace(os.Getenv("SSO_FUNDING_KEY"))
		if expected == "" {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
				"success": false,
				"message": "资金令牌管理接口未启用（SSO_FUNDING_KEY 未配置）",
			})
			return
		}
		got := strings.TrimSpace(c.GetHeader(SSOFundingKeyHeader))
		if got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(expected)) != 1 {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"success": false,
				"message": "资金令牌管理凭据无效",
			})
			return
		}
		c.Next()
	}
}
