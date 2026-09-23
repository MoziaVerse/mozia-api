package router

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service/authz"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestCallAnalyticsRoutesEnforceGeneralAdminWithoutUserManagement(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.CasbinRule{}, &model.AuthzRole{}, &model.User{}))
	oldDB, oldMaster, oldRedis, oldLimit := model.DB, common.IsMasterNode, common.RedisEnabled, common.GlobalApiRateLimitEnable
	model.DB, common.IsMasterNode, common.RedisEnabled, common.GlobalApiRateLimitEnable = db, true, false, false
	t.Cleanup(func() {
		require.NoError(t, authz.ClearUserPermissions(42))
		model.DB, common.IsMasterNode, common.RedisEnabled, common.GlobalApiRateLimitEnable = oldDB, oldMaster, oldRedis, oldLimit
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, authz.Init(db))
	require.NoError(t, authz.SetUserPermissions(42, authz.PermissionsMap{authz.ResourceUserManage: {authz.ActionRead: false}}))
	require.NoError(t, db.Create(&model.User{Id: 7073, Username: "analytics_customer", AffCode: "analytics-aff", Password: "private-password", Email: "private@example.org"}).Error)

	// Reuse the cookie-session pattern from middleware/root_auth_test.go, with the
	// real API router and its actual authorization chain (no replacement middleware).
	engine := gin.New()
	engine.Use(sessions.Sessions("session", cookie.NewStore([]byte("analytics-route-test-secret"))))
	engine.GET("/test-login/:role", func(c *gin.Context) {
		role, parseErr := strconv.Atoi(c.Param("role"))
		require.NoError(t, parseErr)
		session := sessions.Default(c)
		session.Set("username", "analytics_operator")
		session.Set("id", 42)
		session.Set("role", role)
		session.Set("status", common.UserStatusEnabled)
		session.Set("group", "default")
		require.NoError(t, session.Save())
		c.Status(http.StatusNoContent)
	})
	SetApiRouter(engine)
	request := func(path string, role int) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if role > 0 {
			login := httptest.NewRecorder()
			engine.ServeHTTP(login, httptest.NewRequest(http.MethodGet, "/test-login/"+strconv.Itoa(role), nil))
			require.Equal(t, http.StatusNoContent, login.Code)
			for _, cookie := range login.Result().Cookies() {
				req.AddCookie(cookie)
			}
			req.Header.Set("New-Api-User", "42")
		}
		response := httptest.NewRecorder()
		engine.ServeHTTP(response, req)
		return response
	}
	paths := []string{
		"/api/log/analytics?page_size=101", "/api/log/analytics/users?keyword=7073",
		"/api/log/call-report?page_size=101", "/api/log/call-report/users?keyword=7073",
	}
	for _, path := range paths {
		anonymous := request(path, 0)
		assert.Equal(t, http.StatusUnauthorized, anonymous.Code)
		assert.NotContains(t, anonymous.Body.String(), "analytics_customer")
		ordinary := request(path, common.RoleCommonUser)
		assert.Contains(t, ordinary.Body.String(), `"success":false`)
		assert.NotContains(t, ordinary.Body.String(), `"data"`)
	}
	assert.False(t, authz.Can(42, common.RoleAdminUser, authz.UserManageRead))
	assert.True(t, authz.Can(42, common.RoleAdminUser, authz.GeneralAdminAccess))
	for _, prefix := range []string{"/api/log/analytics", "/api/log/call-report"} {
		analytics := request(prefix+"?page_size=101", common.RoleAdminUser)
		assert.Equal(t, http.StatusBadRequest, analytics.Code)
		assert.Contains(t, analytics.Body.String(), "invalid pagination")
		users := request(prefix+"/users?keyword=7073", common.RoleAdminUser)
		assert.Equal(t, http.StatusOK, users.Code)
		assert.JSONEq(t, `{"success":true,"message":"","data":[{"id":7073,"username":"analytics_customer"}]}`, users.Body.String())
	}

	require.NoError(t, authz.SetUserPermissions(42, authz.PermissionsMap{authz.ResourceGeneralAdmin: {authz.ActionAccess: false}}))
	for _, path := range paths {
		denied := request(path, common.RoleAdminUser)
		assert.Equal(t, http.StatusForbidden, denied.Code)
		assert.NotContains(t, denied.Body.String(), `"data"`)
	}
}
