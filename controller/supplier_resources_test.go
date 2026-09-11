package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSupplierResourceHTTPContract(t *testing.T) {
	db := setupMaterialControllerTestDB(t)
	oldRedis := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = oldRedis })
	require.NoError(t, db.AutoMigrate(&model.Option{}, &model.Supplier{}, &model.SupplierPool{}, &model.SupplierBinding{}, &model.SupplierRoutingRule{}, &model.RoutingRevision{}, &model.SupplierAttempt{}, &model.Log{}, &model.User{}))
	require.NoError(t, db.Create(&model.User{Id: 7, Username: "resource-review", Group: "default"}).Error)
	oldLog := model.LOG_DB
	model.LOG_DB = db
	t.Cleanup(func() { model.LOG_DB = oldLog })
	require.NoError(t, model.MigrateSupplierResources())
	supplier := model.Supplier{Name: "A", SupplierResourceMeta: model.SupplierResourceMeta{Version: 1}}
	require.NoError(t, db.Create(&supplier).Error)
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.Use(func(c *gin.Context) { c.Set("id", 7) })
	engine.GET("/suppliers/:id", SupplierResourceDetail("supplier"))
	engine.PATCH("/suppliers/:id", SupplierResourceWrite("supplier"))
	engine.PUT("/legacy", PublishSupplierRouting)
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest("GET", "/suppliers/1", nil))
	require.Equal(t, 200, recorder.Code)
	etag := recorder.Header().Get("ETag")
	assert.Equal(t, `"supplier-1-v1"`, etag)
	for _, test := range []struct {
		body, match, code string
		status            int
	}{
		{`{"name":"A2"}`, "", "precondition_required", 428},
		{`{"name":"A2"}`, `"supplier-1-v0"`, "resource_version_conflict", 412},
		{`{"name":""}`, etag, "validation_failed", 422},
		{`{"name":"A2","contact":""}`, etag, "", 200},
		{`{"name":"A3"}`, etag, "resource_version_conflict", 412},
	} {
		request := httptest.NewRequest(http.MethodPatch, "/suppliers/1", strings.NewReader(test.body))
		request.Header.Set("If-Match", test.match)
		recorder := httptest.NewRecorder()
		engine.ServeHTTP(recorder, request)
		require.Equal(t, test.status, recorder.Code, recorder.Body.String())
		var response struct {
			Success bool   `json:"success"`
			Code    string `json:"code"`
			Data    struct {
				Application string `json:"application"`
			} `json:"data"`
		}
		require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
		assert.Equal(t, test.code, response.Code)
		if test.status == 200 {
			assert.True(t, response.Success)
			assert.Equal(t, "not_required", response.Data.Application)
			assert.Equal(t, `"supplier-1-v2"`, recorder.Header().Get("ETag"))
		}
	}
	recorder = httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/legacy", strings.NewReader(`{}`)))
	assert.Equal(t, 410, recorder.Code)
}
