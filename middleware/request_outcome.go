package middleware

import (
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

// RequestOutcome wraps authentication and channel selection so known-user
// rejections are counted alongside requests that reached a provider.
func RequestOutcome() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method != http.MethodPost || !model.IsCallAnalyticsPath(c.Request.URL.Path) {
			c.Next()
			return
		}
		started := time.Now()
		c.Set("call_analytics_enabled", true)
		defer func() {
			panicValue := recover()
			model.RecordRequestOutcome(c, started, panicValue != nil)
			if panicValue != nil {
				panic(panicValue) // Preserve the existing outer Gin recovery handler.
			}
		}()
		c.Next()
	}
}
