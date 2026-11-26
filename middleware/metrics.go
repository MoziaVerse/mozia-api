package middleware

import (
	"github.com/QuantumNous/new-api/pkg/metrics"
	"github.com/gin-gonic/gin"
)

// ObservePaymentWebhook records payment callback success/rejection/error rates
// by provider without interfering with provider-specific verification handlers.
func ObservePaymentWebhook(provider string) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				metrics.RecordPaymentWebhook(provider, "error")
				panic(r)
			}
			code := c.Writer.Status()
			switch {
			case code >= 200 && code < 300:
				metrics.RecordPaymentWebhook(provider, "success")
			case code >= 400 && code < 500:
				metrics.RecordPaymentWebhook(provider, "rejected")
			default:
				metrics.RecordPaymentWebhook(provider, "error")
			}
		}()
		c.Next()
	}
}
