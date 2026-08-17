package controller

import (
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

type resellerIdentityRouteRequest struct {
	ResellerId int    `json:"reseller_id"`
	Status     string `json:"status"`
}

type resellerAssignmentConflictResolveRequest struct {
	Action string `json:"action"`
}

func ClaimResellerVerifiedIdentity(c *gin.Context) {
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, resellerAdminBodyLimit))
	if err != nil {
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid request")
		return
	}
	var request model.ResellerVerifiedIdentityClaimInput
	if common.Unmarshal(body, &request) != nil {
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid request")
		return
	}
	record, err := model.ClaimResellerVerifiedIdentity(request)
	if err == nil {
		writeResellerAdminSuccess(c, http.StatusOK, record)
		return
	}
	if errors.Is(err, model.ErrInvalidResellerCustomerIdentity) {
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid request")
		return
	}
	logger.LogError(c.Request.Context(), "ClaimResellerVerifiedIdentity database error: "+err.Error())
	middleware.AbortResellerRequest(c, http.StatusInternalServerError, middleware.ResellerErrorInternal, "internal error")
}

func GetResellerIdentityRoute(c *gin.Context) {
	record, err := model.GetResellerIdentityRoute(c.Param("provider"))
	if err == nil {
		writeResellerAdminSuccess(c, http.StatusOK, record)
		return
	}
	if errors.Is(err, model.ErrInvalidResellerIdentityProvider) {
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid identity provider")
		return
	}
	logger.LogError(c.Request.Context(), "GetResellerIdentityRoute database error: "+err.Error())
	middleware.AbortResellerRequest(c, http.StatusInternalServerError, middleware.ResellerErrorInternal, "internal error")
}

func UpsertResellerIdentityRoute(c *gin.Context) {
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, resellerAdminBodyLimit))
	if err != nil {
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid request")
		return
	}
	var request resellerIdentityRouteRequest
	if common.Unmarshal(body, &request) != nil {
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid request")
		return
	}
	record, err := model.UpsertResellerIdentityRoute(c.Param("provider"), request.ResellerId, request.Status)
	switch {
	case err == nil:
		writeResellerAdminSuccess(c, http.StatusOK, record)
	case errors.Is(err, model.ErrInvalidResellerIdentityRoute):
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid identity route")
	case errors.Is(err, model.ErrResellerNotFound):
		middleware.AbortResellerRequest(c, http.StatusNotFound, middleware.ResellerErrorNotFound, "reseller not found")
	default:
		logger.LogError(c.Request.Context(), "UpsertResellerIdentityRoute database error: "+err.Error())
		middleware.AbortResellerRequest(c, http.StatusInternalServerError, middleware.ResellerErrorInternal, "internal error")
	}
}

func ListResellerAssignmentConflicts(c *gin.Context) {
	records, err := model.ListResellerAssignmentConflicts(c.Query("status"))
	if err == nil {
		writeResellerAdminSuccess(c, http.StatusOK, records)
		return
	}
	if errors.Is(err, model.ErrInvalidResellerIdentityRoute) {
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid conflict status")
		return
	}
	logger.LogError(c.Request.Context(), "ListResellerAssignmentConflicts database error: "+err.Error())
	middleware.AbortResellerRequest(c, http.StatusInternalServerError, middleware.ResellerErrorInternal, "internal error")
}

func ResolveResellerAssignmentConflict(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id < 1 {
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid request")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, resellerAdminBodyLimit))
	if err != nil {
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid request")
		return
	}
	var request resellerAssignmentConflictResolveRequest
	actorSubject := c.GetHeader("X-Platform-Actor-Subject")
	if common.Unmarshal(body, &request) != nil || actorSubject == "" {
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid request")
		return
	}
	record, err := model.ResolveResellerAssignmentConflict(id, request.Action, actorSubject)
	switch {
	case err == nil:
		writeResellerAdminSuccess(c, http.StatusOK, record)
	case errors.Is(err, model.ErrInvalidResellerIdentityRoute):
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid request")
	case errors.Is(err, model.ErrResellerAssignmentConflictNotFound):
		middleware.AbortResellerRequest(c, http.StatusNotFound, middleware.ResellerErrorNotFound, "assignment conflict not found")
	case errors.Is(err, model.ErrResellerNotFound), errors.Is(err, model.ErrResellerCustomerConflict):
		middleware.AbortResellerRequest(c, http.StatusConflict, middleware.ResellerErrorConflict, "assignment conflict changed")
	default:
		logger.LogError(c.Request.Context(), "ResolveResellerAssignmentConflict database error: "+err.Error())
		middleware.AbortResellerRequest(c, http.StatusInternalServerError, middleware.ResellerErrorInternal, "internal error")
	}
}
