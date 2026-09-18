package router

import (
	"fmt"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/mozia_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConditionalRoutingLocksChannelAndPreservesRequest(t *testing.T) {
	require.NoError(t, i18n.Init())
	t.Setenv("SQL_DSN", "local")
	originalDB, wasMaster, sqlitePath := model.DB, common.IsMasterNode, common.SQLitePath
	common.IsMasterNode, common.SQLitePath = false, "file:conditional-route-init?mode=memory&cache=shared"
	require.NoError(t, model.InitDB())
	common.IsMasterNode, common.SQLitePath = wasMaster, sqlitePath
	initDB, err := model.DB.DB()
	require.NoError(t, err)
	require.NoError(t, initDB.Close())
	model.DB = originalDB
	_, db, _ := setupResellerAdminTest(t)
	require.NoError(t, db.AutoMigrate(&model.ResellerCustomer{}, &model.Channel{}, &model.Ability{}))
	original := mozia_setting.UserModelRedirects2JSONString()
	cache := common.MemoryCacheEnabled
	t.Cleanup(func() {
		require.NoError(t, mozia_setting.UpdateUserModelRedirectsByJSONString(original))
		common.MemoryCacheEnabled = cache
		if cache {
			model.InitChannelCache()
		}
	})
	user := model.User{Username: "routing-test", Group: "default"}
	require.NoError(t, db.Create(&user).Error)
	channels := []model.Channel{
		{Id: 1, Type: constant.ChannelTypeOpenAI, Name: "text", Key: "test-key", Models: "k3,k2", Group: "default", Status: common.ChannelStatusEnabled, Priority: common.GetPointer(int64(100))},
		{Id: 2, Type: constant.ChannelTypeOpenAI, Name: "video", Key: "test-key", Models: "k3,k2", Group: "default", Status: common.ChannelStatusEnabled},
	}
	require.NoError(t, db.Create(&channels).Error)
	metadataResponse := httptest.NewRecorder()
	metadata, _ := gin.CreateTestContext(metadataResponse)
	metadata.Request = httptest.NewRequest("GET", "/api/mozia/user-model-redirect/targets", nil)
	controller.GetMoziaRoutingTargets(metadata)
	var targets struct {
		Success bool             `json:"success"`
		Data    []map[string]any `json:"data"`
	}
	require.NoError(t, common.Unmarshal(metadataResponse.Body.Bytes(), &targets))
	require.True(t, targets.Success)
	require.Len(t, targets.Data, 2)
	for _, target := range targets.Data {
		assert.Len(t, target, 4, "only ID, name, models and status may leave the server")
		assert.NotContains(t, target, "key")
	}
	assert.ErrorContains(t, service.UpsertMoziaUserModelRedirect(mozia_setting.UserModelRedirect{ID: "bad-target", AllUsers: true, SourceModel: "k3", TargetModel: "not-configured", TargetChannelId: 2}), "not configured")
	require.NoError(t, db.Create(&[]model.Ability{{Group: "default", Model: "k3", ChannelId: 1, Enabled: true, Priority: common.GetPointer(int64(100))}, {Group: "default", Model: "k3", ChannelId: 2, Enabled: true}, {Group: "default", Model: "k2", ChannelId: 2, Enabled: true}}).Error)
	require.NoError(t, mozia_setting.UpdateUserModelRedirectsByJSONString(`{"video":{"id":"video","all_users":true,"source_model":"k3","target_model":"k3","target_channel_id":2,"only_thinking_disabled":false,"conditions":[{"operator":"has_video"}]}}`))
	const body = `{"model":"k3","stream":true,"messages":[{"role":"user","content":[{"type":"video_url","video_url":{"url":"https://example.com/test.mp4"}}]}]}`
	allowed := map[string]bool{"k3": true}
	group := "default"
	specified := ""
	engine := gin.New()
	engine.Use(func(c *gin.Context) {
		c.Set("id", user.Id)
		common.SetContextKey(c, constant.ContextKeyUsingGroup, group)
		common.SetContextKey(c, constant.ContextKeyUserGroup, "default")
		common.SetContextKey(c, constant.ContextKeyTokenModelLimitEnabled, true)
		common.SetContextKey(c, constant.ContextKeyTokenModelLimit, allowed)
		if specified != "" {
			common.SetContextKey(c, constant.ContextKeyTokenSpecificChannelId, specified)
		}
	})
	engine.POST("/v1/chat/completions", middleware.Distribute(), func(c *gin.Context) {
		if common.GetContextKeyString(c, constant.ContextKeyConditionalRouteID) == "" {
			assert.Equal(t, 1, c.GetInt("channel_id"))
			c.Status(204)
			return
		}
		assert.Equal(t, "k3", c.GetString("original_model"))
		assert.Equal(t, 2, c.GetInt("channel_id"))
		assert.Equal(t, "2", c.GetString("specific_channel_id"))
		assert.Equal(t, "video", common.GetContextKeyString(c, constant.ContextKeyConditionalRouteID))
		actual, err := io.ReadAll(c.Request.Body)
		require.NoError(t, err)
		assert.JSONEq(t, body, string(actual))
		channel, _, err := service.CacheGetRandomSatisfiedChannel(&service.RetryParam{Ctx: c, TokenGroup: "default", ModelName: "k3", RequestPath: c.Request.URL.Path, Retry: common.GetPointer(9)})
		require.NoError(t, err)
		require.NotNil(t, channel)
		assert.Equal(t, 2, channel.Id)
		c.Status(204)
	})
	request := func(requestBody string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(requestBody))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, r)
		return w
	}
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("memory_cache=%t", enabled), func(t *testing.T) {
			common.MemoryCacheEnabled = enabled
			if enabled {
				model.InitChannelCache()
			}
			w := request(`{"model":"k3","messages":[{"content":"text"}]}`)
			require.Equal(t, 204, w.Code, w.Body.String())
			w = request(body)
			require.Equal(t, 204, w.Code, w.Body.String())
			group = "private"
			w = request(body)
			assert.Equal(t, 503, w.Code, w.Body.String())
			group = "default"
			specified = "1"
			w = request(body)
			assert.Equal(t, 403, w.Code, w.Body.String())
			specified = ""
			require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 2).Update("status", common.ChannelStatusManuallyDisabled).Error)
			if enabled {
				model.InitChannelCache()
			}
			w = request(body)
			assert.Equal(t, 503, w.Code, w.Body.String())
			require.NoError(t, db.Model(&model.Channel{}).Where("id = ?", 2).Update("status", common.ChannelStatusEnabled).Error)
		})
	}
	require.NoError(t, mozia_setting.UpdateUserModelRedirectsByJSONString(`{"video":{"id":"video","all_users":true,"source_model":"k3","target_model":"k2","target_channel_id":2,"only_thinking_disabled":false}}`))
	common.MemoryCacheEnabled = false
	w := request(body)
	assert.Equal(t, 403, w.Code, w.Body.String())
}
