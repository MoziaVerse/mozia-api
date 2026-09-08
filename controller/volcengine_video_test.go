package controller

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestVolcengineVideoQueryListAndDelete(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.Channel{}))
	originalDB, originalCache := model.DB, common.MemoryCacheEnabled
	model.DB, common.MemoryCacheEnabled = db, false
	t.Cleanup(func() {
		model.DB, common.MemoryCacheEnabled = originalDB, originalCache
		_ = sqlDB.Close()
	})
	service.InitHttpClient()
	now := time.Now().Unix()
	states := map[string]string{"cgt-own": "running", "cgt-second": "queued", "cgt-foreign": "running", "cgt-done": "succeeded"}
	deleted := make(map[string]bool)
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		assert.Equal(t, "Bearer saved-key", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		id := strings.TrimPrefix(r.URL.Path, constant.VolcengineVideoTaskPath+"/")
		if r.URL.Path == constant.VolcengineVideoTaskPath {
			assert.Equal(t, "500", r.URL.Query().Get("page_size"))
			assert.Equal(t, "1", r.URL.Query().Get("page_num"))
			ids := r.URL.Query()["filter.task_ids"]
			assert.NotContains(t, ids, "cgt-foreign")
			rawItems := make([]map[string]any, 0)
			// Deliberately include a foreign task even though it was not requested.
			ids = append(ids, "cgt-foreign")
			for _, taskID := range ids {
				if deleted[taskID] {
					continue
				}
				rawItems = append(rawItems, map[string]any{"id": taskID, "status": states[taskID], "created_at": now, "model": "upstream-model"})
			}
			data, marshalErr := common.Marshal(map[string]any{"items": rawItems, "total": len(rawItems)})
			assert.NoError(t, marshalErr)
			_, _ = w.Write(data)
			return
		}
		if id == "cgt-error" {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"error":{"code":"Throttling","message":"slow down","param":"model"}}`)
			return
		}
		if deleted[id] {
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"error":{"code":"NotFound","message":"deleted"}}`)
			return
		}
		if r.Method == http.MethodDelete {
			var stored model.Task
			assert.NoError(t, db.Where("task_id = ?", id).First(&stored).Error)
			if states[id] == "succeeded" {
				assert.Equal(t, model.TaskStatus(model.TaskStatusSuccess), stored.Status, "persist terminal status before deleting usage")
				deleted[id] = true
			} else {
				states[id] = "cancelled"
			}
			_, _ = io.WriteString(w, "{}")
			return
		}
		_, _ = io.WriteString(w, `{"id":"`+id+`","status":"`+states[id]+`","model":"upstream-model","seed":9007199254740993,"generate_audio":false,"content":{"video_url":"https://example.com/v.mp4","last_frame_url":"https://example.com/f.png"},"usage":{"total_tokens":500},"future_field":{"n":0}}`)
	}))
	defer server.Close()
	baseURL := server.URL + "/api/v3"
	require.NoError(t, db.Create(&model.Channel{Id: 1, Type: constant.ChannelTypeMoziaArtsapi, Key: "rotated-key", BaseURL: &baseURL}).Error)
	for _, id := range []string{"cgt-own", "cgt-second", "cgt-foreign", "cgt-done", "cgt-error"} {
		userID := 42
		if id == "cgt-foreign" {
			userID = 99
		}
		task := &model.Task{
			TaskID: id, UserId: userID, ChannelId: 1, Platform: constant.TaskPlatformVolcengineVideo,
			Status: model.TaskStatusSubmitted, SubmitTime: now,
			Properties:  model.Properties{OriginModelName: "public-model"},
			PrivateData: model.TaskPrivateData{Key: "saved-key", BillingContext: &model.TaskBillingContext{PerCallBilling: true}},
		}
		require.NoError(t, db.Create(task).Error)
	}
	r := gin.New()
	limitedModel := ""
	r.Use(func(c *gin.Context) {
		c.Set("id", 42)
		if limitedModel != "" {
			common.SetContextKey(c, constant.ContextKeyTokenModelLimitEnabled, true)
			common.SetContextKey(c, constant.ContextKeyTokenModelLimit, map[string]bool{limitedModel: true})
		}
	})
	r.GET(constant.VolcengineVideoTaskPath, RelayVolcengineVideoTaskList)
	r.GET(constant.VolcengineVideoTaskPath+"/:task_id", RelayVolcengineVideoTaskFetch)
	r.DELETE(constant.VolcengineVideoTaskPath+"/:task_id", RelayVolcengineVideoTaskFetch)

	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(method, constant.VolcengineVideoTaskPath+"/cgt-foreign", nil))
		assert.Equal(t, http.StatusNotFound, rec.Code)
		assert.Zero(t, calls.Load(), "authorization must happen before upstream access")
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, constant.VolcengineVideoTaskPath+"/cgt-own", nil))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"seed":9007199254740993`)
	assert.Contains(t, rec.Body.String(), `"last_frame_url"`)
	var stored model.Task
	require.NoError(t, db.Where("task_id = ?", "cgt-own").First(&stored).Error)
	assert.Equal(t, rec.Body.Bytes(), []byte(stored.Data))
	assert.Equal(t, model.TaskStatus(model.TaskStatusInProgress), stored.Status)

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, constant.VolcengineVideoTaskPath+"?page_num=2&page_size=1&filter.task_ids=cgt-own&filter.task_ids=cgt-second&filter.task_ids=cgt-foreign", nil))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var list struct {
		Items []json.RawMessage `json:"items"`
		Total int               `json:"total"`
	}
	require.NoError(t, common.Unmarshal(rec.Body.Bytes(), &list))
	assert.Equal(t, 2, list.Total)
	require.Len(t, list.Items, 1)
	assert.NotContains(t, rec.Body.String(), "cgt-foreign")

	for _, id := range []string{"cgt-own", "cgt-done"} {
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, constant.VolcengineVideoTaskPath+"/"+id, nil))
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		assert.JSONEq(t, "{}", rec.Body.String())
	}
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, constant.VolcengineVideoTaskPath+"/cgt-own", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"status":"cancelled"`)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, constant.VolcengineVideoTaskPath+"/cgt-done", nil))
	assert.Equal(t, http.StatusNotFound, rec.Code)

	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, constant.VolcengineVideoTaskPath+"/cgt-error", nil))
	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
	assert.JSONEq(t, `{"error":{"code":"Throttling","message":"slow down","param":"model"}}`, rec.Body.String())

	for _, query := range []string{"page_num=0", "page_size=501", "filter.status=invalid", "filter.service_tier=invalid"} {
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, constant.VolcengineVideoTaskPath+"?"+query, nil))
		assert.Equal(t, http.StatusBadRequest, rec.Code, query)
	}
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, constant.VolcengineVideoTaskPath+"?filter.task_ids=cgt-foreign", nil))
	assert.JSONEq(t, `{"items":[],"total":0}`, rec.Body.String())
	limitedModel = "another-model"
	before := calls.Load()
	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(method, constant.VolcengineVideoTaskPath+"/cgt-own", nil))
		assert.Equal(t, http.StatusForbidden, rec.Code)
	}
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, constant.VolcengineVideoTaskPath, nil))
	assert.JSONEq(t, `{"items":[],"total":0}`, rec.Body.String())
	assert.Equal(t, before, calls.Load(), "token model restrictions apply before upstream access")
}
