package router

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/mozia_setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResellerModelAccessContract(t *testing.T) {
	_, db, request := setupResellerAdminTest(t)
	require.NoError(t, db.AutoMigrate(&model.ResellerCustomer{}, &model.Ability{}, &model.Channel{}, &model.Token{}))
	agency := seedReseller(t, db, "Agency", model.ResellerStatusActive, "reseller.example.com", "owner")
	require.NoError(t, db.Model(&agency).Update("matrix_host", "portal.example.com").Error)
	user := model.User{Username: "customer", OidcId: "customer-sub", Group: "ext", Quota: 123456}
	require.NoError(t, db.Create(&user).Error)
	require.NoError(t, db.Create(&model.UserSSO{UserId: user.Id, SSOSub: "customer-sub"}).Error)
	require.NoError(t, db.Create(&model.ResellerCustomer{ResellerId: agency.Id, Subject: "customer-sub", Status: model.ResellerCustomerStatusActive}).Error)
	key := model.Token{UserId: user.Id, Key: "reseller-fixture-key", Group: "auto", ModelLimitsEnabled: true, ModelLimits: "allowed,blocked"}
	require.NoError(t, db.Create(&key).Error)
	require.NoError(t, db.Create(&[]model.Ability{
		{Group: "default", Model: "allowed", ChannelId: 1, Enabled: true},
		{Group: "default", Model: "blocked", ChannelId: 1, Enabled: true},
	}).Error)
	endpoint := fmt.Sprintf("/api/internal/v1/platform/resellers/%d/model-access", agency.Id)
	t.Setenv("MATRIX_RESELLER_REGISTRATION_TOKEN", "registration-test-token")

	// Pre-migration rows have NULL policy and keep existing access.
	require.NoError(t, db.Model(&agency).Update("model_access", nil).Error)
	policy, err := model.GetUserResellerModelAccess(user.Id)
	require.NoError(t, err)
	assert.True(t, policy.Allows("blocked"))
	unauthorized := request(http.MethodPut, endpoint, `{"restricted":true,"models":[]}`, "matrix-reseller-test-token", "model-access-auth")
	assert.Equal(t, 401, unauthorized.Code)
	for _, body := range []string{`{}`, `{"restricted":true}`, `{"restricted":null,"models":[]}`, `{"restricted":true,"models":["unknown"]}`, `{"restricted":"false","models":[]}`} {
		response := request(http.MethodPut, endpoint, body, "mozia-mega-test-token", "model-access-invalid")
		assert.Equal(t, 400, response.Code, body)
	}
	response := request(http.MethodPut, endpoint, `{"restricted":true,"models":[" allowed ","allowed"]}`, "mozia-mega-test-token", "model-access-save")
	require.Equal(t, 200, response.Code, response.Body.String())
	policy, err = model.GetUserResellerModelAccess(user.Id)
	require.NoError(t, err)
	assert.Equal(t, []string{"allowed"}, policy.Models)
	assert.Nil(t, service.EnforceResellerModelAccess(user.Id, "allowed"))
	require.NotNil(t, service.EnforceResellerModelAccess(user.Id, "blocked"))
	assert.Equal(t, 403, service.EnforceResellerModelAccess(user.Id, "blocked").StatusCode)
	other, err := model.GetUserResellerModelAccess(user.Id + 100)
	require.NoError(t, err)
	assert.True(t, other.Allows("blocked"))
	// Legacy branding writes must never reset authorization.
	response = request(http.MethodPut, strings.TrimSuffix(endpoint, "model-access")+"presentation", `{"brand_name":"Agency","logo":"","favicon":""}`, "mozia-mega-test-token", "model-access-brand")
	require.Equal(t, 200, response.Code)
	response = request(http.MethodPost, "/api/internal/v1/reseller/registration/presentation", `{"host":"portal.example.com"}`, "registration-test-token", "model-access-host")
	require.Equal(t, 200, response.Code)
	var resolved struct{ Data model.ResellerPresentation }
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &resolved))
	assert.Equal(t, policy, resolved.Data.ModelAccess)
	response = request(http.MethodGet, "/api/internal/v1/platform/resellers", "", "mozia-mega-test-token", "model-access-list")
	require.Equal(t, 200, response.Code)
	var listed struct{ Data []model.ResellerAdminRecord }
	require.NoError(t, common.Unmarshal(response.Body.Bytes(), &listed))
	require.Len(t, listed.Data, 1)
	assert.Equal(t, policy, listed.Data[0].ModelAccess)
	// Grants can be reduced to zero, then reset. Existing key and user settings remain unchanged.
	for _, body := range []string{`{"restricted":true,"models":[]}`, `{"restricted":false,"models":[]}`} {
		response = request(http.MethodPut, endpoint, body, "mozia-mega-test-token", "model-access-change")
		require.Equal(t, 200, response.Code)
		policy, err = model.GetUserResellerModelAccess(user.Id)
		require.NoError(t, err)
		assert.Equal(t, !policy.Restricted, policy.Allows("allowed"))
	}
	var persisted model.Token
	require.NoError(t, db.First(&persisted, key.Id).Error)
	assert.Equal(t, key.ModelLimits, persisted.ModelLimits)
	assert.Equal(t, key.Group, persisted.Group)
	require.NoError(t, db.First(&user, user.Id).Error)
	assert.Equal(t, "ext", user.Group)
	assert.Equal(t, 123456, user.Quota)
	// Ownership changes and suspension affect the same existing key immediately.
	second := seedReseller(t, db, "Second Agency", model.ResellerStatusActive, "second.example.com", "second-owner")
	require.NoError(t, db.Model(&second).Update("model_access", model.ResellerModelAccess{Restricted: true, Models: []string{"allowed"}}).Error)
	require.NoError(t, db.Model(&model.ResellerCustomer{}).Where("subject = ?", "customer-sub").Update("reseller_id", second.Id).Error)
	assert.Nil(t, service.EnforceResellerModelAccess(user.Id, "allowed"))
	require.NotNil(t, service.EnforceResellerModelAccess(user.Id, "blocked"))
	require.NoError(t, db.Model(&second).Update("status", model.ResellerStatusSuspended).Error)
	require.NotNil(t, service.EnforceResellerModelAccess(user.Id, "allowed"))
	// Invalid stored authorization must fail closed, never become unrestricted.
	require.NoError(t, db.Model(&second).Update("model_access", "invalid-json").Error)
	apiErr := service.EnforceResellerModelAccess(user.Id, "allowed")
	require.NotNil(t, apiErr)
	assert.Equal(t, http.StatusServiceUnavailable, apiErr.StatusCode)
}

func TestResellerModelAccessRelayAndCatalog(t *testing.T) {
	// Initialize dialect-specific column names against an isolated database.
	t.Setenv("SQL_DSN", "local")
	wasMaster, sqlitePath := common.IsMasterNode, common.SQLitePath
	common.IsMasterNode, common.SQLitePath = false, "file:reseller-model-access-init?mode=memory&cache=shared"
	require.NoError(t, model.InitDB())
	common.IsMasterNode, common.SQLitePath = wasMaster, sqlitePath
	sqlDB, err := model.DB.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())
	_, db, _ := setupResellerAdminTest(t)
	require.NoError(t, db.AutoMigrate(&model.ResellerCustomer{}, &model.Channel{}, &model.Ability{}))
	agency := seedReseller(t, db, "Agency", model.ResellerStatusActive, "portal.example.com", "owner")
	user := model.User{Username: "customer", OidcId: "customer-sub"}
	require.NoError(t, db.Create(&user).Error)
	require.NoError(t, db.Create(&model.ResellerCustomer{ResellerId: agency.Id, Subject: user.OidcId, Status: model.ResellerCustomerStatusActive}).Error)
	policy := model.ResellerModelAccess{Restricted: true, Models: []string{"allowed"}}
	require.NoError(t, db.Model(&agency).Update("model_access", policy).Error)
	channel := model.Channel{Type: constant.ChannelTypeOpenAI, Name: "test", Key: "upstream-fixture", Models: "allowed,blocked", Group: "default", Status: common.ChannelStatusEnabled}
	require.NoError(t, db.Create(&channel).Error)
	originalSelfUse := operation_setting.SelfUseModeEnabled
	operation_setting.SelfUseModeEnabled = true
	t.Cleanup(func() { operation_setting.SelfUseModeEnabled = originalSelfUse })
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set("id", user.Id)
		common.SetContextKey(c, constant.ContextKeyUserGroup, "default")
		common.SetContextKey(c, constant.ContextKeyTokenSpecificChannelId, fmt.Sprint(channel.Id))
		common.SetContextKey(c, constant.ContextKeyTokenModelLimitEnabled, true)
		common.SetContextKey(c, constant.ContextKeyTokenModelLimit, map[string]bool{"allowed": true, "blocked": true})
	})
	engine.GET("/v1/models", func(c *gin.Context) { controller.ListModels(c, constant.ChannelTypeAnthropic) })
	engine.GET("/v1/models/:model", func(c *gin.Context) { controller.RetrieveModel(c, constant.ChannelTypeOpenAI) })
	engine.POST("/v1/chat/completions", middleware.Distribute(), func(c *gin.Context) { c.Status(204) })
	engine.POST("/v1/messages", middleware.Distribute(), func(c *gin.Context) { c.Status(204) })
	engine.POST("/v1beta/models/:model", middleware.Distribute(), func(c *gin.Context) { c.Status(204) })
	for _, test := range []struct {
		path, body string
		status     int
	}{
		{"/v1/chat/completions", `{"model":"allowed"}`, 204},
		{"/v1/chat/completions", `{"model":"blocked"}`, 403},
		{"/v1/messages", `{"model":"blocked"}`, 403},
		{"/v1beta/models/blocked:generateContent", `{}`, 403},
	} {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest("POST", test.path, strings.NewReader(test.body))
		req.Header.Set("Content-Type", "application/json")
		engine.ServeHTTP(recorder, req)
		assert.Equal(t, test.status, recorder.Code, recorder.Body.String())
	}
	for _, empty := range []bool{false, true} {
		if empty {
			require.NoError(t, db.Model(&agency).Update("model_access", model.ResellerModelAccess{Restricted: true}).Error)
		}
		recorder := httptest.NewRecorder()
		engine.ServeHTTP(recorder, httptest.NewRequest("GET", "/v1/models", nil))
		require.Equal(t, 200, recorder.Code)
		var result struct {
			Data []struct {
				ID string `json:"id"`
			}
		}
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &result))
		if empty {
			assert.Empty(t, result.Data)
		} else {
			require.Len(t, result.Data, 1)
			assert.Equal(t, "allowed", result.Data[0].ID)
		}
	}
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest("GET", "/v1/models/blocked", nil))
	assert.Equal(t, 404, recorder.Code)
	// Redirect targets are checked before channel selection, not only the advertised alias.
	originalRedirects := mozia_setting.UserModelRedirects2JSONString()
	t.Cleanup(func() { require.NoError(t, mozia_setting.UpdateUserModelRedirectsByJSONString(originalRedirects)) })
	require.NoError(t, mozia_setting.UpdateUserModelRedirectsByJSONString(fmt.Sprintf(`{"%d:allowed":{"user_id":%d,"source_model":"allowed","target_model":"blocked","only_thinking_disabled":false}}`, user.Id, user.Id)))
	require.NoError(t, db.Model(&agency).Update("model_access", policy).Error)
	redirectRouter := gin.New()
	redirectRouter.POST("/v1/chat/completions", func(c *gin.Context) { c.Set("id", user.Id) }, middleware.Distribute(), func(c *gin.Context) { c.Status(204) })
	recorder = httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"allowed"}`))
	req.Header.Set("Content-Type", "application/json")
	redirectRouter.ServeHTTP(recorder, req)
	assert.Equal(t, 403, recorder.Code, recorder.Body.String())
}
