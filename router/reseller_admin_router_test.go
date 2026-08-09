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
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type resellerAdminItem struct {
	Id                int    `json:"id"`
	Name              string `json:"name"`
	Status            string `json:"status"`
	Host              string `json:"host"`
	OwnerSubject      string `json:"owner_subject"`
	OwnerUserId       int    `json:"owner_user_id"`
	OwnerUsername     string `json:"owner_username"`
	OwnerDisplayName  string `json:"owner_display_name"`
	OwnerQuota        int    `json:"owner_quota"`
	OwnerUsedQuota    int    `json:"owner_used_quota"`
	OwnerRequestCount int    `json:"owner_request_count"`
	MemberCount       int    `json:"member_count"`
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
		assert.Equal(t, 7000000, response.Data[0].OwnerQuota)
		assert.Equal(t, 3000000, response.Data[0].OwnerUsedQuota)
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
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.UserSSO{}, &model.Reseller{}, &model.ResellerDomain{}, &model.ResellerMember{}))
	model.DB = db
	t.Setenv("MATRIX_RESELLER_SERVICE_TOKEN", "matrix-reseller-test-token")
	t.Setenv("MOZIA_MEGA_SERVICE_TOKEN", "mozia-mega-test-token")
	t.Cleanup(func() {
		model.DB = originalDB
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
