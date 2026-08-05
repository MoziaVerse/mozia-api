package controller

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const resellerContextBodyLimit = 4 << 10

var resellerRolePermissions = map[string][]string{
	model.ResellerRoleOwner:  {"reseller:read", "reseller:write"},
	model.ResellerRoleAdmin:  {"reseller:read", "reseller:write"},
	model.ResellerRoleViewer: {"reseller:read"},
}

type resellerContextRequest struct {
	Subject    string          `json:"subject"`
	Host       string          `json:"host"`
	ResellerId json.RawMessage `json:"reseller_id"`
}

func GetResellerContext(c *gin.Context) {
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, resellerContextBodyLimit))
	var request resellerContextRequest
	if err != nil || common.Unmarshal(body, &request) != nil || len(request.ResellerId) != 0 || !model.ValidResellerSubject(request.Subject) {
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid request")
		return
	}

	host, err := model.NormalizeResellerHost(request.Host)
	if err != nil {
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid request")
		return
	}

	resellerContext, err := model.ResolveResellerContext(request.Subject, host)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		middleware.AbortResellerRequest(c, http.StatusNotFound, middleware.ResellerErrorContextNotFound, "reseller context not found")
		return
	}
	if err != nil {
		logger.LogError(c.Request.Context(), "ResolveResellerContext database error: "+err.Error())
		middleware.AbortResellerRequest(c, http.StatusInternalServerError, middleware.ResellerErrorInternal, "internal error")
		return
	}

	permissions, knownRole := resellerRolePermissions[resellerContext.Role]
	if !knownRole {
		middleware.AbortResellerRequest(c, http.StatusNotFound, middleware.ResellerErrorContextNotFound, "reseller context not found")
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"reseller_id":   resellerContext.ResellerId,
			"reseller_name": resellerContext.ResellerName,
			"host":          resellerContext.Host,
			"role":          resellerContext.Role,
			"permissions":   permissions,
		},
		"request_id": c.GetString(common.RequestIdKey),
	})
}
