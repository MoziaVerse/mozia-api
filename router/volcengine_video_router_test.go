package router

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVolcengineVideoNativeRouterContract(t *testing.T) {
	_, db, _ := setupResellerAdminTest(t)
	require.NoError(t, db.AutoMigrate(
		&model.Task{}, &model.Channel{}, &model.Ability{}, &model.Token{}, &model.Model{},
		&model.ResellerCustomer{}, &model.MoziaModelQuotaPolicy{}, &model.Log{},
		&model.MoziaWalletReservation{}, &model.MoziaWalletTransaction{}, &model.UserSubscription{},
	))
	oldLogDB, oldCache, oldRedis, oldBatch := model.LOG_DB, common.MemoryCacheEnabled, common.RedisEnabled, common.BatchUpdateEnabled
	oldMainType, oldLogType := common.MainDatabaseType(), common.LogDatabaseType()
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.DatabaseTypeSQLite)
	t.Setenv("LOG_SQL_DSN", "")
	require.NoError(t, model.InitLogDB())
	model.LOG_DB, common.MemoryCacheEnabled, common.RedisEnabled, common.BatchUpdateEnabled = db, false, false, false
	oldPrices := ratio_setting.ModelPrice2JSONString()
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"native-test-video":0}`))
	t.Cleanup(func() {
		model.LOG_DB, common.MemoryCacheEnabled, common.RedisEnabled, common.BatchUpdateEnabled = oldLogDB, oldCache, oldRedis, oldBatch
		common.SetDatabaseTypes(oldMainType, oldLogType)
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(oldPrices))
	})
	user := model.User{Username: "native-client", Status: common.UserStatusEnabled, Group: "default", Quota: 1000000}
	require.NoError(t, db.Create(&user).Error)
	token := model.Token{UserId: user.Id, Key: "nativeclienttoken", Status: common.TokenStatusEnabled, ExpiredTime: -1, RemainQuota: 1000000, Group: "default", ModelLimitsEnabled: true, ModelLimits: "native-test-video"}
	require.NoError(t, db.Create(&token).Error)
	var calls atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		assert.Equal(t, "Bearer native-upstream-key", r.Header.Get("Authorization"))
		assert.Equal(t, constant.VolcengineVideoTaskPath, r.URL.Path)
		assert.Equal(t, http.MethodPost, r.Method)
		data, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		assert.JSONEq(t, `{"model":"doubao-seedance-2-0","content":[{"type":"text","text":"a cat"}],"duration":-1,"generate_audio":false}`, string(data))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"cgt-route"}`)
	}))
	defer upstream.Close()
	service.InitHttpClient()
	baseURL, mapping := upstream.URL, `{"native-test-video":"doubao-seedance-2-0"}`
	channel := model.Channel{Type: constant.ChannelTypeMoziaArtsapi, Key: "native-upstream-key", BaseURL: &baseURL, ModelMapping: &mapping, Models: "native-test-video", Group: "default", Status: common.ChannelStatusEnabled}
	require.NoError(t, db.Create(&channel).Error)
	require.NoError(t, channel.AddAbilities(db))

	engine := gin.New()
	SetVideoRouter(engine)
	for _, endpoint := range []struct{ method, path string }{
		{http.MethodPost, constant.VolcengineVideoTaskPath},
		{http.MethodGet, constant.VolcengineVideoTaskPath},
		{http.MethodGet, constant.VolcengineVideoTaskPath + "/cgt-route"},
		{http.MethodDelete, constant.VolcengineVideoTaskPath + "/cgt-route"},
	} {
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, httptest.NewRequest(endpoint.method, endpoint.path, nil))
		assert.Equal(t, http.StatusUnauthorized, rec.Code, endpoint)
		assert.Contains(t, rec.Body.String(), "error")
	}
	assert.Zero(t, calls.Load())

	request := httptest.NewRequest(http.MethodPost, constant.VolcengineVideoTaskPath, strings.NewReader(`{"model":"native-test-video","content":[{"type":"text","text":"a cat"}],"duration":-1,"generate_audio":false}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+token.Key)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, request)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"id":"cgt-route"}`, rec.Body.String())
	task, exists, err := model.GetByTaskId(user.Id, "cgt-route")
	require.NoError(t, err)
	require.True(t, exists, "success is returned only after the task is persisted")
	assert.Equal(t, constant.TaskPlatformVolcengineVideo, task.Platform)
	assert.Equal(t, channel.Id, task.ChannelId)
	assert.Equal(t, "native-upstream-key", task.PrivateData.Key)
	assert.Equal(t, "native-test-video", task.Properties.OriginModelName)
	assert.Equal(t, "doubao-seedance-2-0", task.Properties.UpstreamModelName)
}
