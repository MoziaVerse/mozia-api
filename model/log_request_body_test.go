package model

import (
	"bytes"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCaptureRequestBodyLogRedactsSecretsAndInlineData(t *testing.T) {
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
	body := `{"prompt":"` + strings.Repeat("a", int(requestBodyLogLimit)) + `"}`
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	CaptureRequestBodyLog(c)
	other := map[string]interface{}{}
	attachRequestBodyLog(c, other)

	requestBody, ok := other["request_body"].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, requestBody["_omitted"], "exceeds")
	assert.Equal(t, int64(len(body)), requestBody["_size_bytes"])
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
