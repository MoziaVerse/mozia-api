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
	ResellerId int `json:"reseller_id"`
}

type resellerAssignmentConflictResolveRequest struct {
	Action string `json:"action"`
}

func GetHduResellerIdentityRoute(c *gin.Context) {
	record, err := model.GetHduResellerIdentityRoute()
	if err == nil {
		writeResellerAdminSuccess(c, http.StatusOK, record)
		return
	}
	logger.LogError(c.Request.Context(), "GetHduResellerIdentityRoute database error: "+err.Error())
	middleware.AbortResellerRequest(c, http.StatusInternalServerError, middleware.ResellerErrorInternal, "internal error")
}

func UpsertHduResellerIdentityRoute(c *gin.Context) {
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
	record, err := model.UpsertHduResellerIdentityRoute(request.ResellerId)
	switch {
	case err == nil:
		writeResellerAdminSuccess(c, http.StatusOK, record)
	case errors.Is(err, model.ErrInvalidResellerIdentityRoute):
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid identity route")
	case errors.Is(err, model.ErrResellerNotFound):
		middleware.AbortResellerRequest(c, http.StatusNotFound, middleware.ResellerErrorNotFound, "reseller not found")
	default:
		logger.LogError(c.Request.Context(), "UpsertHduResellerIdentityRoute database error: "+err.Error())
		middleware.AbortResellerRequest(c, http.StatusInternalServerError, middleware.ResellerErrorInternal, "internal error")
	}
}

func ListResellerAssignmentConflicts(c *gin.Context) {
	records, err := model.ListPendingResellerAssignmentConflicts()
	if err == nil {
		writeResellerAdminSuccess(c, http.StatusOK, records)
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
