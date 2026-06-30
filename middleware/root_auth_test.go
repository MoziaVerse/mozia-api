package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func performRootOnlySessionRequest(t *testing.T, role int, addUserHeader bool) *httptest.ResponseRecorder {
	t.Helper()

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(sessions.Sessions("session", cookie.NewStore([]byte("root-auth-test"))))
	router.GET("/login", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("username", "root")
		session.Set("role", role)
		session.Set("id", 42)
		session.Set("status", common.UserStatusEnabled)
		session.Set("group", "default")
		require.NoError(t, session.Save())
		c.Status(http.StatusNoContent)
	})
	router.GET("/api/test", RootOnlyAuth(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"id":      c.GetInt("id"),
		})
	})

	loginRecorder := httptest.NewRecorder()
	loginRequest := httptest.NewRequest(http.MethodGet, "/login", nil)
	router.ServeHTTP(loginRecorder, loginRequest)
	require.Equal(t, http.StatusNoContent, loginRecorder.Code)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	for _, item := range loginRecorder.Result().Cookies() {
		request.AddCookie(item)
	}
	if addUserHeader {
		request.Header.Set("New-Api-User", "42")
	}
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestRootOnlyAuthAllowsDashboardRootSession(t *testing.T) {
	recorder := performRootOnlySessionRequest(t, common.RoleRootUser, true)

	require.Equal(t, http.StatusOK, recorder.Code)
	require.JSONEq(t, `{"success":true,"id":42}`, recorder.Body.String())
}

func TestRootOnlyAuthRejectsDashboardAdminSessionWithoutExpiringLogin(t *testing.T) {
	recorder := performRootOnlySessionRequest(t, common.RoleAdminUser, true)

	require.Equal(t, http.StatusForbidden, recorder.Code)
}

func TestRootOnlyAuthRequiresUserHeaderForDashboardSession(t *testing.T) {
	recorder := performRootOnlySessionRequest(t, common.RoleRootUser, false)

	require.Equal(t, http.StatusUnauthorized, recorder.Code)
}
