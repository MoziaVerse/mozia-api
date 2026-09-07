package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupResellerRateLimitsForTest(t *testing.T) {
	t.Helper()
	oldRedis := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = oldRedis })
	// Contract fixtures share test credentials and do not run a Redis server.
	// The rate-limit regression below supplies its own small budgets.
	t.Setenv("RESELLER_SERVICE_READ_RATE_LIMIT", "10000")
	t.Setenv("RESELLER_SERVICE_WRITE_RATE_LIMIT", "10000")
}

func TestResellerServiceLimitsAreIndependentOfPublicIPAndWrites(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldRedis, oldEnable, oldNum, oldDuration := common.RedisEnabled, common.GlobalApiRateLimitEnable, common.GlobalApiRateLimitNum, common.GlobalApiRateLimitDuration
	common.RedisEnabled, common.GlobalApiRateLimitEnable, common.GlobalApiRateLimitNum, common.GlobalApiRateLimitDuration = false, true, 1, 60
	t.Cleanup(func() {
		common.RedisEnabled, common.GlobalApiRateLimitEnable, common.GlobalApiRateLimitNum, common.GlobalApiRateLimitDuration = oldRedis, oldEnable, oldNum, oldDuration
	})
	const registrationToken = "rate-limit-test-registration"
	const megaToken = "rate-limit-test-mega"
	t.Setenv("MATRIX_RESELLER_REGISTRATION_TOKEN", registrationToken)
	t.Setenv("MOZIA_MEGA_SERVICE_TOKEN", megaToken)
	t.Setenv("RESELLER_SERVICE_READ_RATE_LIMIT", "2")
	t.Setenv("RESELLER_SERVICE_WRITE_RATE_LIMIT", "1")
	t.Setenv("RESELLER_SERVICE_RATE_LIMIT_DURATION", "60")
	engine := gin.New()
	engine.Use(sessions.Sessions("test", cookie.NewStore([]byte("test-session-secret"))))
	SetApiRouter(engine)
	request := func(method, path, token, ip string) *httptest.ResponseRecorder {
		t.Helper()
		// Invalid JSON exercises real auth/routing/limiting, without database writes.
		req := httptest.NewRequest(method, path, strings.NewReader("{"))
		req.RemoteAddr = ip + ":1234"
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		res := httptest.NewRecorder()
		engine.ServeHTTP(res, req)
		return res
	}
	const ip = "192.0.2.187"
	assert.Equal(t, http.StatusUnauthorized, request("GET", "/api/user/self", "", ip).Code)
	assert.Equal(t, http.StatusTooManyRequests, request("GET", "/api/user/self", "", ip).Code)
	const readPath = "/api/internal/v1/reseller/registration/presentation"
	assert.Equal(t, http.StatusUnauthorized, request("POST", readPath, megaToken, ip).Code)
	assert.Equal(t, http.StatusBadRequest, request("POST", readPath, registrationToken, ip).Code)
	assert.Equal(t, http.StatusBadRequest, request("POST", readPath, registrationToken, ip).Code)
	limited := request("POST", readPath, registrationToken, "192.0.2.188")
	require.Equal(t, http.StatusTooManyRequests, limited.Code)
	assert.Equal(t, "60", limited.Header().Get("Retry-After"))
	assert.Contains(t, limited.Body.String(), "reseller_rate_limited")
	const writePath = "/api/internal/v1/reseller/registration/customers/register"
	assert.Equal(t, http.StatusBadRequest, request("POST", writePath, registrationToken, ip).Code)
	assert.Equal(t, http.StatusTooManyRequests, request("POST", writePath, registrationToken, ip).Code)
	assert.Equal(t, http.StatusBadRequest, request("POST", "/api/internal/v1/platform/resellers", megaToken, ip).Code)
	assert.Equal(t, http.StatusTooManyRequests, request("PUT", "/api/internal/v1/platform/resellers/1/presentation", megaToken, ip).Code)
	assert.Equal(t, http.StatusUnauthorized, request("POST", writePath, "forged-token", ip).Code)
}
