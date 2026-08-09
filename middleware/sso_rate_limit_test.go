package middleware

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// buildSSOLimitedRouter mounts handler behind limiter, with a stand-in for
// SSOAuth() that just sets "id" the way the real one does after resolving the
// SSO subject.
func buildSSOLimitedRouter(limiter func(c *gin.Context)) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/sso/token/:id/key", func(c *gin.Context) {
		uid := c.Request.Header.Get("X-Test-User-Id")
		var parsed int
		fmt.Sscanf(uid, "%d", &parsed)
		c.Set("id", parsed)
	}, limiter, func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"success": true})
	})
	return router
}

// callAsUser fires one request as userId, always from the SAME client IP —
// which is the whole point: in production every /api/sso caller shares one IP.
func callAsUser(router *gin.Engine, userId int) int {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/sso/token/1/key", nil)
	request.Header.Set("X-Test-User-Id", fmt.Sprintf("%d", userId))
	request.RemoteAddr = "192.168.16.1:12345"
	router.ServeHTTP(recorder, request)
	return recorder.Code
}

// withCriticalRateLimit pins the critical-limit knobs for one test and restores
// them afterwards (they are process-wide globals). Redis is forced off so the
// limiter takes its in-memory path; both paths key on the same string, so the
// isolation being asserted here is the same one that applies in production.
func withCriticalRateLimit(t *testing.T, num int, duration int64) {
	t.Helper()
	prevEnable, prevNum, prevDuration, prevRedis :=
		common.CriticalRateLimitEnable, common.CriticalRateLimitNum,
		common.CriticalRateLimitDuration, common.RedisEnabled
	common.CriticalRateLimitEnable, common.CriticalRateLimitNum,
		common.CriticalRateLimitDuration, common.RedisEnabled =
		true, num, duration, false
	t.Cleanup(func() {
		common.CriticalRateLimitEnable, common.CriticalRateLimitNum,
			common.CriticalRateLimitDuration, common.RedisEnabled =
			prevEnable, prevNum, prevDuration, prevRedis
	})
}

// The regression this change is about: two different end users behind one
// shared client IP must not consume each other's budget.
func TestSSOCriticalRateLimitIsolatesUsersBehindSharedIP(t *testing.T) {
	withCriticalRateLimit(t, 2, 60)
	router := buildSSOLimitedRouter(SSOCriticalRateLimit())

	// user 1001 burns its entire budget
	require.Equal(t, http.StatusOK, callAsUser(router, 1001))
	require.Equal(t, http.StatusOK, callAsUser(router, 1001))
	require.Equal(t, http.StatusTooManyRequests, callAsUser(router, 1001),
		"same user over budget should be limited")

	// user 1002, same IP, must still be served
	require.Equal(t, http.StatusOK, callAsUser(router, 1002),
		"a different end user behind the same IP must not be blocked")
	require.Equal(t, http.StatusOK, callAsUser(router, 1002))
	require.Equal(t, http.StatusTooManyRequests, callAsUser(router, 1002),
		"per-user ceiling still applies to the second user")
}

// Guards the counterfactual: the old IP-keyed limiter does collapse into a
// single shared quota under these exact conditions. Documents why the sso
// routes had to move off it.
func TestCriticalRateLimitCollapsesToSharedQuotaBehindSharedIP(t *testing.T) {
	withCriticalRateLimit(t, 2, 60)
	router := buildSSOLimitedRouter(CriticalRateLimit())

	require.Equal(t, http.StatusOK, callAsUser(router, 2001))
	require.Equal(t, http.StatusOK, callAsUser(router, 2002))
	require.Equal(t, http.StatusTooManyRequests, callAsUser(router, 2003),
		"IP-keyed limiting starves an unrelated user once the shared budget is gone")
}

// A request that somehow reaches the limiter without authentication must not
// fall back to an unlimited or shared bucket.
func TestSSOCriticalRateLimitRejectsUnauthenticatedRequest(t *testing.T) {
	withCriticalRateLimit(t, 2, 60)
	router := buildSSOLimitedRouter(SSOCriticalRateLimit())

	require.Equal(t, http.StatusUnauthorized, callAsUser(router, 0))
}

// The kill switch keeps working for the sso variant.
func TestSSOCriticalRateLimitRespectsDisableFlag(t *testing.T) {
	withCriticalRateLimit(t, 1, 60)
	common.CriticalRateLimitEnable = false
	router := buildSSOLimitedRouter(SSOCriticalRateLimit())

	require.Equal(t, http.StatusOK, callAsUser(router, 3001))
	require.Equal(t, http.StatusOK, callAsUser(router, 3001))
}
