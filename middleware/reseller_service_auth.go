package middleware

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"net/http"
	"os"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	ResellerErrorInvalidRequest      = "reseller_invalid_request"
	ResellerErrorInvalidRequestID    = "reseller_invalid_request_id"
	ResellerErrorConflict            = "reseller_conflict"
	ResellerErrorPricingVersion      = "reseller_pricing_version_conflict"
	ResellerErrorPricingMargin       = "reseller_pricing_margin_conflict"
	ResellerErrorNotFound            = "reseller_not_found"
	ResellerErrorServiceUnauthorized = "reseller_service_unauthorized"
	ResellerErrorContextNotFound     = "reseller_context_not_found"
	ResellerErrorForbidden           = "reseller_forbidden"
	ResellerErrorInvitationExpired   = "reseller_invitation_expired"
	ResellerErrorInvitationRevoked   = "reseller_invitation_revoked"
	ResellerErrorInvitationConsumed  = "reseller_invitation_consumed"
	ResellerErrorInternal            = "reseller_internal_error"

	resellerContextKey        = "reseller_context"
	resellerSubjectHeaderName = "X-Reseller-Subject"
	resellerHostHeaderName    = "X-Reseller-Host"
)

func ResellerServiceAuth() gin.HandlerFunc {
	return resellerServiceAuthForEnv("MATRIX_RESELLER_SERVICE_TOKEN")
}

func ResellerAdminServiceAuth() gin.HandlerFunc {
	return resellerServiceAuthForEnv("MOZIA_MEGA_SERVICE_TOKEN")
}

func ResellerManagementServiceAuth() gin.HandlerFunc {
	return resellerTenantServiceAuthForEnv("MATRIX_RESELLER_MANAGEMENT_TOKEN")
}

func ResellerRegistrationServiceAuth() gin.HandlerFunc {
	return resellerServiceAuthForEnv("MATRIX_RESELLER_REGISTRATION_TOKEN")
}

func resellerServiceAuthForEnv(envKey string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !authenticateResellerService(c, envKey) {
			return
		}
		c.Next()
	}
}

func resellerTenantServiceAuthForEnv(envKey string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !authenticateResellerService(c, envKey) {
			return
		}
		if _, exists := c.GetQuery("reseller_id"); exists {
			AbortResellerRequest(c, http.StatusBadRequest, ResellerErrorInvalidRequest, "invalid request")
			return
		}
		subjects := c.Request.Header.Values(resellerSubjectHeaderName)
		hosts := c.Request.Header.Values(resellerHostHeaderName)
		if len(subjects) != 1 || len(hosts) != 1 || !model.ValidResellerSubject(subjects[0]) {
			AbortResellerRequest(c, http.StatusBadRequest, ResellerErrorInvalidRequest, "invalid request")
			return
		}
		host, err := model.NormalizeResellerHost(hosts[0])
		if err != nil {
			AbortResellerRequest(c, http.StatusBadRequest, ResellerErrorInvalidRequest, "invalid request")
			return
		}
		resellerContext, err := model.ResolveResellerContext(subjects[0], host)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			AbortResellerRequest(c, http.StatusNotFound, ResellerErrorContextNotFound, "reseller context not found")
			return
		}
		if err != nil {
			logger.LogError(c.Request.Context(), "ResolveResellerContext database error: "+err.Error())
			AbortResellerRequest(c, http.StatusInternalServerError, ResellerErrorInternal, "internal error")
			return
		}
		c.Set(resellerContextKey, resellerContext)
		c.Next()
	}
}

func authenticateResellerService(c *gin.Context, envKey string) bool {
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
		return false
	}

	requestIDs := c.Request.Header.Values(common.RequestIdKey)
	if len(requestIDs) > 1 || len(requestIDs) == 1 && !validResellerRequestID(requestIDs[0]) {
		AbortResellerRequest(c, http.StatusBadRequest, ResellerErrorInvalidRequestID, "invalid request id")
		return false
	}
	if len(requestIDs) == 1 {
		setResellerRequestID(c, requestIDs[0])
	} else {
		setResellerRequestID(c, common.NewRequestId())
	}
	return true
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

func GetResellerContext(c *gin.Context) (*model.ResellerContext, bool) {
	value, ok := c.Get(resellerContextKey)
	if !ok {
		return nil, false
	}
	resellerContext, ok := value.(*model.ResellerContext)
	return resellerContext, ok && resellerContext != nil
}
