package router

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestVideoContentRouteAliases(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	SetVideoRouter(r)

	routes := map[string]bool{}
	for _, route := range r.Routes() {
		routes[route.Method+" "+route.Path] = true
	}

	assert.True(t, routes["POST /v1/video/generations"])
	assert.True(t, routes["GET /v1/video/generations/:task_id"])
	assert.True(t, routes["GET /v1/video/generations/:task_id/content"])
	assert.True(t, routes["POST /v1/videos"])
	assert.True(t, routes["GET /v1/videos/:task_id"])
	assert.True(t, routes["GET /v1/videos/:task_id/content"])
}
