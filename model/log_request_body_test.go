package model

import (
	"bytes"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCaptureRequestBodyLogRedactsSecretsAndInlineData(t *testing.T) {
	t.Setenv("REQUEST_BODY_LOG_USER_ID", "42")
	t.Setenv("REQUEST_BODY_LOG_UNTIL", time.Now().Add(time.Hour).Format(time.RFC3339))
	body := `{
		"model":"minimax/minimax-h3-ref2va",
		"prompt":"a lighthouse in a storm",
		"max_tokens":64,
		"api_key":"secret-key",
		"accessToken":"secret-token",
		"client_password":"secret-password",
		"reference_image":"https://cdn.example.com/ref.png?signature=secret#fragment",
		"image":"data:image/png;base64,AAAA"
	}`
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("id", 42)
	c.Request = httptest.NewRequest("POST", "/v1/video/generations", bytes.NewBufferString(body))
	c.Request.Header.Set("Content-Type", "application/json")

	CaptureRequestBodyLog(c)
	other := map[string]interface{}{}
	attachRequestBodyLog(c, other)

	requestBody, ok := other["request_body"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "minimax/minimax-h3-ref2va", requestBody["model"])
	assert.Equal(t, "a lighthouse in a storm", requestBody["prompt"])
	assert.Equal(t, float64(64), requestBody["max_tokens"])
	assert.Equal(t, "[REDACTED]", requestBody["api_key"])
	assert.Equal(t, "[REDACTED]", requestBody["accessToken"])
	assert.Equal(t, "[REDACTED]", requestBody["client_password"])
	assert.Equal(t, "https://cdn.example.com/ref.png?redacted", requestBody["reference_image"])
	assert.Contains(t, requestBody["image"], "[REDACTED inline data")
}

func TestCaptureRequestBodyLogOmitsOversizedBody(t *testing.T) {
	t.Setenv("REQUEST_BODY_LOG_USER_ID", "42")
	t.Setenv("REQUEST_BODY_LOG_UNTIL", time.Now().Add(time.Hour).Format(time.RFC3339))
	const size = 16*1024 + 1
	body := `{"prompt":"` + strings.Repeat("a", size-len(`{"prompt":""}`)) + `"}`
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("id", 42)
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	CaptureRequestBodyLog(c)
	other := map[string]interface{}{}
	attachRequestBodyLog(c, other)

	assert.NotContains(t, other, "request_body")
	requestBody, ok := other["request_summary"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "request body exceeds 16384 byte diagnostic limit", requestBody["_omitted"])
	assert.Equal(t, int64(len(body)), requestBody["_size_bytes"])
}

func TestCaptureRequestBodyLogCapturesBodyAtSizeLimit(t *testing.T) {
	t.Setenv("REQUEST_BODY_LOG_USER_ID", "42")
	t.Setenv("REQUEST_BODY_LOG_UNTIL", time.Now().Add(time.Hour).Format(time.RFC3339))
	const size = 16 * 1024
	body := `{"prompt":"` + strings.Repeat("a", size-len(`{"prompt":""}`)) + `"}`
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("id", 42)
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	CaptureRequestBodyLog(c)
	other := map[string]interface{}{}
	attachRequestBodyLog(c, other)

	requestBody, ok := other["request_body"].(map[string]any)
	require.True(t, ok)
	assert.Len(t, requestBody["prompt"], size-len(`{"prompt":""}`))
}

func TestFormatUserLogsRemovesAdminRequestBody(t *testing.T) {
	logs := []*Log{{
		Other: common.MapToJsonStr(map[string]interface{}{
			"request_body": map[string]any{"prompt": "private"},
			"model_price":  0.1,
		}),
	}}

	formatUserLogs(logs, 0)

	other, err := common.StrToMap(logs[0].Other)
	require.NoError(t, err)
	assert.NotContains(t, other, "request_body")
	assert.Equal(t, 0.1, other["model_price"])
}

func TestRequestBodyDiagnosticLimitAfterRedaction(t *testing.T) {
	t.Setenv("REQUEST_BODY_LOG_USER_ID", "42")
	t.Setenv("REQUEST_BODY_LOG_UNTIL", time.Now().Add(time.Hour).Format(time.RFC3339))
	body := `{"messages":[` + strings.TrimSuffix(strings.Repeat(`{"key":"a"},`, 1000), ",") + `]}`
	require.Less(t, len(body), int(requestBodyLogLimit))
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("id", 42)
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	CaptureRequestBodyLog(c)
	other := map[string]interface{}{}
	attachRequestBodyLog(c, other)
	assert.NotContains(t, other, "request_body")
	assert.Equal(t, "redacted request body exceeds diagnostic limit", other["request_summary"].(map[string]any)["_omitted"])
}

func TestRequestBodyLoggingDefaultsAndScope(t *testing.T) {
	for _, tc := range []struct{ name, userID, until string }{
		{"disabled", "", ""},
		{"missing expiry", "42", ""},
		{"invalid expiry", "42", "invalid"},
		{"expired", "42", "2020-01-01T00:00:00Z"},
		{"other user", "43", time.Now().Add(time.Hour).Format(time.RFC3339)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("REQUEST_BODY_LOG_USER_ID", tc.userID)
			t.Setenv("REQUEST_BODY_LOG_UNTIL", tc.until)
			body := `{"messages":[{"content":[{"type":"video_url","video_url":{"url":"data:video/mp4;base64,AAAA"}},{"type":"text","text":"private prompt"}]}]}`
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Set("id", 42)
			c.Request = httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			CaptureRequestBodyLog(c)
			other := map[string]interface{}{"model_price": 0.1, "quota": 123, "effective_model": "routed"}
			attachRequestBodyLog(c, other)
			assert.NotContains(t, other, "request_body")
			summary, ok := other["request_summary"].(map[string]any)
			require.True(t, ok)
			assert.Equal(t, map[string]any{"_omitted": "request body logging disabled", "_size_bytes": int64(len(body)), "_has_video": true}, summary)
			assert.Equal(t, 0.1, other["model_price"])
			assert.Equal(t, 123, other["quota"])
			assert.Equal(t, "routed", other["effective_model"])
			storage, err := common.GetBodyStorage(c)
			require.NoError(t, err)
			replay, err := storage.NewReader()
			require.NoError(t, err)
			defer replay.Close()
			unchanged, err := io.ReadAll(replay)
			require.NoError(t, err)
			assert.Equal(t, body, string(unchanged))
		})
	}
}
