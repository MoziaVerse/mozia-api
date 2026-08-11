package controller

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

const resellerAdminBodyLimit = 4 << 10

type createResellerAdminRequest struct {
	Name         string `json:"name"`
	Host         string `json:"host"`
	OwnerSubject string `json:"owner_subject"`
}

type updateResellerAdminStatusRequest struct {
	Status string `json:"status"`
}

func ListResellerAdminRecords(c *gin.Context) {
	records, err := model.ListResellerAdminRecords()
	if err != nil {
		logger.LogError(c.Request.Context(), "ListResellerAdminRecords database error: "+err.Error())
		middleware.AbortResellerRequest(c, http.StatusInternalServerError, middleware.ResellerErrorInternal, "internal error")
		return
	}
	writeResellerAdminSuccess(c, http.StatusOK, records)
}

func CreateResellerAdmin(c *gin.Context) {
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, resellerAdminBodyLimit))
	if err != nil {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("CreateResellerAdmin invalid request: read body failed error=%q body=%q", err.Error(), string(body)))
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid request")
		return
	}

	var request createResellerAdminRequest
	if err := common.Unmarshal(body, &request); err != nil {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("CreateResellerAdmin invalid request: decode failed error=%q body=%q", err.Error(), string(body)))
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid request")
		return
	}

	record, err := model.CreateResellerAdminRecord(request.Name, request.Host, request.OwnerSubject)
	if err != nil {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("CreateResellerAdmin failed error=%q body=%q", err.Error(), common.GetJsonString(request)))
	}
	switch {
	case err == nil:
		writeResellerAdminSuccess(c, http.StatusCreated, record)
	case errors.Is(err, model.ErrInvalidResellerName),
		errors.Is(err, model.ErrInvalidResellerHost),
		errors.Is(err, model.ErrInvalidResellerOwnerSubject):
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid request")
	case errors.Is(err, model.ErrResellerConflict):
		middleware.AbortResellerRequest(c, http.StatusConflict, middleware.ResellerErrorConflict, "duplicate reseller")
	default:
		logger.LogError(c.Request.Context(), "CreateResellerAdminRecord database error: "+err.Error())
		middleware.AbortResellerRequest(c, http.StatusInternalServerError, middleware.ResellerErrorInternal, "internal error")
	}
}

func UpdateResellerAdminStatus(c *gin.Context) {
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

	var request updateResellerAdminStatusRequest
	if common.Unmarshal(body, &request) != nil {
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid request")
		return
	}

	record, err := model.UpdateResellerAdminStatus(id, request.Status)
	switch {
	case err == nil:
		writeResellerAdminSuccess(c, http.StatusOK, record)
	case errors.Is(err, model.ErrInvalidResellerStatus):
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid request")
	case errors.Is(err, model.ErrResellerNotFound):
		middleware.AbortResellerRequest(c, http.StatusNotFound, middleware.ResellerErrorNotFound, "reseller not found")
	default:
		logger.LogError(c.Request.Context(), "UpdateResellerAdminStatus database error: "+err.Error())
		middleware.AbortResellerRequest(c, http.StatusInternalServerError, middleware.ResellerErrorInternal, "internal error")
	}
}

func writeResellerAdminSuccess(c *gin.Context, status int, data any) {
	requestID := c.GetString(common.RequestIdKey)
	if requestID == "" {
		requestID = common.NewRequestId()
		c.Header(common.RequestIdKey, requestID)
	}
	c.JSON(status, gin.H{
		"success":    true,
		"data":       data,
		"request_id": requestID,
	})
}
