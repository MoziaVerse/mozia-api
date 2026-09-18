package router

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
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
	assert.True(t, routes["HEAD /v1/video/generations/:task_id/content"])
	assert.True(t, routes["GET /v1/video/generations/:task_id/content/:filename"])
	assert.True(t, routes["HEAD /v1/video/generations/:task_id/content/:filename"])
	assert.True(t, routes["POST /v1/videos"])
	assert.True(t, routes["GET /v1/videos/:task_id"])
	assert.True(t, routes["GET /v1/videos/:task_id/content"])
	assert.True(t, routes["HEAD /v1/videos/:task_id/content"])
	assert.True(t, routes["GET /v1/videos/:task_id/content/:filename"])
	assert.True(t, routes["HEAD /v1/videos/:task_id/content/:filename"])
}

func TestVideoTaskFetchWithDistribution(t *testing.T) {
	require.NoError(t, i18n.Init())
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/tasks.db"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	previousDB := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = previousDB
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.Channel{}))
	require.NoError(t, db.Create(&model.Channel{Id: 1, Type: constant.ChannelTypeMoziaH3}).Error)
	require.NoError(t, db.Create(&model.Channel{
		Id: 2, Type: constant.ChannelTypeMoziaH3, Status: common.ChannelStatusEnabled,
		ChannelInfo: model.ChannelInfo{IsMultiKey: true},
	}).Error)
	task := model.Task{
		TaskID: "task_existing", UserId: 42, ChannelId: 1,
		Platform:   constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeMoziaH3)),
		Status:     model.TaskStatusQueued,
		Properties: model.Properties{OriginModelName: "public/h3"},
		Data:       []byte(`{"status":"queued"}`),
	}
	require.NoError(t, db.Create(&task).Error)
	for _, route := range []string{"/v1/video/generations/:task_id", "/v1/videos/:task_id"} {
		for _, tc := range []struct {
			name, allowedModel, specificChannel, errorText string
			userID, status                                 int
		}{
			{name: "owner", userID: 42, status: http.StatusOK},
			{name: "allowed model", userID: 42, allowedModel: "public/h3", status: http.StatusOK},
			{name: "other user", userID: 99, status: http.StatusBadRequest, errorText: "task_not_exist"},
			{name: "blocked model", userID: 42, allowedModel: "other-model", status: http.StatusForbidden},
			{name: "selected channel has no keys", userID: 42, specificChannel: "2", status: http.StatusInternalServerError, errorText: "no keys available"},
		} {
			t.Run(route+"/"+tc.name, func(t *testing.T) {
				r := gin.New()
				r.GET(route, func(c *gin.Context) {
					c.Set("id", tc.userID)
					if tc.allowedModel != "" {
						common.SetContextKey(c, constant.ContextKeyTokenModelLimitEnabled, true)
						common.SetContextKey(c, constant.ContextKeyTokenModelLimit, map[string]bool{tc.allowedModel: true})
					}
					if tc.specificChannel != "" {
						common.SetContextKey(c, constant.ContextKeyTokenSpecificChannelId, tc.specificChannel)
					}
				}, middleware.Distribute(), controller.RelayTaskFetch)
				response := httptest.NewRecorder()
				path := route[:len(route)-len(":task_id")] + task.TaskID
				r.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
				require.Equal(t, tc.status, response.Code, response.Body.String())
				if tc.status == http.StatusOK {
					var result struct{ ID, Model, Status string }
					require.NoError(t, common.Unmarshal(response.Body.Bytes(), &result))
					assert.Equal(t, task.TaskID, result.ID)
					assert.Equal(t, "public/h3", result.Model)
					assert.Equal(t, "queued", result.Status)
				} else if tc.errorText != "" {
					assert.Contains(t, response.Body.String(), tc.errorText)
				}
			})
		}
	}
}

func TestTaskRoutesDeferChannelSelection(t *testing.T) {
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/suno/fetch/task_existing"},
		{http.MethodPost, "/suno/fetch"},
		{http.MethodGet, "/mj/task/task_existing/fetch"},
		{http.MethodPost, "/mj/task/list-by-condition"},
		{http.MethodPost, "/v1/videos/task_existing/remix"},
	} {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			r := gin.New()
			r.Handle(tc.method, tc.path, middleware.Distribute(), func(c *gin.Context) {
				c.Status(http.StatusNoContent)
			})
			response := httptest.NewRecorder()
			r.ServeHTTP(response, httptest.NewRequest(tc.method, tc.path, nil))
			assert.Equal(t, http.StatusNoContent, response.Code, response.Body.String())
		})
	}
}
