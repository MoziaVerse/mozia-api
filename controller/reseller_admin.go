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

type updateResellerAdminRequest struct {
	Name       string  `json:"name"`
	Host       string  `json:"host"`
	MatrixHost *string `json:"matrix_host"`
}

type updateResellerAdminBankTransferRequest struct {
	PaymentConfigEnabled *bool  `json:"payment_config_enabled"`
	AccountName          string `json:"account_name"`
	AccountNumber        string `json:"account_number"`
	BankName             string `json:"bank_name"`
}

type updateResellerAdminPresentationRequest struct {
	BrandName                  string  `json:"brand_name"`
	Logo                       *string `json:"logo"`
	Favicon                    *string `json:"favicon"`
	IcpFilingNumber            string  `json:"icp_filing_number"`
	PublicSecurityFilingNumber string  `json:"public_security_filing_number"`
	ValueAddedTelecomLicense   string  `json:"value_added_telecom_license"`
	CopyrightText              string  `json:"copyright_text"`
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

func UpdateResellerAdmin(c *gin.Context) {
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

	var request updateResellerAdminRequest
	if common.Unmarshal(body, &request) != nil {
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid request")
		return
	}

	record, err := model.UpdateResellerAdminRecord(id, request.Name, request.Host, request.MatrixHost)
	if err != nil {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("UpdateResellerAdmin failed reseller_id=%d error=%q body=%q", id, err.Error(), common.GetJsonString(request)))
	}
	switch {
	case err == nil:
		writeResellerAdminSuccess(c, http.StatusOK, record)
	case errors.Is(err, model.ErrInvalidResellerName), errors.Is(err, model.ErrInvalidResellerHost):
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid request")
	case errors.Is(err, model.ErrResellerNotFound):
		middleware.AbortResellerRequest(c, http.StatusNotFound, middleware.ResellerErrorNotFound, "reseller not found")
	case errors.Is(err, model.ErrResellerConflict):
		middleware.AbortResellerRequest(c, http.StatusConflict, middleware.ResellerErrorConflict, "duplicate reseller")
	default:
		logger.LogError(c.Request.Context(), "UpdateResellerAdminRecord database error: "+err.Error())
		middleware.AbortResellerRequest(c, http.StatusInternalServerError, middleware.ResellerErrorInternal, "internal error")
	}
}

func UpdateResellerAdminPresentation(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id < 1 {
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid request")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, resellerLogoBodyLimit*2))
	if err != nil {
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid request")
		return
	}
	var request updateResellerAdminPresentationRequest
	if common.Unmarshal(body, &request) != nil || request.Logo == nil || request.Favicon == nil {
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid request")
		return
	}
	branding, err := model.UpdateResellerPresentation(id, request.BrandName, *request.Logo, *request.Favicon, request.IcpFilingNumber, request.PublicSecurityFilingNumber, request.ValueAddedTelecomLicense, request.CopyrightText)
	switch {
	case err == nil:
		writeResellerAdminSuccess(c, http.StatusOK, branding)
	case errors.Is(err, model.ErrInvalidResellerPresentation), errors.Is(err, model.ErrInvalidResellerLogo), errors.Is(err, model.ErrInvalidResellerFavicon):
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid reseller presentation")
	case errors.Is(err, model.ErrResellerNotFound):
		middleware.AbortResellerRequest(c, http.StatusNotFound, middleware.ResellerErrorNotFound, "reseller not found")
	default:
		logger.LogError(c.Request.Context(), "UpdateResellerPresentation database error: "+err.Error())
		middleware.AbortResellerRequest(c, http.StatusInternalServerError, middleware.ResellerErrorInternal, "internal error")
	}
}

func UpdateResellerAdminBankTransfer(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id < 1 {
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid request")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, resellerAdminBodyLimit))
	var request updateResellerAdminBankTransferRequest
	if err != nil || common.Unmarshal(body, &request) != nil || request.PaymentConfigEnabled == nil {
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid request")
		return
	}
	config, err := model.UpdateResellerBankTransferConfig(id, request.PaymentConfigEnabled, request.AccountName, request.AccountNumber, request.BankName, false)
	switch {
	case err == nil:
		writeResellerAdminSuccess(c, http.StatusOK, config)
	case errors.Is(err, model.ErrInvalidResellerBankTransfer):
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid bank transfer config")
	case errors.Is(err, model.ErrResellerNotFound):
		middleware.AbortResellerRequest(c, http.StatusNotFound, middleware.ResellerErrorNotFound, "reseller not found")
	default:
		logger.LogError(c.Request.Context(), "UpdateResellerAdminBankTransfer database error: "+err.Error())
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
