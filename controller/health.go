package controller

import (
	"context"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/metrics"
	"github.com/gin-gonic/gin"
)

var (
	instanceDraining atomic.Bool

	// Overridable probe hooks for deterministic unit testing.
	probeMainDB  = model.PingDBContext
	probeLogDB   = model.PingLogDBContext
	probeRedis   = common.PingRedisContext
	isLogDBSplit = func() bool {
		return model.LOG_DB != nil && model.LOG_DB != model.DB
	}
	isRedisEnabled = func() bool {
		return common.RedisEnabled
	}
)

// SetDraining switches the instance into or out of graceful shutdown draining.
// While draining, /readyz returns 503 immediately so reverse proxies stop
// sending new traffic while in-flight requests finish.
func SetDraining(draining bool) {
	instanceDraining.Store(draining)
	metrics.SetReady(!draining)
}

// IsDraining reports whether this instance is currently draining traffic.
func IsDraining() bool {
	return instanceDraining.Load()
}

// Healthz is a lightweight liveness probe confirming the HTTP server process is alive.
func Healthz(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"node":    common.NodeName,
		"version": common.Version,
	})
}

// Readyz verifies that the instance is not draining and can actively reach its
// primary database, log database (if split), and Redis (if enabled) within 2s.
func Readyz(c *gin.Context) {
	checks := make(map[string]string, 4)

	if IsDraining() {
		checks["lifecycle"] = "draining"
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status":  "draining",
			"ready":   false,
			"node":    common.NodeName,
			"version": common.Version,
			"checks":  checks,
		})
		return
	}
	checks["lifecycle"] = "ok"

	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()

	ready := true

	if err := probeMainDB(ctx); err != nil {
		ready = false
		checks["db"] = "error: database unreachable"
	} else {
		checks["db"] = "ok"
	}

	if isLogDBSplit() {
		if err := probeLogDB(ctx); err != nil {
			ready = false
			checks["log_db"] = "error: log database unreachable"
		} else {
			checks["log_db"] = "ok"
		}
	}

	if isRedisEnabled() {
		if err := probeRedis(ctx); err != nil {
			ready = false
			checks["redis"] = "error: redis unreachable"
		} else {
			checks["redis"] = "ok"
		}
	}

	statusCode := http.StatusOK
	statusText := "ok"
	if !ready {
		statusCode = http.StatusServiceUnavailable
		statusText = "unready"
	}
	c.JSON(statusCode, gin.H{
		"status":             statusText,
		"ready":              ready,
		"node":               common.NodeName,
		"version":            common.Version,
		"active_connections": middleware.GetStats().ActiveConnections,
		"checks":             checks,
	})
}
