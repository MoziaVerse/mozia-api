package router

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type resellerAdminItem struct {
	Id                         int     `json:"id"`
	Name                       string  `json:"name"`
	Status                     string  `json:"status"`
	Host                       string  `json:"host"`
	Logo                       string  `json:"logo"`
	Favicon                    string  `json:"favicon"`
	BrandName                  string  `json:"brand_name"`
	IcpFilingNumber            string  `json:"icp_filing_number"`
	PublicSecurityFilingNumber string  `json:"public_security_filing_number"`
	ValueAddedTelecomLicense   string  `json:"value_added_telecom_license"`
	CopyrightText              string  `json:"copyright_text"`
	OwnerSubject               string  `json:"owner_subject"`
	OwnerUserId                int     `json:"owner_user_id"`
	OwnerUsername              string  `json:"owner_username"`
	OwnerDisplayName           string  `json:"owner_display_name"`
	OwnerBalance               float64 `json:"owner_balance"`
	OwnerGiftBalance           float64 `json:"owner_gift_balance"`
	OwnerPaidBalance           float64 `json:"owner_paid_balance"`
	OwnerRequestCount          int     `json:"owner_request_count"`
	BalanceDisplayType         string  `json:"balance_display_type"`
	BalanceCurrencySymbol      string  `json:"balance_currency_symbol"`
	MemberCount                int     `json:"member_count"`
}

func TestResellerAdminPresentationContract(t *testing.T) {
	_, db, request := setupResellerAdminTest(t)
	reseller := seedReseller(t, db, "Portal Agency", model.ResellerStatusActive, "portal.example.com", "portal-owner", "portal-viewer")
	logo := "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="
	favicon := "data:image/x-icon;base64,AAABAAEAEAAAAQAgAA=="
	body := fmt.Sprintf(`{"brand_name":"杭电智算平台","logo":%q,"favicon":%q,"icp_filing_number":"浙ICP备12345678号-1","public_security_filing_number":"浙公网安备33010000000001号","value_added_telecom_license":"浙B2-20250001","copyright_text":"© 杭州电子科技大学"}`, logo, favicon)

	recorder := request(http.MethodPut, fmt.Sprintf("/api/internal/v1/platform/resellers/%d/presentation", reseller.Id), body, "mozia-mega-test-token", "admin-presentation_123")
	require.Equal(t, http.StatusOK, recorder.Code)

	list := request(http.MethodGet, "/api/internal/v1/platform/resellers", "", "mozia-mega-test-token", "admin-presentation-list_123")
	var response resellerAdminListResponse
	require.NoError(t, common.Unmarshal(list.Body.Bytes(), &response))
	require.Len(t, response.Data, 1)
	assert.Equal(t, "杭电智算平台", response.Data[0].BrandName)
	assert.Equal(t, "浙ICP备12345678号-1", response.Data[0].IcpFilingNumber)
	assert.Equal(t, "浙公网安备33010000000001号", response.Data[0].PublicSecurityFilingNumber)
	assert.Equal(t, "浙B2-20250001", response.Data[0].ValueAddedTelecomLicense)
	assert.Equal(t, "© 杭州电子科技大学", response.Data[0].CopyrightText)

	presentationRecorder := request(http.MethodPost, "/api/internal/v1/reseller/presentation", `{"host":"portal.example.com"}`, "matrix-reseller-test-token", "admin-presentation-resolve_123")
	require.Equal(t, http.StatusOK, presentationRecorder.Code)
	var presentationEnvelope resellerM2Envelope
	require.NoError(t, common.Unmarshal(presentationRecorder.Body.Bytes(), &presentationEnvelope))
	var presentation model.ResellerPresentation
	require.NoError(t, common.Unmarshal(presentationEnvelope.RawData, &presentation))
	assert.Equal(t, "杭电智算平台", presentation.BrandName)
	assert.Equal(t, "浙ICP备12345678号-1", presentation.IcpFilingNumber)
	assert.Equal(t, "浙公网安备33010000000001号", presentation.PublicSecurityFilingNumber)
	assert.Equal(t, "浙B2-20250001", presentation.ValueAddedTelecomLicense)
	assert.Equal(t, "© 杭州电子科技大学", presentation.CopyrightText)
}

type resellerAdminListResponse struct {
	Success bool                `json:"success"`
	Data    []resellerAdminItem `json:"data"`
	Error   struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	RequestId string `json:"request_id"`
}

type resellerAdminItemResponse struct {
	Success bool              `json:"success"`
	Data    resellerAdminItem `json:"data"`
	Error   struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	RequestId string `json:"request_id"`
}

type resellerAdminContextResponse struct {
	Success bool `json:"success"`
	Data    struct {
		ResellerId int `json:"reseller_id"`
	} `json:"data"`
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	RequestId string `json:"request_id"`
}

func TestResellerAdminContract(t *testing.T) {
	t.Run("uses dedicated mega token for list", func(t *testing.T) {
		_, db, request := setupResellerAdminTest(t)
		require.NoError(t, db.Create(&model.User{
			Username: "agency-owner-a", DisplayName: "代理商负责人 A", Password: "test-password", OidcId: "oidc-owner-a", AffCode: "aff-owner-a",
		}).Error)
		require.NoError(t, db.Create(&model.User{
			Username: "duplicate-owner-a", DisplayName: "重复绑定账号", Password: "test-password", OidcId: "oidc-owner-a", AffCode: "aff-duplicate-owner-a",
		}).Error)
		matrixOwner := model.User{
			Username: "matrix-owner-a", DisplayName: "Matrix 代理商负责人", Password: "test-password", AffCode: "aff-matrix-owner-a", Quota: 7000000, UsedQuota: 3000000, RequestCount: 42,
		}
		require.NoError(t, db.Create(&matrixOwner).Error)
		require.NoError(t, db.Create(&model.UserSSO{SSOSub: "oidc-owner-a", UserId: matrixOwner.Id}).Error)
		require.NoError(t, db.Create(&[]model.MoziaWalletBalance{
			{UserId: matrixOwner.Id, Source: model.MoziaWalletSourceGift, Balance: 2000000},
			{UserId: matrixOwner.Id, Source: model.MoziaWalletSourcePaid, Balance: 4000000},
		}).Error)
		seedReseller(t, db, "Agency A", model.ResellerStatusActive, "portal-a.example.com", "oidc-owner-a", "oidc-viewer-a")

		recorder := request(http.MethodGet, "/api/internal/v1/platform/resellers", "", "matrix-reseller-test-token", "admin-list_123")
		var unauthorized resellerAdminListResponse
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &unauthorized))
		require.Equal(t, http.StatusUnauthorized, recorder.Code)
		assert.Equal(t, middleware.ResellerErrorServiceUnauthorized, unauthorized.Error.Code)

		recorder = request(http.MethodGet, "/api/internal/v1/platform/resellers", "", "mozia-mega-test-token", "admin-list_123")
		var response resellerAdminListResponse
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
		require.Equal(t, http.StatusOK, recorder.Code)
		require.Len(t, response.Data, 1)
		assert.True(t, response.Success)
		assert.Equal(t, "admin-list_123", response.RequestId)
		assert.Equal(t, "Agency A", response.Data[0].Name)
		assert.Equal(t, model.ResellerStatusActive, response.Data[0].Status)
		assert.Equal(t, "portal-a.example.com", response.Data[0].Host)
		assert.Equal(t, "oidc-owner-a", response.Data[0].OwnerSubject)
		assert.Equal(t, matrixOwner.Id, response.Data[0].OwnerUserId)
		assert.Equal(t, "matrix-owner-a", response.Data[0].OwnerUsername)
		assert.Equal(t, "Matrix 代理商负责人", response.Data[0].OwnerDisplayName)
		assert.Equal(t, 14.0, response.Data[0].OwnerBalance)
		assert.Equal(t, 4.0, response.Data[0].OwnerGiftBalance)
		assert.Equal(t, 8.0, response.Data[0].OwnerPaidBalance)
		assert.Equal(t, operation_setting.QuotaDisplayTypeUSD, response.Data[0].BalanceDisplayType)
		assert.Equal(t, "$", response.Data[0].BalanceCurrencySymbol)
		assert.Equal(t, 42, response.Data[0].OwnerRequestCount)
		assert.Equal(t, 2, response.Data[0].MemberCount)
	})

	t.Run("creates reseller with normalized host and active owner records", func(t *testing.T) {
		_, db, request := setupResellerAdminTest(t)
		require.NoError(t, db.Create(&model.User{
			Username: "agency-owner-new", DisplayName: "新代理商负责人", Password: "test-password", OidcId: "oidc-owner-new", AffCode: "aff-owner-new",
		}).Error)

		recorder := request(
			http.MethodPost,
			"/api/internal/v1/platform/resellers",
			`{"name":"Agency New","host":"Portal-New.example.com.:443","owner_subject":"oidc-owner-new"}`,
			"mozia-mega-test-token",
			"",
		)
		var response resellerAdminItemResponse
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
		require.Equal(t, http.StatusCreated, recorder.Code)
		assert.True(t, response.Success)
		assert.NotEmpty(t, response.RequestId)
		assert.Equal(t, response.RequestId, recorder.Header().Get(common.RequestIdKey))
		assert.Equal(t, "Agency New", response.Data.Name)
		assert.Equal(t, model.ResellerStatusActive, response.Data.Status)
		assert.Equal(t, "portal-new.example.com", response.Data.Host)
		assert.Equal(t, "oidc-owner-new", response.Data.OwnerSubject)
		assert.Equal(t, "agency-owner-new", response.Data.OwnerUsername)
		assert.Equal(t, "新代理商负责人", response.Data.OwnerDisplayName)
		assert.Equal(t, 1, response.Data.MemberCount)

		var reseller model.Reseller
		require.NoError(t, db.First(&reseller, response.Data.Id).Error)
		assert.Equal(t, "Agency New", reseller.Name)
		assert.Equal(t, model.ResellerStatusActive, reseller.Status)

		var domain model.ResellerDomain
		require.NoError(t, db.Where("reseller_id = ?", reseller.Id).First(&domain).Error)
		assert.Equal(t, "portal-new.example.com", domain.Host)
		assert.True(t, domain.Verified)
		assert.Equal(t, model.ResellerDomainStatusActive, domain.Status)

		var member model.ResellerMember
		require.NoError(t, db.Where("reseller_id = ?", reseller.Id).First(&member).Error)
		assert.Equal(t, "oidc-owner-new", member.Subject)
		assert.Equal(t, model.ResellerRoleOwner, member.Role)
		assert.Equal(t, model.ResellerMemberStatusActive, member.Status)
	})

	t.Run("allows multiple resellers on the shared platform host", func(t *testing.T) {
		_, db, request := setupResellerAdminTest(t)

		create := func(name string, subject string) resellerAdminItemResponse {
			recorder := request(
				http.MethodPost,
				"/api/internal/v1/platform/resellers",
				fmt.Sprintf(`{"name":%q,"host":"reseller.mzsjai.com","owner_subject":%q}`, name, subject),
				"mozia-mega-test-token",
				"shared-host-create_123",
			)
			var response resellerAdminItemResponse
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			require.Equal(t, http.StatusCreated, recorder.Code)
			assert.Equal(t, "reseller.mzsjai.com", response.Data.Host)
			return response
		}

		resellerA := create("Shared Agency A", "shared-owner-a")
		resellerB := create("Shared Agency B", "shared-owner-b")

		var domainCount int64
		require.NoError(t, db.Model(&model.ResellerDomain{}).Where("host = ?", "reseller.mzsjai.com").Count(&domainCount).Error)
		assert.Zero(t, domainCount)

		for subject, resellerId := range map[string]int{
			"shared-owner-a": resellerA.Data.Id,
			"shared-owner-b": resellerB.Data.Id,
		} {
			recorder := request(
				http.MethodPost,
				"/api/internal/v1/reseller/context",
				fmt.Sprintf(`{"subject":%q,"host":"reseller.mzsjai.com"}`, subject),
				"matrix-reseller-test-token",
				"shared-host-context_123",
			)
			var response resellerAdminContextResponse
			require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
			require.Equal(t, http.StatusOK, recorder.Code)
			assert.Equal(t, resellerId, response.Data.ResellerId)
		}

		require.NoError(t, db.Create(&model.ResellerMember{
			ResellerId: resellerB.Data.Id,
			Subject:    "shared-owner-a",
			Role:       model.ResellerRoleViewer,
			Status:     model.ResellerMemberStatusActive,
		}).Error)
		ambiguous := request(
			http.MethodPost,
			"/api/internal/v1/reseller/context",
			`{"subject":"shared-owner-a","host":"reseller.mzsjai.com"}`,
			"matrix-reseller-test-token",
			"shared-host-ambiguous_123",
		)
		var ambiguousResponse resellerAdminContextResponse
		require.NoError(t, common.Unmarshal(ambiguous.Body.Bytes(), &ambiguousResponse))
		require.Equal(t, http.StatusNotFound, ambiguous.Code)
		assert.Equal(t, middleware.ResellerErrorContextNotFound, ambiguousResponse.Error.Code)
	})

	t.Run("rejects duplicate host without partial rows", func(t *testing.T) {
		_, db, request := setupResellerAdminTest(t)
		seedReseller(t, db, "Agency A", model.ResellerStatusActive, "portal-a.example.com", "oidc-owner-a")

		beforeCounts := resellerTableCounts(t, db)
		recorder := request(
			http.MethodPost,
			"/api/internal/v1/platform/resellers",
			`{"name":"Agency Duplicate","host":"PORTAL-A.EXAMPLE.COM","owner_subject":"oidc-owner-duplicate"}`,
			"mozia-mega-test-token",
			"duplicate-request_123",
		)
		var response resellerAdminItemResponse
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
		require.Equal(t, http.StatusConflict, recorder.Code)
		assert.False(t, response.Success)
		assert.Equal(t, middleware.ResellerErrorConflict, response.Error.Code)
		assert.Equal(t, beforeCounts, resellerTableCounts(t, db))
	})

	t.Run("edits reseller name and switches between shared and custom hosts", func(t *testing.T) {
		_, db, request := setupResellerAdminTest(t)
		reseller := seedReseller(t, db, "Agency A", model.ResellerStatusActive, "portal-a.example.com", "oidc-owner-a")

		shared := request(
			http.MethodPatch,
			fmt.Sprintf("/api/internal/v1/platform/resellers/%d", reseller.Id),
			`{"name":"Agency A","host":"reseller.mzsjai.com"}`,
			"mozia-mega-test-token",
			"edit-shared_123",
		)
		var sharedResponse resellerAdminItemResponse
		require.NoError(t, common.Unmarshal(shared.Body.Bytes(), &sharedResponse))
		require.Equal(t, http.StatusOK, shared.Code)
		assert.Equal(t, "Agency A", sharedResponse.Data.Name)
		assert.Equal(t, "reseller.mzsjai.com", sharedResponse.Data.Host)

		var domainCount int64
		require.NoError(t, db.Model(&model.ResellerDomain{}).Where("reseller_id = ?", reseller.Id).Count(&domainCount).Error)
		assert.Zero(t, domainCount)

		custom := request(
			http.MethodPatch,
			fmt.Sprintf("/api/internal/v1/platform/resellers/%d", reseller.Id),
			`{"name":"Agency Custom","host":"Portal-New.example.com.:443"}`,
			"mozia-mega-test-token",
			"edit-custom_123",
		)
		var customResponse resellerAdminItemResponse
		require.NoError(t, common.Unmarshal(custom.Body.Bytes(), &customResponse))
		require.Equal(t, http.StatusOK, custom.Code)
		assert.Equal(t, "Agency Custom", customResponse.Data.Name)
		assert.Equal(t, "portal-new.example.com", customResponse.Data.Host)

		var domain model.ResellerDomain
		require.NoError(t, db.Where("reseller_id = ?", reseller.Id).Take(&domain).Error)
		assert.Equal(t, "portal-new.example.com", domain.Host)
		assert.True(t, domain.Verified)
		assert.Equal(t, model.ResellerDomainStatusActive, domain.Status)
	})

	t.Run("duplicate edited host rolls back reseller changes", func(t *testing.T) {
		_, db, request := setupResellerAdminTest(t)
		resellerA := seedReseller(t, db, "Agency A", model.ResellerStatusActive, "portal-a.example.com", "oidc-owner-a")
		seedReseller(t, db, "Agency B", model.ResellerStatusActive, "portal-b.example.com", "oidc-owner-b")

		recorder := request(
			http.MethodPatch,
			fmt.Sprintf("/api/internal/v1/platform/resellers/%d", resellerA.Id),
			`{"name":"Agency Changed","host":"PORTAL-B.EXAMPLE.COM"}`,
			"mozia-mega-test-token",
			"edit-conflict_123",
		)
		var response resellerAdminItemResponse
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
		require.Equal(t, http.StatusConflict, recorder.Code)
		assert.Equal(t, middleware.ResellerErrorConflict, response.Error.Code)

		var persisted model.Reseller
		require.NoError(t, db.First(&persisted, resellerA.Id).Error)
		assert.Equal(t, "Agency A", persisted.Name)
		var domain model.ResellerDomain
		require.NoError(t, db.Where("reseller_id = ?", resellerA.Id).Take(&domain).Error)
		assert.Equal(t, "portal-a.example.com", domain.Host)
	})

	t.Run("suspended reseller stops resolving existing context immediately", func(t *testing.T) {
		_, db, request := setupResellerAdminTest(t)
		reseller := seedReseller(t, db, "Agency A", model.ResellerStatusActive, "portal-a.example.com", "oidc-owner-a")

		contextRecorder := request(
			http.MethodPost,
			"/api/internal/v1/reseller/context",
			`{"subject":"oidc-owner-a","host":"portal-a.example.com"}`,
			"matrix-reseller-test-token",
			"context-before_123",
		)
		var contextBefore resellerAdminContextResponse
		require.NoError(t, common.Unmarshal(contextRecorder.Body.Bytes(), &contextBefore))
		require.Equal(t, http.StatusOK, contextRecorder.Code)
		assert.Equal(t, reseller.Id, contextBefore.Data.ResellerId)

		recorder := request(
			http.MethodPatch,
			fmt.Sprintf("/api/internal/v1/platform/resellers/%d/status", reseller.Id),
			`{"status":"suspended"}`,
			"mozia-mega-test-token",
			"status-request_123",
		)
		var updateResponse resellerAdminItemResponse
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &updateResponse))
		require.Equal(t, http.StatusOK, recorder.Code)
		assert.Equal(t, model.ResellerStatusSuspended, updateResponse.Data.Status)

		contextRecorder = request(
			http.MethodPost,
			"/api/internal/v1/reseller/context",
			`{"subject":"oidc-owner-a","host":"portal-a.example.com"}`,
			"matrix-reseller-test-token",
			"context-after_123",
		)
		var contextAfter resellerAdminContextResponse
		require.NoError(t, common.Unmarshal(contextRecorder.Body.Bytes(), &contextAfter))
		require.Equal(t, http.StatusNotFound, contextRecorder.Code)
		assert.Equal(t, middleware.ResellerErrorContextNotFound, contextAfter.Error.Code)
		assert.Equal(t, "reseller context not found", contextAfter.Error.Message)
	})
}

func setupResellerAdminTest(t *testing.T) (*gin.Engine, *gorm.DB, func(method string, path string, body string, token string, requestID string) *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	originalDB := model.DB
	originalQuotaPerUnit := common.QuotaPerUnit
	originalDisplayType := operation_setting.GetQuotaDisplayType()
	common.QuotaPerUnit = 500000
	operation_setting.GetGeneralSetting().QuotaDisplayType = operation_setting.QuotaDisplayTypeUSD
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.UserSSO{}, &model.MoziaWalletBalance{}, &model.Reseller{}, &model.ResellerDomain{}, &model.ResellerMember{}))
	model.DB = db
	t.Setenv("MATRIX_RESELLER_SERVICE_TOKEN", "matrix-reseller-test-token")
	t.Setenv("MOZIA_MEGA_SERVICE_TOKEN", "mozia-mega-test-token")
	t.Setenv("MATRIX_RESELLER_SHARED_HOST", "reseller.mzsjai.com")
	t.Cleanup(func() {
		model.DB = originalDB
		common.QuotaPerUnit = originalQuotaPerUnit
		operation_setting.GetGeneralSetting().QuotaDisplayType = originalDisplayType
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			_ = sqlDB.Close()
		}
	})

	engine := gin.New()
	registerResellerRoutes(engine.Group("/api"))

	request := func(method string, path string, body string, token string, requestID string) *httptest.ResponseRecorder {
		t.Helper()
		recorder := httptest.NewRecorder()
		httpRequest := httptest.NewRequest(method, path, strings.NewReader(body))
		if body != "" {
			httpRequest.Header.Set("Content-Type", "application/json")
		}
		if token != "" {
			httpRequest.Header.Set("Authorization", "Bearer "+token)
		}
		if requestID != "" {
			httpRequest.Header.Set(common.RequestIdKey, requestID)
		}
		engine.ServeHTTP(recorder, httpRequest)
		return recorder
	}
	return engine, db, request
}

func seedReseller(t *testing.T, db *gorm.DB, name string, status string, host string, ownerSubject string, extraSubjects ...string) model.Reseller {
	t.Helper()
	reseller := model.Reseller{Name: name, Status: status}
	require.NoError(t, db.Create(&reseller).Error)
	require.NoError(t, db.Create(&model.ResellerDomain{
		ResellerId: reseller.Id,
		Host:       host,
		Verified:   true,
		Status:     model.ResellerDomainStatusActive,
	}).Error)
	require.NoError(t, db.Create(&model.ResellerMember{
		ResellerId: reseller.Id,
		Subject:    ownerSubject,
		Role:       model.ResellerRoleOwner,
		Status:     model.ResellerMemberStatusActive,
	}).Error)
	for _, subject := range extraSubjects {
		require.NoError(t, db.Create(&model.ResellerMember{
			ResellerId: reseller.Id,
			Subject:    subject,
			Role:       model.ResellerRoleViewer,
			Status:     model.ResellerMemberStatusActive,
		}).Error)
	}
	return reseller
}

func resellerTableCounts(t *testing.T, db *gorm.DB) [3]int64 {
	t.Helper()
	var resellerCount int64
	var domainCount int64
	var memberCount int64
	require.NoError(t, db.Model(&model.Reseller{}).Count(&resellerCount).Error)
	require.NoError(t, db.Model(&model.ResellerDomain{}).Count(&domainCount).Error)
	require.NoError(t, db.Model(&model.ResellerMember{}).Count(&memberCount).Error)
	return [3]int64{resellerCount, domainCount, memberCount}
}
