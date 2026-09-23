package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"gorm.io/gorm"
)

func TestRequestOutcomeCapturesFinalResultWithoutChangingBilling(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&model.Log{}))
	previousDB, previousConsume, previousErrors := model.LOG_DB, common.LogConsumeEnabled, constant.ErrorLogEnabled
	model.LOG_DB, common.LogConsumeEnabled, constant.ErrorLogEnabled = db, true, true
	t.Cleanup(func() {
		model.LOG_DB, common.LogConsumeEnabled, constant.ErrorLogEnabled = previousDB, previousConsume, previousErrors
		sqlDB, err := db.DB()
		require.NoError(t, err)
		require.NoError(t, sqlDB.Close())
	})

	for _, tc := range []struct {
		name    string
		handler gin.HandlerFunc
		outcome string
		status  int
	}{
		{"routing rejection", func(c *gin.Context) {
			abortWithOpenAiMessage(c, http.StatusServiceUnavailable, "channel unavailable", types.ErrorCodeGetChannelFailed)
		}, "error", 503},
		{"retry recovered", func(c *gin.Context) {
			require.NoError(t, db.Create(&model.Log{Type: model.LogTypeError, RequestId: c.GetString(common.RequestIdKey), UserId: 42}).Error)
			model.ObserveCallConsumption(c, model.RecordConsumeLogParams{})
			c.Status(200)
		}, "success", 200},
		{"stream failed after HTTP 200", func(c *gin.Context) {
			c.Status(200)
			c.Writer.Flush()
			// Snapshot must still be recorded when consumption logging is disabled.
			common.LogConsumeEnabled = false
			defer func() { common.LogConsumeEnabled = true }()
			model.RecordConsumeLog(c, 42, model.RecordConsumeLogParams{IsStream: true, Other: map[string]interface{}{
				"stream_status": map[string]interface{}{"status": "error", "end_reason": "scanner_error"},
			}})
		}, "error", 200},
		{"error after response started", func(c *gin.Context) {
			c.Status(200)
			c.Writer.Flush()
			c.Set("call_analytics_error_status", 502)
			c.Set("call_analytics_error_code", "bad_response")
		}, "error", 200},
		{"client cancelled", func(c *gin.Context) {
			model.ObserveCallConsumption(c, model.RecordConsumeLogParams{IsStream: true, Other: map[string]interface{}{
				"stream_status": map[string]interface{}{"status": "error", "end_reason": "client_gone"},
			}})
		}, "cancelled", 200},
		{"unconfirmed stream", func(c *gin.Context) {
			model.ObserveCallConsumption(c, model.RecordConsumeLogParams{IsStream: true})
		}, "unknown", 200},
		{"panic recovered by outer handler", func(c *gin.Context) { panic("test") }, "error", 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := gin.New()
			router.Use(gin.Recovery(), RequestId(), RequestOutcome())
			router.POST("/v1/chat/completions", func(c *gin.Context) {
				c.Set("id", 42)
				c.Set("call_analytics_requested_model", "moonshotai/kimi-k3")
				c.Set("call_analytics_effective_model", "moonshotai/kimi-k2.6")
				c.Set("token_key", "secret-must-not-be-logged")
				tc.handler(c)
			})
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest("POST", "/v1/chat/completions", nil))
			var logs []model.Log
			require.NoError(t, db.Where("request_id = ? AND type = ?", response.Header().Get(common.RequestIdKey), model.LogTypeRequestOutcome).Find(&logs).Error)
			require.Len(t, logs, 1)
			assert.Equal(t, tc.outcome, gjson.Get(logs[0].Other, "request_outcome.status").String())
			assert.EqualValues(t, tc.status, gjson.Get(logs[0].Other, "request_outcome.status_code").Int())
			assert.Equal(t, "moonshotai/kimi-k3", logs[0].ModelName)
			assert.Equal(t, "moonshotai/kimi-k2.6", gjson.Get(logs[0].Other, "effective_model").String())
			assert.Zero(t, logs[0].Quota)
			assert.Zero(t, logs[0].PromptTokens)
			assert.Zero(t, logs[0].CompletionTokens)
			assert.NotContains(t, logs[0].Other, "secret-must-not-be-logged")
			assert.NotContains(t, logs[0].Other, "request_body")
		})
	}

	// Existing log readers must not expose private terminal metadata or duplicates.
	logs, _, err := model.GetUserLogs(42, 0, 0, 0, "", "", 0, 50, "", "", "")
	require.NoError(t, err)
	require.Len(t, logs, 1)
	assert.Equal(t, model.LogTypeError, logs[0].Type)
	logs, _, err = model.GetUserLogs(42, model.LogTypeRequestOutcome, 0, 0, "", "", 0, 50, "", "", "")
	require.NoError(t, err)
	assert.Empty(t, logs)
	logs, err = model.GetLogByTokenId(0)
	require.NoError(t, err)
	require.Len(t, logs, 1)
	assert.Equal(t, model.LogTypeError, logs[0].Type)

	t.Run("unidentified and non-text calls omitted", func(t *testing.T) {
		for _, path := range []string{"/v1/chat/completions", "/v1/images/generations"} {
			router := gin.New()
			router.Use(RequestId(), RequestOutcome())
			router.POST(path, func(c *gin.Context) {
				if path == "/v1/images/generations" {
					c.Set("id", 42)
				}
				c.Status(401)
			})
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest("POST", path, nil))
			var count int64
			require.NoError(t, db.Model(&model.Log{}).Where("request_id = ?", response.Header().Get(common.RequestIdKey)).Count(&count).Error)
			assert.Zero(t, count)
		}
	})
}
