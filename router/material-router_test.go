package router

import (
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMaterialRoutesExposeOnlyProviderNeutralPaths(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()

	SetRelayRouter(engine)

	routes := engine.Routes()
	require.NotEmpty(t, routes)

	registered := make(map[string]bool, len(routes))
	for _, route := range routes {
		registered[route.Method+" "+route.Path] = true
		assert.NotContains(t, strings.ToLower(route.Path), "cool")
	}

	assert.True(t, registered[http.MethodPost+" /v1/sd/upload"])
	assert.True(t, registered[http.MethodPost+" /v1/sd/upload_url"])
	assert.False(t, registered[http.MethodPost+" /v1/materials"])
	assert.False(t, registered[http.MethodPost+" /v1/materials/import"])
}
