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

type resellerContextTestResponse struct {
	Success bool `json:"success"`
	Data    struct {
		ResellerId   int      `json:"reseller_id"`
		ResellerName string   `json:"reseller_name"`
		Host         string   `json:"host"`
		Role         string   `json:"role"`
		Permissions  []string `json:"permissions"`
		Logo         string   `json:"logo"`
	} `json:"data"`
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	RequestId string `json:"request_id"`
}

func TestResellerContextContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	originalDB := model.DB
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Reseller{}, &model.ResellerDomain{}, &model.ResellerMember{}))
	model.DB = db
	t.Setenv("MATRIX_RESELLER_SERVICE_TOKEN", "matrix-reseller-test-token")
	t.Setenv("MATRIX_RESELLER_REGISTRATION_TOKEN", "matrix-registration-test-token")
	t.Cleanup(func() {
		model.DB = originalDB
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			_ = sqlDB.Close()
		}
	})

	resellerA := model.Reseller{Name: "Agency A", Status: model.ResellerStatusActive}
	resellerB := model.Reseller{Name: "Agency B", Status: model.ResellerStatusActive}
	require.NoError(t, db.Create(&resellerA).Error)
	require.NoError(t, db.Create(&resellerB).Error)
	require.NoError(t, db.Create(&[]model.ResellerDomain{
		{ResellerId: resellerA.Id, Host: "portal-a.example.com", Verified: true, Status: model.ResellerDomainStatusActive},
		{ResellerId: resellerB.Id, Host: "portal-b.example.com", Verified: true, Status: model.ResellerDomainStatusActive},
	}).Error)
	require.NoError(t, db.Create(&[]model.ResellerMember{
		{ResellerId: resellerA.Id, Subject: "oidc-owner-a", Role: model.ResellerRoleOwner, Status: model.ResellerMemberStatusActive},
		{ResellerId: resellerB.Id, Subject: "oidc-owner-b", Role: model.ResellerRoleOwner, Status: model.ResellerMemberStatusActive},
	}).Error)
	resellerA.Logo = "data:image/png;base64,aGR1"
	matrixHost := "matrix.hdu.edu.cn"
	require.NoError(t, db.Model(&resellerA).Updates(map[string]any{"logo": resellerA.Logo, "matrix_host": matrixHost}).Error)

	engine := gin.New()
	engine.Use(middleware.RequestId())
	registerResellerRoutes(engine.Group("/api"))

	request := func(body string, token string, requestID string) (*httptest.ResponseRecorder, resellerContextTestResponse) {
		t.Helper()
		recorder := httptest.NewRecorder()
		httpRequest := httptest.NewRequest(http.MethodPost, "/api/internal/v1/reseller/context", strings.NewReader(body))
		httpRequest.Header.Set("Content-Type", "application/json")
		if token != "" {
			httpRequest.Header.Set("Authorization", "Bearer "+token)
		}
		if requestID != "" {
			httpRequest.Header.Set(common.RequestIdKey, requestID)
		}
		engine.ServeHTTP(recorder, httpRequest)
		var response resellerContextTestResponse
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
		return recorder, response
	}

	t.Run("derives tenant from normalized host and subject", func(t *testing.T) {
		recorder, response := request(
			`{"subject":"oidc-owner-a","host":"PORTAL-A.EXAMPLE.COM.:443"}`,
			"matrix-reseller-test-token",
			"reseller-request_123",
		)
		require.Equal(t, http.StatusOK, recorder.Code)
		assert.True(t, response.Success)
		assert.Equal(t, resellerA.Id, response.Data.ResellerId)
		assert.Equal(t, "Agency A", response.Data.ResellerName)
		assert.Equal(t, "portal-a.example.com", response.Data.Host)
		assert.Equal(t, model.ResellerRoleOwner, response.Data.Role)
		assert.Equal(t, []string{"reseller:read", "reseller:write", "reseller:pricing:read", "reseller:pricing:write", "reseller:invitations:read", "reseller:invitations:write"}, response.Data.Permissions)
		assert.Equal(t, "reseller-request_123", response.RequestId)
		assert.Equal(t, response.RequestId, recorder.Header().Get(common.RequestIdKey))
	})

	t.Run("resolves Matrix branding from its independent host", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		httpRequest := httptest.NewRequest(http.MethodPost, "/api/internal/v1/reseller/registration/presentation", strings.NewReader(`{"host":"MATRIX.HDU.EDU.CN.:443"}`))
		httpRequest.Header.Set("Content-Type", "application/json")
		httpRequest.Header.Set("Authorization", "Bearer matrix-registration-test-token")
		httpRequest.Header.Set(common.RequestIdKey, "matrix-brand_123")
		engine.ServeHTTP(recorder, httpRequest)
		var response resellerContextTestResponse
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
		require.Equal(t, http.StatusOK, recorder.Code)
		assert.Equal(t, "Agency A", response.Data.ResellerName)
		assert.Equal(t, "matrix.hdu.edu.cn", response.Data.Host)
		assert.Equal(t, resellerA.Logo, response.Data.Logo)
	})

	t.Run("rejects forged reseller id", func(t *testing.T) {
		recorder, response := request(
			fmt.Sprintf(`{"subject":"oidc-owner-a","host":"portal-a.example.com","reseller_id":%d}`, resellerB.Id),
			"matrix-reseller-test-token",
			"forged-tenant_123",
		)
		assert.Equal(t, http.StatusBadRequest, recorder.Code)
		assert.Equal(t, middleware.ResellerErrorInvalidRequest, response.Error.Code)
	})

	t.Run("does not disclose cross tenant or missing membership", func(t *testing.T) {
		for name, body := range map[string]string{
			"cross host":        `{"subject":"oidc-owner-a","host":"portal-b.example.com"}`,
			"unregistered port": `{"subject":"oidc-owner-a","host":"portal-a.example.com:8443"}`,
			"not a member":      `{"subject":"oidc-not-a-member","host":"portal-a.example.com"}`,
			"unknown domain":    `{"subject":"oidc-owner-a","host":"unknown.example.com"}`,
		} {
			t.Run(name, func(t *testing.T) {
				recorder, response := request(body, "matrix-reseller-test-token", "not-found_123")
				assert.Equal(t, http.StatusNotFound, recorder.Code)
				assert.Equal(t, middleware.ResellerErrorContextNotFound, response.Error.Code)
				assert.Equal(t, "reseller context not found", response.Error.Message)
			})
		}
	})

	t.Run("authenticates before propagating request id", func(t *testing.T) {
		recorder, response := request(
			`{"subject":"oidc-owner-a","host":"portal-a.example.com"}`,
			"wrong-token",
			"attacker-request_123",
		)
		assert.Equal(t, http.StatusUnauthorized, recorder.Code)
		assert.Equal(t, middleware.ResellerErrorServiceUnauthorized, response.Error.Code)
		assert.NotEqual(t, "attacker-request_123", recorder.Header().Get(common.RequestIdKey))
		assert.Equal(t, recorder.Header().Get(common.RequestIdKey), response.RequestId)
	})

	t.Run("rejects invalid request id after authentication", func(t *testing.T) {
		recorder, response := request(
			`{"subject":"oidc-owner-a","host":"portal-a.example.com"}`,
			"matrix-reseller-test-token",
			"bad request id",
		)
		assert.Equal(t, http.StatusBadRequest, recorder.Code)
		assert.Equal(t, middleware.ResellerErrorInvalidRequestID, response.Error.Code)
		assert.NotEqual(t, "bad request id", recorder.Header().Get(common.RequestIdKey))
	})
}
