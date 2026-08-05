package middleware

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"os"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

const (
	ResellerErrorInvalidRequest      = "reseller_invalid_request"
	ResellerErrorInvalidRequestID    = "reseller_invalid_request_id"
	ResellerErrorConflict            = "reseller_conflict"
	ResellerErrorNotFound            = "reseller_not_found"
	ResellerErrorServiceUnauthorized = "reseller_service_unauthorized"
	ResellerErrorContextNotFound     = "reseller_context_not_found"
	ResellerErrorInternal            = "reseller_internal_error"
)

func ResellerServiceAuth() gin.HandlerFunc {
	return resellerServiceAuthForEnv("MATRIX_RESELLER_SERVICE_TOKEN")
}

func ResellerAdminServiceAuth() gin.HandlerFunc {
	return resellerServiceAuthForEnv("MOZIA_MEGA_SERVICE_TOKEN")
}

func resellerServiceAuthForEnv(envKey string) gin.HandlerFunc {
	return func(c *gin.Context) {
		expectedToken := os.Getenv(envKey)
		authorizations := c.Request.Header.Values("Authorization")
		authorization := ""
		if len(authorizations) == 1 {
			authorization = authorizations[0]
		}
		if expectedToken == "" || len(authorizations) != 1 || len(authorization) <= len("Bearer ") ||
			!strings.EqualFold(authorization[:len("Bearer ")], "Bearer ") ||
			!resellerServiceTokensEqual(authorization[len("Bearer "):], expectedToken) {
			AbortResellerRequest(c, http.StatusUnauthorized, ResellerErrorServiceUnauthorized, "service authentication failed")
			return
		}

		requestIDs := c.Request.Header.Values(common.RequestIdKey)
		if len(requestIDs) > 1 || len(requestIDs) == 1 && !validResellerRequestID(requestIDs[0]) {
			AbortResellerRequest(c, http.StatusBadRequest, ResellerErrorInvalidRequestID, "invalid request id")
			return
		}
		if len(requestIDs) == 1 {
			setResellerRequestID(c, requestIDs[0])
		} else {
			setResellerRequestID(c, common.NewRequestId())
		}
		c.Next()
	}
}

func AbortResellerRequest(c *gin.Context, status int, code string, message string) {
	requestID := c.GetString(common.RequestIdKey)
	if requestID == "" {
		requestID = common.NewRequestId()
		setResellerRequestID(c, requestID)
	}
	c.JSON(status, gin.H{
		"success": false,
		"error": gin.H{
			"code":    code,
			"message": message,
		},
		"request_id": requestID,
	})
	c.Abort()
}

func resellerServiceTokensEqual(provided string, expected string) bool {
	providedHash := sha256.Sum256([]byte(provided))
	expectedHash := sha256.Sum256([]byte(expected))
	return subtle.ConstantTimeCompare(providedHash[:], expectedHash[:]) == 1
}

func validResellerRequestID(requestID string) bool {
	if len(requestID) < 8 || len(requestID) > 64 {
		return false
	}
	for i := 0; i < len(requestID); i++ {
		character := requestID[i]
		if (character < 'a' || character > 'z') &&
			(character < 'A' || character > 'Z') &&
			(character < '0' || character > '9') &&
			character != '-' && character != '_' {
			return false
		}
	}
	return true
}

func setResellerRequestID(c *gin.Context, requestID string) {
	c.Set(common.RequestIdKey, requestID)
	c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), common.RequestIdKey, requestID))
	c.Header(common.RequestIdKey, requestID)
}
