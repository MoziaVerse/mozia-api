package controller

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHealthzAndReadyzProbes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	origMainDB := probeMainDB
	origLogDB := probeLogDB
	origRedis := probeRedis
	origLogSplit := isLogDBSplit
	origRedisEnabled := isRedisEnabled
	t.Cleanup(func() {
		probeMainDB = origMainDB
		probeLogDB = origLogDB
		probeRedis = origRedis
		isLogDBSplit = origLogSplit
		isRedisEnabled = origRedisEnabled
		SetDraining(false)
	})

	router := gin.New()
	router.GET("/healthz", Healthz)
	router.GET("/readyz", Readyz)

	tests := []struct {
		name           string
		draining       bool
		mainDBErr      error
		logDBSplit     bool
		logDBErr       error
		redisEnabled   bool
		redisErr       error
		expectedCode   int
		expectedStatus string
	}{
		{
			name:           "ready when all dependencies respond",
			redisEnabled:   true,
			expectedCode:   http.StatusOK,
			expectedStatus: "ok",
		},
		{
			name:           "draining immediately returns 503",
			draining:       true,
			expectedCode:   http.StatusServiceUnavailable,
			expectedStatus: "draining",
		},
		{
			name:           "main db unreachable returns 503",
			mainDBErr:      errors.New("dial tcp timeout"),
			redisEnabled:   true,
			expectedCode:   http.StatusServiceUnavailable,
			expectedStatus: "unready",
		},
		{
			name:           "split log db unreachable returns 503",
			logDBSplit:     true,
			logDBErr:       errors.New("log db down"),
			redisEnabled:   true,
			expectedCode:   http.StatusServiceUnavailable,
			expectedStatus: "unready",
		},
		{
			name:           "redis unreachable returns 503",
			redisEnabled:   true,
			redisErr:       errors.New("redis connection refused"),
			expectedCode:   http.StatusServiceUnavailable,
			expectedStatus: "unready",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			SetDraining(tc.draining)
			probeMainDB = func(context.Context) error { return tc.mainDBErr }
			isLogDBSplit = func() bool { return tc.logDBSplit }
			probeLogDB = func(context.Context) error { return tc.logDBErr }
			isRedisEnabled = func() bool { return tc.redisEnabled }
			probeRedis = func(context.Context) error { return tc.redisErr }

			// /healthz always returns 200 while HTTP server is alive
			wHealth := httptest.NewRecorder()
			router.ServeHTTP(wHealth, httptest.NewRequest(http.MethodGet, "/healthz", nil))
			assert.Equal(t, http.StatusOK, wHealth.Code)

			wReady := httptest.NewRecorder()
			router.ServeHTTP(wReady, httptest.NewRequest(http.MethodGet, "/readyz", nil))
			assert.Equal(t, tc.expectedCode, wReady.Code)

			var payload map[string]any
			require.NoError(t, common.Unmarshal(wReady.Body.Bytes(), &payload))
			assert.Equal(t, tc.expectedStatus, payload["status"])
		})
	}
}
