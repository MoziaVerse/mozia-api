package controller

import (
	"encoding/json"
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

const resellerManagementBodyLimit = 4 << 10
const resellerLogoBodyLimit = 384 << 10
const resellerPlatformBatchAssignBodyLimit = 64 << 10
const resellerProfileBackfillDefaultLimit = 100
const resellerProfileBackfillMaxLimit = 200

type resellerCustomerStatusRequest struct {
	Status     string          `json:"status"`
	ResellerId json.RawMessage `json:"reseller_id"`
}

type resellerSubagentCreateRequest struct {
	Name       string          `json:"name"`
	CustomerId int             `json:"customer_id"`
	ResellerId json.RawMessage `json:"reseller_id"`
}

type resellerSubagentCapabilitiesRequest struct {
	CanManagePricing         *bool           `json:"can_manage_pricing"`
	CanCreateInvitations     *bool           `json:"can_create_invitations"`
	CanManageCustomerAccess  *bool           `json:"can_manage_customer_access"`
	CanManageCustomerPayment *bool           `json:"can_manage_customer_payment"`
	ResellerId               json.RawMessage `json:"reseller_id"`
}

type resellerCustomerSubagentRequest struct {
	SubagentMemberId *int            `json:"subagent_member_id"`
	ResellerId       json.RawMessage `json:"reseller_id"`
}

type resellerCustomerRemarkRequest struct {
	Remark     *string         `json:"remark"`
	ResellerId json.RawMessage `json:"reseller_id"`
}

type resellerLogoRequest struct {
	Logo *string `json:"logo"`
}

type resellerFaviconRequest struct {
	Favicon *string `json:"favicon"`
}

func resellerBooleanField(body []byte, field string) (bool, bool) {
	var fields map[string]json.RawMessage
	if common.Unmarshal(body, &fields) != nil || len(fields) != 1 {
		return false, false
	}
	var value *bool
	if common.Unmarshal(fields[field], &value) != nil || value == nil {
		return false, false
	}
	return *value, true
}

type resellerBankTransferRequest struct {
	AccountName   string          `json:"account_name"`
	AccountNumber string          `json:"account_number"`
	BankName      string          `json:"bank_name"`
	ResellerId    json.RawMessage `json:"reseller_id"`
}

type resellerCustomerPaymentMethodRequest struct {
	Subject    string          `json:"subject"`
	ResellerId json.RawMessage `json:"reseller_id"`
}

type resellerInvitationCreateRequest struct {
	ExpiresInHours *int            `json:"expires_in_hours"`
	ResellerId     json.RawMessage `json:"reseller_id"`
}

type resellerInvitationConsumeRequest struct {
	Token      string          `json:"token"`
	Subject    string          `json:"subject"`
	MatrixName string          `json:"matrix_name"`
	Phone      string          `json:"phone"`
	ResellerId json.RawMessage `json:"reseller_id"`
}

type resellerCustomerIdentitySyncRequest struct {
	Subject     string          `json:"subject"`
	MatrixName  string          `json:"matrix_name"`
	Phone       string          `json:"phone"`
	HduVerified bool            `json:"hdu_verified"`
	ResellerId  json.RawMessage `json:"reseller_id"`
}

type resellerCustomerBatchAssignRequest struct {
	Customers  []model.ResellerCustomerBatchAssignInput `json:"customers"`
	ResellerId json.RawMessage                          `json:"reseller_id"`
}

func GetResellerManagementProfile(c *gin.Context) {
	resellerContext, ok := resellerManagementContext(c)
	if !ok {
		return
	}
	permissions, knownRole := resellerPermissions(resellerContext)
	if !knownRole {
		middleware.AbortResellerRequest(c, http.StatusNotFound, middleware.ResellerErrorContextNotFound, "reseller context not found")
		return
	}
	branding, err := model.GetResellerBranding(resellerContext.ResellerId)
	if err != nil {
		logger.LogError(c.Request.Context(), "GetResellerBranding database error: "+err.Error())
		middleware.AbortResellerRequest(c, http.StatusInternalServerError, middleware.ResellerErrorInternal, "internal error")
		return
	}
	bankTransfer, err := model.GetResellerBankTransferConfig(resellerContext.ResellerId)
	if err != nil {
		logger.LogError(c.Request.Context(), "GetResellerBankTransferConfig database error: "+err.Error())
		middleware.AbortResellerRequest(c, http.StatusInternalServerError, middleware.ResellerErrorInternal, "internal error")
		return
	}
	writeResellerAdminSuccess(c, http.StatusOK, gin.H{
		"reseller_id":                   resellerContext.ResellerId,
		"reseller_name":                 resellerContext.ResellerName,
		"host":                          resellerContext.Host,
		"subject":                       resellerContext.Subject,
		"role":                          resellerContext.Role,
		"permissions":                   permissions,
		"logo":                          branding.Logo,
		"favicon":                       branding.Favicon,
		"brand_name":                    branding.BrandName,
		"icp_filing_number":             branding.IcpFilingNumber,
		"public_security_filing_number": branding.PublicSecurityFilingNumber,
		"copyright_text":                branding.CopyrightText,
		"bank_transfer":                 bankTransfer,
	})
}

func UpdateResellerManagementBankTransfer(c *gin.Context) {
	resellerContext, ok := resellerManagementContext(c)
	if !ok {
		return
	}
	if resellerContext.Role != model.ResellerRoleOwner {
		middleware.AbortResellerRequest(c, http.StatusForbidden, middleware.ResellerErrorForbidden, "reseller owner access required")
		return
	}
	body, ok := resellerRequestBody(c, resellerManagementBodyLimit)
	if !ok {
		return
	}
	var request resellerBankTransferRequest
	if common.Unmarshal(body, &request) != nil || len(request.ResellerId) != 0 {
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid request")
		return
	}
	current, err := model.GetResellerBankTransferConfig(resellerContext.ResellerId)
	if err != nil {
		logger.LogError(c.Request.Context(), "GetResellerBankTransferConfig database error: "+err.Error())
		middleware.AbortResellerRequest(c, http.StatusInternalServerError, middleware.ResellerErrorInternal, "internal error")
		return
	}
	if !current.Allowed {
		middleware.AbortResellerRequest(c, http.StatusForbidden, middleware.ResellerErrorForbidden, "custom payment configuration is not enabled")
		return
	}
	config, err := model.UpdateResellerBankTransferConfig(resellerContext.ResellerId, nil, request.AccountName, request.AccountNumber, request.BankName, true)
	switch {
	case err == nil:
		writeResellerAdminSuccess(c, http.StatusOK, config)
	case errors.Is(err, model.ErrInvalidResellerBankTransfer):
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid bank transfer config")
	default:
		logger.LogError(c.Request.Context(), "UpdateResellerManagementBankTransfer database error: "+err.Error())
		middleware.AbortResellerRequest(c, http.StatusInternalServerError, middleware.ResellerErrorInternal, "internal error")
	}
}

func GetResellerRegistrationCustomerPaymentMethod(c *gin.Context) {
	body, ok := resellerRequestBody(c, resellerManagementBodyLimit)
	if !ok {
		return
	}
	var request resellerCustomerPaymentMethodRequest
	if common.Unmarshal(body, &request) != nil || len(request.ResellerId) != 0 || !model.ValidResellerSubject(request.Subject) {
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid request")
		return
	}
	paymentMethod, err := model.ResolveResellerCustomerPaymentMethod(request.Subject)
	if err != nil {
		logger.LogError(c.Request.Context(), "ResolveResellerCustomerPaymentMethod database error: "+err.Error())
		middleware.AbortResellerRequest(c, http.StatusInternalServerError, middleware.ResellerErrorInternal, "internal error")
		return
	}
	writeResellerAdminSuccess(c, http.StatusOK, paymentMethod)
}

func UpdateResellerManagementLogo(c *gin.Context) {
	resellerContext, ok := resellerManagementContext(c)
	if !ok {
		return
	}
	if resellerContext.Role != model.ResellerRoleOwner {
		middleware.AbortResellerRequest(c, http.StatusForbidden, middleware.ResellerErrorForbidden, "reseller owner access required")
		return
	}
	body, ok := resellerRequestBody(c, resellerLogoBodyLimit)
	if !ok {
		return
	}
	var request resellerLogoRequest
	if common.Unmarshal(body, &request) != nil || request.Logo == nil {
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid request")
		return
	}
	branding, err := model.UpdateResellerLogo(resellerContext.ResellerId, *request.Logo)
	switch {
	case err == nil:
		writeResellerAdminSuccess(c, http.StatusOK, branding)
	case errors.Is(err, model.ErrInvalidResellerLogo):
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid reseller logo")
	default:
		logger.LogError(c.Request.Context(), "UpdateResellerLogo database error: "+err.Error())
		middleware.AbortResellerRequest(c, http.StatusInternalServerError, middleware.ResellerErrorInternal, "internal error")
	}
}

func UpdateResellerManagementFavicon(c *gin.Context) {
	resellerContext, ok := resellerManagementContext(c)
	if !ok {
		return
	}
	if resellerContext.Role != model.ResellerRoleOwner {
		middleware.AbortResellerRequest(c, http.StatusForbidden, middleware.ResellerErrorForbidden, "reseller owner access required")
		return
	}
	body, ok := resellerRequestBody(c, resellerLogoBodyLimit)
	if !ok {
		return
	}
	var request resellerFaviconRequest
	if common.Unmarshal(body, &request) != nil || request.Favicon == nil {
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid request")
		return
	}
	branding, err := model.UpdateResellerFavicon(resellerContext.ResellerId, *request.Favicon)
	switch {
	case err == nil:
		writeResellerAdminSuccess(c, http.StatusOK, branding)
	case errors.Is(err, model.ErrInvalidResellerFavicon):
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid reseller favicon")
	default:
		logger.LogError(c.Request.Context(), "UpdateResellerFavicon database error: "+err.Error())
		middleware.AbortResellerRequest(c, http.StatusInternalServerError, middleware.ResellerErrorInternal, "internal error")
	}
}

func ListResellerManagementMembers(c *gin.Context) {
	resellerContext, ok := resellerManagementContext(c)
	if !ok {
		return
	}
	if resellerContext.Role == model.ResellerRoleSubagent {
		middleware.AbortResellerRequest(c, http.StatusForbidden, middleware.ResellerErrorForbidden, "reseller members forbidden")
		return
	}
	records, err := model.ListResellerMemberRecords(resellerContext.ResellerId)
	if err != nil {
		logger.LogError(c.Request.Context(), "ListResellerMemberRecords database error: "+err.Error())
		middleware.AbortResellerRequest(c, http.StatusInternalServerError, middleware.ResellerErrorInternal, "internal error")
		return
	}
	writeResellerAdminSuccess(c, http.StatusOK, records)
}

func CreateResellerManagementSubagent(c *gin.Context) {
	resellerContext, ok := resellerManagementContext(c)
	if !ok {
		return
	}
	if resellerContext.Role != model.ResellerRoleOwner {
		middleware.AbortResellerRequest(c, http.StatusForbidden, middleware.ResellerErrorForbidden, "reseller owner access required")
		return
	}
	body, ok := resellerRequestBody(c, resellerManagementBodyLimit)
	if !ok {
		return
	}
	var request resellerSubagentCreateRequest
	if common.Unmarshal(body, &request) != nil || len(request.ResellerId) != 0 || request.CustomerId <= 0 {
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid request")
		return
	}
	record, err := model.CreateResellerSubagentMember(resellerContext.ResellerId, request.CustomerId, request.Name)
	switch {
	case err == nil:
		writeResellerAdminSuccess(c, http.StatusCreated, record)
	case errors.Is(err, model.ErrInvalidResellerName):
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid request")
	case errors.Is(err, model.ErrResellerCustomerNotFound):
		middleware.AbortResellerRequest(c, http.StatusNotFound, middleware.ResellerErrorNotFound, "reseller customer not found")
	case errors.Is(err, model.ErrResellerConflict):
		middleware.AbortResellerRequest(c, http.StatusConflict, middleware.ResellerErrorConflict, "reseller member already exists")
	case errors.Is(err, model.ErrResellerCustomerConflict):
		middleware.AbortResellerRequest(c, http.StatusConflict, middleware.ResellerErrorConflict, "customer is already assigned to a subagent")
	default:
		logger.LogError(c.Request.Context(), "CreateResellerSubagentMember database error: "+err.Error())
		middleware.AbortResellerRequest(c, http.StatusInternalServerError, middleware.ResellerErrorInternal, "internal error")
	}
}

func UpdateResellerManagementSubagentCapabilities(c *gin.Context) {
	resellerContext, ok := resellerManagementContext(c)
	if !ok {
		return
	}
	if resellerContext.Role != model.ResellerRoleOwner {
		middleware.AbortResellerRequest(c, http.StatusForbidden, middleware.ResellerErrorForbidden, "reseller owner access required")
		return
	}
	memberId, valid := positivePathID(c)
	if !valid {
		return
	}
	body, ok := resellerRequestBody(c, resellerManagementBodyLimit)
	if !ok {
		return
	}
	var request resellerSubagentCapabilitiesRequest
	if common.Unmarshal(body, &request) != nil || len(request.ResellerId) != 0 || request.CanManagePricing == nil || request.CanCreateInvitations == nil || request.CanManageCustomerAccess == nil || request.CanManageCustomerPayment == nil {
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid request")
		return
	}
	if *request.CanManageCustomerPayment && !resellerContext.PaymentConfigEnabled {
		middleware.AbortResellerRequest(c, http.StatusForbidden, middleware.ResellerErrorForbidden, "custom payment configuration is not enabled")
		return
	}
	record, err := model.UpdateResellerSubagentCapabilities(
		resellerContext.ResellerId,
		memberId,
		*request.CanManagePricing,
		*request.CanCreateInvitations,
		*request.CanManageCustomerAccess,
		*request.CanManageCustomerPayment,
	)
	switch {
	case err == nil:
		writeResellerAdminSuccess(c, http.StatusOK, record)
	case errors.Is(err, model.ErrResellerForbidden):
		middleware.AbortResellerRequest(c, http.StatusNotFound, middleware.ResellerErrorNotFound, "reseller subagent not found")
	default:
		logger.LogError(c.Request.Context(), "UpdateResellerSubagentCapabilities database error: "+err.Error())
		middleware.AbortResellerRequest(c, http.StatusInternalServerError, middleware.ResellerErrorInternal, "internal error")
	}
}

func ListResellerManagementCustomers(c *gin.Context) {
	resellerContext, ok := resellerManagementContext(c)
	if !ok {
		return
	}
	var records []model.ResellerCustomerRecord
	var err error
	if resellerContext.Role == model.ResellerRoleSubagent {
		records, err = model.ListResellerSubagentCustomerRecords(resellerContext.ResellerId, resellerContext.MemberId)
	} else {
		records, err = model.ListResellerCustomerRecords(resellerContext.ResellerId, true)
	}
	if err != nil {
		logger.LogError(c.Request.Context(), "ListResellerCustomerRecords database error: "+err.Error())
		middleware.AbortResellerRequest(c, http.StatusInternalServerError, middleware.ResellerErrorInternal, "internal error")
		return
	}
	writeResellerAdminSuccess(c, http.StatusOK, records)
}

func GetResellerManagementCustomer(c *gin.Context) {
	resellerContext, ok := resellerManagementContext(c)
	if !ok {
		return
	}
	customerId, valid := positivePathID(c)
	if !valid {
		return
	}
	var record *model.ResellerCustomerRecord
	var err error
	if resellerContext.Role == model.ResellerRoleSubagent {
		record, err = model.GetResellerSubagentCustomerRecord(resellerContext.ResellerId, resellerContext.MemberId, customerId)
	} else {
		record, err = model.GetResellerCustomerRecord(resellerContext.ResellerId, customerId, true)
	}
	if errors.Is(err, model.ErrResellerCustomerNotFound) {
		middleware.AbortResellerRequest(c, http.StatusNotFound, middleware.ResellerErrorNotFound, "reseller customer not found")
		return
	}
	if err != nil {
		logger.LogError(c.Request.Context(), "GetResellerCustomerRecord database error: "+err.Error())
		middleware.AbortResellerRequest(c, http.StatusInternalServerError, middleware.ResellerErrorInternal, "internal error")
		return
	}
	writeResellerAdminSuccess(c, http.StatusOK, record)
}

func UpdateResellerManagementCustomerSubagent(c *gin.Context) {
	resellerContext, ok := resellerManagementContext(c)
	if !ok {
		return
	}
	if resellerContext.Role != model.ResellerRoleOwner {
		middleware.AbortResellerRequest(c, http.StatusForbidden, middleware.ResellerErrorForbidden, "reseller owner access required")
		return
	}
	customerId, valid := positivePathID(c)
	if !valid {
		return
	}
	body, ok := resellerRequestBody(c, resellerManagementBodyLimit)
	if !ok {
		return
	}
	var request resellerCustomerSubagentRequest
	if common.Unmarshal(body, &request) != nil || len(request.ResellerId) != 0 || request.SubagentMemberId == nil || *request.SubagentMemberId < 0 {
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid request")
		return
	}
	var memberId *int
	if *request.SubagentMemberId > 0 {
		memberId = request.SubagentMemberId
	}
	record, err := model.AssignResellerCustomerSubagent(resellerContext.ResellerId, customerId, memberId)
	switch {
	case err == nil:
		writeResellerAdminSuccess(c, http.StatusOK, record)
	case errors.Is(err, model.ErrResellerForbidden):
		middleware.AbortResellerRequest(c, http.StatusNotFound, middleware.ResellerErrorNotFound, "reseller subagent not found")
	case errors.Is(err, model.ErrResellerCustomerNotFound):
		middleware.AbortResellerRequest(c, http.StatusNotFound, middleware.ResellerErrorNotFound, "reseller customer not found")
	case errors.Is(err, model.ErrResellerCustomerConflict):
		middleware.AbortResellerRequest(c, http.StatusConflict, middleware.ResellerErrorConflict, "subagent administrator cannot be assigned as a customer")
	default:
		logger.LogError(c.Request.Context(), "AssignResellerCustomerSubagent database error: "+err.Error())
		middleware.AbortResellerRequest(c, http.StatusInternalServerError, middleware.ResellerErrorInternal, "internal error")
	}
}

func UpdateResellerManagementCustomerStatus(c *gin.Context) {
	resellerContext, ok := resellerManagementContext(c)
	if !ok {
		return
	}
	if !resellerManagementWriteAllowed(resellerContext.Role) {
		middleware.AbortResellerRequest(c, http.StatusForbidden, middleware.ResellerErrorForbidden, "reseller write forbidden")
		return
	}
	customerId, valid := positivePathID(c)
	if !valid {
		return
	}
	body, ok := resellerRequestBody(c, resellerManagementBodyLimit)
	if !ok {
		return
	}
	var request resellerCustomerStatusRequest
	if common.Unmarshal(body, &request) != nil || len(request.ResellerId) != 0 {
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid request")
		return
	}
	record, err := model.UpdateResellerCustomerRecordStatus(resellerContext.ResellerId, customerId, request.Status, resellerContext.Role == model.ResellerRoleOwner)
	switch {
	case err == nil:
		writeResellerAdminSuccess(c, http.StatusOK, record)
	case errors.Is(err, model.ErrInvalidResellerCustomerStatus):
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid request")
	case errors.Is(err, model.ErrResellerCustomerNotFound):
		middleware.AbortResellerRequest(c, http.StatusNotFound, middleware.ResellerErrorNotFound, "reseller customer not found")
	default:
		logger.LogError(c.Request.Context(), "UpdateResellerCustomerRecordStatus database error: "+err.Error())
		middleware.AbortResellerRequest(c, http.StatusInternalServerError, middleware.ResellerErrorInternal, "internal error")
	}
}

func UpdateResellerManagementCustomerRemark(c *gin.Context) {
	resellerContext, ok := resellerManagementContext(c)
	if !ok {
		return
	}
	if resellerContext.Role != model.ResellerRoleOwner && resellerContext.Role != model.ResellerRoleSubagent {
		middleware.AbortResellerRequest(c, http.StatusForbidden, middleware.ResellerErrorForbidden, "reseller customer remark write forbidden")
		return
	}
	customerId, valid := positivePathID(c)
	if !valid {
		return
	}
	body, ok := resellerRequestBody(c, resellerManagementBodyLimit)
	if !ok {
		return
	}
	var request resellerCustomerRemarkRequest
	if common.Unmarshal(body, &request) != nil || request.Remark == nil || len(request.ResellerId) != 0 {
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid request")
		return
	}
	var subagentMemberId *int
	if resellerContext.Role == model.ResellerRoleSubagent {
		subagentMemberId = &resellerContext.MemberId
	}
	record, err := model.UpdateResellerCustomerRecordRemark(resellerContext.ResellerId, customerId, *request.Remark, subagentMemberId)
	switch {
	case err == nil:
		writeResellerAdminSuccess(c, http.StatusOK, record)
	case errors.Is(err, model.ErrInvalidResellerCustomerRemark):
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid request")
	case errors.Is(err, model.ErrResellerCustomerNotFound):
		middleware.AbortResellerRequest(c, http.StatusNotFound, middleware.ResellerErrorNotFound, "reseller customer not found")
	default:
		logger.LogError(c.Request.Context(), "UpdateResellerCustomerRecordRemark database error: "+err.Error())
		middleware.AbortResellerRequest(c, http.StatusInternalServerError, middleware.ResellerErrorInternal, "internal error")
	}
}

func UpdateResellerManagementCustomerOverseasModelAccess(c *gin.Context) {
	resellerContext, ok := resellerManagementContext(c)
	if !ok {
		return
	}
	if !resellerManagementWriteAllowed(resellerContext.Role) && !(resellerContext.Role == model.ResellerRoleSubagent && resellerContext.CanManageCustomerAccess) {
		middleware.AbortResellerRequest(c, http.StatusForbidden, middleware.ResellerErrorForbidden, "reseller write forbidden")
		return
	}
	customerId, valid := positivePathID(c)
	if !valid {
		return
	}
	body, ok := resellerRequestBody(c, resellerManagementBodyLimit)
	if !ok {
		return
	}
	allowed, valid := resellerBooleanField(body, "allowed")
	if !valid {
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid request")
		return
	}

	var subagentMemberId *int
	if resellerContext.Role == model.ResellerRoleSubagent {
		subagentMemberId = &resellerContext.MemberId
	}
	record, err := model.UpdateResellerCustomerOverseasModelAccess(resellerContext.ResellerId, customerId, allowed, resellerContext.Role == model.ResellerRoleOwner || subagentMemberId != nil, subagentMemberId)
	switch {
	case err == nil:
		writeResellerAdminSuccess(c, http.StatusOK, record)
	case errors.Is(err, model.ErrResellerCustomerNotFound):
		middleware.AbortResellerRequest(c, http.StatusNotFound, middleware.ResellerErrorNotFound, "reseller customer not found")
	case errors.Is(err, model.ErrResellerCustomerConflict):
		middleware.AbortResellerRequest(c, http.StatusConflict, middleware.ResellerErrorConflict, "reseller customer user unavailable")
	default:
		logger.LogError(c.Request.Context(), "UpdateResellerCustomerOverseasModelAccess database error: "+err.Error())
		middleware.AbortResellerRequest(c, http.StatusInternalServerError, middleware.ResellerErrorInternal, "internal error")
	}
}

func UpdateResellerManagementCustomerPaymentPreference(c *gin.Context) {
	resellerContext, ok := resellerManagementContext(c)
	if !ok {
		return
	}
	if !resellerContext.PaymentConfigEnabled || (!resellerManagementWriteAllowed(resellerContext.Role) && !(resellerContext.Role == model.ResellerRoleSubagent && resellerContext.CanManageCustomerPayment)) {
		middleware.AbortResellerRequest(c, http.StatusForbidden, middleware.ResellerErrorForbidden, "reseller write forbidden")
		return
	}
	customerId, valid := positivePathID(c)
	if !valid {
		return
	}
	body, ok := resellerRequestBody(c, resellerManagementBodyLimit)
	if !ok {
		return
	}
	enabled, valid := resellerBooleanField(body, "enabled")
	if !valid {
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid request")
		return
	}

	var subagentMemberId *int
	if resellerContext.Role == model.ResellerRoleSubagent {
		subagentMemberId = &resellerContext.MemberId
	}
	record, err := model.UpdateResellerCustomerPaymentPreference(resellerContext.ResellerId, customerId, enabled, resellerContext.Role == model.ResellerRoleOwner || subagentMemberId != nil, subagentMemberId)
	switch {
	case err == nil:
		writeResellerAdminSuccess(c, http.StatusOK, record)
	case errors.Is(err, model.ErrResellerCustomerNotFound):
		middleware.AbortResellerRequest(c, http.StatusNotFound, middleware.ResellerErrorNotFound, "reseller customer not found")
	default:
		logger.LogError(c.Request.Context(), "UpdateResellerCustomerPaymentPreference database error: "+err.Error())
		middleware.AbortResellerRequest(c, http.StatusInternalServerError, middleware.ResellerErrorInternal, "internal error")
	}
}

func ListResellerManagementInvitations(c *gin.Context) {
	resellerContext, ok := resellerManagementContext(c)
	if !ok {
		return
	}
	if resellerContext.Role == model.ResellerRoleSubagent && !resellerContext.CanCreateInvitations {
		middleware.AbortResellerRequest(c, http.StatusForbidden, middleware.ResellerErrorForbidden, "reseller invitations forbidden")
		return
	}
	var subagentMemberId *int
	if resellerContext.Role == model.ResellerRoleSubagent {
		subagentMemberId = &resellerContext.MemberId
	}
	records, err := model.ListResellerInvitationRecords(resellerContext.ResellerId, subagentMemberId)
	if err != nil {
		logger.LogError(c.Request.Context(), "ListResellerInvitationRecords database error: "+err.Error())
		middleware.AbortResellerRequest(c, http.StatusInternalServerError, middleware.ResellerErrorInternal, "internal error")
		return
	}
	writeResellerAdminSuccess(c, http.StatusOK, records)
}

func CreateResellerManagementInvitation(c *gin.Context) {
	resellerContext, ok := resellerManagementContext(c)
	if !ok {
		return
	}
	if !resellerManagementWriteAllowed(resellerContext.Role) && !(resellerContext.Role == model.ResellerRoleSubagent && resellerContext.CanCreateInvitations) {
		middleware.AbortResellerRequest(c, http.StatusForbidden, middleware.ResellerErrorForbidden, "reseller write forbidden")
		return
	}
	body, ok := resellerRequestBody(c, resellerManagementBodyLimit)
	if !ok {
		return
	}
	var request resellerInvitationCreateRequest
	if common.Unmarshal(body, &request) != nil || len(request.ResellerId) != 0 {
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid request")
		return
	}
	expiresInHours := 72
	if request.ExpiresInHours != nil {
		expiresInHours = *request.ExpiresInHours
	}
	var subagentMemberId *int
	if resellerContext.Role == model.ResellerRoleSubagent {
		subagentMemberId = &resellerContext.MemberId
	}
	record, err := model.CreateResellerInvitationRecord(resellerContext.ResellerId, resellerContext.Subject, subagentMemberId, expiresInHours)
	switch {
	case err == nil:
		writeResellerAdminSuccess(c, http.StatusCreated, record)
	case errors.Is(err, model.ErrInvalidResellerInvitation), errors.Is(err, model.ErrInvalidResellerSubject):
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid request")
	case errors.Is(err, model.ErrResellerConflict):
		middleware.AbortResellerRequest(c, http.StatusConflict, middleware.ResellerErrorConflict, "duplicate reseller invitation")
	default:
		logger.LogError(c.Request.Context(), "CreateResellerInvitationRecord database error: "+err.Error())
		middleware.AbortResellerRequest(c, http.StatusInternalServerError, middleware.ResellerErrorInternal, "internal error")
	}
}

func RevokeResellerManagementInvitation(c *gin.Context) {
	resellerContext, ok := resellerManagementContext(c)
	if !ok {
		return
	}
	if !resellerManagementWriteAllowed(resellerContext.Role) && !(resellerContext.Role == model.ResellerRoleSubagent && resellerContext.CanCreateInvitations) {
		middleware.AbortResellerRequest(c, http.StatusForbidden, middleware.ResellerErrorForbidden, "reseller write forbidden")
		return
	}
	invitationId, valid := positivePathID(c)
	if !valid {
		return
	}
	var subagentMemberId *int
	if resellerContext.Role == model.ResellerRoleSubagent {
		subagentMemberId = &resellerContext.MemberId
	}
	record, err := model.RevokeResellerInvitationRecord(resellerContext.ResellerId, invitationId, subagentMemberId)
	switch {
	case err == nil:
		writeResellerAdminSuccess(c, http.StatusOK, record)
	case errors.Is(err, model.ErrResellerInvitationNotFound):
		middleware.AbortResellerRequest(c, http.StatusNotFound, middleware.ResellerErrorNotFound, "reseller invitation not found")
	case errors.Is(err, model.ErrResellerInvitationExpired):
		middleware.AbortResellerRequest(c, http.StatusConflict, middleware.ResellerErrorInvitationExpired, "reseller invitation expired")
	case errors.Is(err, model.ErrResellerInvitationRevoked):
		middleware.AbortResellerRequest(c, http.StatusConflict, middleware.ResellerErrorInvitationRevoked, "reseller invitation revoked")
	case errors.Is(err, model.ErrResellerInvitationConsumed):
		middleware.AbortResellerRequest(c, http.StatusConflict, middleware.ResellerErrorInvitationConsumed, "reseller invitation consumed")
	default:
		logger.LogError(c.Request.Context(), "RevokeResellerInvitationRecord database error: "+err.Error())
		middleware.AbortResellerRequest(c, http.StatusInternalServerError, middleware.ResellerErrorInternal, "internal error")
	}
}

func ConsumeResellerRegistrationInvitation(c *gin.Context) {
	body, ok := resellerRequestBody(c, resellerManagementBodyLimit)
	if !ok {
		return
	}
	var request resellerInvitationConsumeRequest
	if common.Unmarshal(body, &request) != nil || len(request.ResellerId) != 0 {
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid request")
		return
	}
	record, err := model.ConsumeResellerInvitationRecord(request.Token, request.Subject, request.MatrixName, request.Phone)
	switch {
	case err == nil:
		writeResellerAdminSuccess(c, http.StatusCreated, record)
	case errors.Is(err, model.ErrInvalidResellerInvitation):
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid request")
	case errors.Is(err, model.ErrResellerInvitationNotFound):
		middleware.AbortResellerRequest(c, http.StatusNotFound, middleware.ResellerErrorNotFound, "reseller invitation not found")
	case errors.Is(err, model.ErrResellerInvitationExpired):
		middleware.AbortResellerRequest(c, http.StatusConflict, middleware.ResellerErrorInvitationExpired, "reseller invitation expired")
	case errors.Is(err, model.ErrResellerInvitationRevoked):
		middleware.AbortResellerRequest(c, http.StatusConflict, middleware.ResellerErrorInvitationRevoked, "reseller invitation revoked")
	case errors.Is(err, model.ErrResellerInvitationConsumed):
		middleware.AbortResellerRequest(c, http.StatusConflict, middleware.ResellerErrorInvitationConsumed, "reseller invitation consumed")
	case errors.Is(err, model.ErrResellerCustomerConflict):
		middleware.AbortResellerRequest(c, http.StatusConflict, middleware.ResellerErrorConflict, "reseller customer already assigned")
	case errors.Is(err, model.ErrResellerNotFound):
		middleware.AbortResellerRequest(c, http.StatusNotFound, middleware.ResellerErrorNotFound, "reseller not found")
	default:
		logger.LogError(c.Request.Context(), "ConsumeResellerInvitationRecord database error: "+err.Error())
		middleware.AbortResellerRequest(c, http.StatusInternalServerError, middleware.ResellerErrorInternal, "internal error")
	}
}

func SyncResellerRegistrationCustomerIdentity(c *gin.Context) {
	body, ok := resellerRequestBody(c, resellerManagementBodyLimit)
	if !ok {
		return
	}
	var request resellerCustomerIdentitySyncRequest
	if common.Unmarshal(body, &request) != nil || len(request.ResellerId) != 0 {
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid request")
		return
	}
	updated, err := model.SyncResellerCustomerIdentity(request.Subject, request.MatrixName, request.Phone)
	if err == nil && request.HduVerified {
		_, err = model.ClaimHduResellerIdentity(request.Subject, request.MatrixName, request.Phone)
	}
	switch {
	case err == nil:
		writeResellerAdminSuccess(c, http.StatusOK, gin.H{"updated": updated})
	case errors.Is(err, model.ErrInvalidResellerCustomerIdentity):
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid request")
	default:
		logger.LogError(c.Request.Context(), "SyncResellerCustomerIdentity database error: "+err.Error())
		middleware.AbortResellerRequest(c, http.StatusInternalServerError, middleware.ResellerErrorInternal, "internal error")
	}
}

func ListPendingResellerRegistrationCustomerProfiles(c *gin.Context) {
	if _, exists := c.GetQuery("reseller_id"); exists {
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid request")
		return
	}
	afterId := 0
	if raw := c.Query("after_id"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 0 {
			middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid request")
			return
		}
		afterId = value
	}
	limit := resellerProfileBackfillDefaultLimit
	if raw := c.Query("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > resellerProfileBackfillMaxLimit {
			middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid request")
			return
		}
		limit = value
	}

	page, err := model.ListPendingResellerCustomerProfiles(afterId, limit)
	if err != nil {
		logger.LogError(c.Request.Context(), "ListPendingResellerCustomerProfiles database error: "+err.Error())
		middleware.AbortResellerRequest(c, http.StatusInternalServerError, middleware.ResellerErrorInternal, "internal error")
		return
	}
	writeResellerAdminSuccess(c, http.StatusOK, page)
}

func ListResellerAdminCustomers(c *gin.Context) {
	resellerId, valid := positivePathID(c)
	if !valid {
		return
	}
	if _, err := model.GetResellerAdminRecord(resellerId); errors.Is(err, model.ErrResellerNotFound) {
		middleware.AbortResellerRequest(c, http.StatusNotFound, middleware.ResellerErrorNotFound, "reseller not found")
		return
	} else if err != nil {
		logger.LogError(c.Request.Context(), "GetResellerAdminRecord database error: "+err.Error())
		middleware.AbortResellerRequest(c, http.StatusInternalServerError, middleware.ResellerErrorInternal, "internal error")
		return
	}
	records, err := model.ListResellerCustomerRecords(resellerId, true)
	if err != nil {
		logger.LogError(c.Request.Context(), "ListResellerCustomerRecords database error: "+err.Error())
		middleware.AbortResellerRequest(c, http.StatusInternalServerError, middleware.ResellerErrorInternal, "internal error")
		return
	}
	writeResellerAdminSuccess(c, http.StatusOK, records)
}

func UnbindResellerAdminCustomer(c *gin.Context) {
	resellerId, valid := positivePathID(c)
	if !valid {
		return
	}
	customerId, err := strconv.Atoi(c.Param("customer_id"))
	if err != nil || customerId < 1 {
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid request")
		return
	}
	err = model.UnbindResellerCustomerRecord(resellerId, customerId)
	switch {
	case err == nil:
		writeResellerAdminSuccess(c, http.StatusOK, gin.H{"reseller_id": resellerId, "customer_id": customerId})
	case errors.Is(err, model.ErrResellerCustomerNotFound):
		middleware.AbortResellerRequest(c, http.StatusNotFound, middleware.ResellerErrorNotFound, "reseller customer not found")
	default:
		logger.LogError(c.Request.Context(), "UnbindResellerCustomerRecord database error: "+err.Error())
		middleware.AbortResellerRequest(c, http.StatusInternalServerError, middleware.ResellerErrorInternal, "internal error")
	}
}

func BatchAssignResellerAdminCustomers(c *gin.Context) {
	resellerId, valid := positivePathID(c)
	if !valid {
		return
	}
	body, ok := resellerRequestBody(c, resellerPlatformBatchAssignBodyLimit)
	if !ok {
		return
	}

	var request resellerCustomerBatchAssignRequest
	if common.Unmarshal(body, &request) != nil || len(request.ResellerId) != 0 || len(request.Customers) == 0 || len(request.Customers) > 100 {
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid request")
		return
	}

	seenSubjects := make(map[string]struct{}, len(request.Customers))
	for _, customer := range request.Customers {
		if _, exists := seenSubjects[customer.Subject]; exists {
			middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid request")
			return
		}
		seenSubjects[customer.Subject] = struct{}{}
	}

	record, err := model.BatchAssignResellerCustomerRecords(resellerId, request.Customers)
	switch {
	case err == nil:
		writeResellerAdminSuccess(c, http.StatusOK, record)
	case errors.Is(err, model.ErrInvalidResellerCustomerIdentity):
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid request")
	case errors.Is(err, model.ErrResellerNotFound):
		middleware.AbortResellerRequest(c, http.StatusNotFound, middleware.ResellerErrorNotFound, "reseller not found")
	default:
		logger.LogError(c.Request.Context(), "BatchAssignResellerCustomerRecords database error: "+err.Error())
		middleware.AbortResellerRequest(c, http.StatusInternalServerError, middleware.ResellerErrorInternal, "internal error")
	}
}

func resellerManagementContext(c *gin.Context) (*model.ResellerContext, bool) {
	resellerContext, ok := middleware.GetResellerContext(c)
	if !ok {
		logger.LogError(c.Request.Context(), "missing reseller management context")
		middleware.AbortResellerRequest(c, http.StatusInternalServerError, middleware.ResellerErrorInternal, "internal error")
		return nil, false
	}
	if _, knownRole := resellerPermissions(resellerContext); !knownRole {
		middleware.AbortResellerRequest(c, http.StatusNotFound, middleware.ResellerErrorContextNotFound, "reseller context not found")
		return nil, false
	}
	return resellerContext, true
}

func resellerManagementWriteAllowed(role string) bool {
	return role == model.ResellerRoleOwner || role == model.ResellerRoleAdmin
}

func resellerRequestBody(c *gin.Context, limit int64) ([]byte, bool) {
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, limit))
	if err != nil {
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid request")
		return nil, false
	}
	return body, true
}

func positivePathID(c *gin.Context) (int, bool) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id < 1 {
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid request")
		return 0, false
	}
	return id, true
}
