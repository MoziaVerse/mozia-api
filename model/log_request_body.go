package model

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/mozia_setting"
	"github.com/gin-gonic/gin"
)

const (
	requestBodyLogContextKey = "request_body_log_snapshot"
	requestSummaryContextKey = "request_body_log_summary"
	requestBodyLogLimit      = int64(16 * 1024)
)

// CaptureRequestBodyLog keeps a bounded summary by default. Body capture requires
// a specific user and an unexpired diagnostic window; it never changes relay data.
func CaptureRequestBodyLog(c *gin.Context) {
	if c == nil || c.Request == nil {
		return
	}
	if _, exists := c.Get(requestSummaryContextKey); exists {
		return
	}
	if !strings.HasPrefix(strings.ToLower(c.GetHeader("Content-Type")), "application/json") {
		return
	}

	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return
	}
	size := storage.Size()
	if size == 0 {
		return
	}
	summary := map[string]any{"_omitted": "request body logging disabled", "_size_bytes": size}
	c.Set(requestSummaryContextKey, summary)
	// Reuse routing's media detection within the existing inspection size bound.
	// Larger bodies retain their size; absence of _has_video means not inspected.
	if size <= 1536*1024 {
		if body, err := storage.Bytes(); err == nil {
			summary["_has_video"] = mozia_setting.HasVideoInput(body)
		}
	}
	userID, err := strconv.Atoi(os.Getenv("REQUEST_BODY_LOG_USER_ID"))
	until, timeErr := time.Parse(time.RFC3339, os.Getenv("REQUEST_BODY_LOG_UNTIL"))
	if err != nil || userID <= 0 || c.GetInt("id") != userID || timeErr != nil || !time.Now().Before(until) {
		return
	}
	if size > requestBodyLogLimit {
		summary["_omitted"] = fmt.Sprintf("request body exceeds %d byte diagnostic limit", requestBodyLogLimit)
		return
	}

	body, err := storage.Bytes()
	if err != nil {
		return
	}
	var value any
	if err := common.Unmarshal(body, &value); err != nil {
		return
	}
	value = redactRequestLogValue(value)
	encoded, err := common.Marshal(value)
	if err != nil || int64(len(encoded)) > requestBodyLogLimit {
		summary["_omitted"] = "redacted request body exceeds diagnostic limit"
		return
	}
	delete(summary, "_omitted")
	c.Set(requestBodyLogContextKey, value)
}

func attachRequestBodyLog(c *gin.Context, other map[string]interface{}) {
	if c == nil || other == nil {
		return
	}
	if summary, exists := c.Get(requestSummaryContextKey); exists {
		other["request_summary"] = summary
	}
	body, exists := c.Get(requestBodyLogContextKey)
	if !exists {
		return
	}
	other["request_body"] = body
}

func redactRequestLogValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if isSensitiveRequestLogKey(key) {
				typed[key] = "[REDACTED]"
				continue
			}
			typed[key] = redactRequestLogValue(child)
		}
		return typed
	case []any:
		for i := range typed {
			typed[i] = redactRequestLogValue(typed[i])
		}
		return typed
	case string:
		if isInlineBinaryValue(typed) {
			return fmt.Sprintf("[REDACTED inline data, %d bytes]", len(typed))
		}
		if sanitized, changed := redactURLCredentials(typed); changed {
			return sanitized
		}
		return typed
	default:
		return value
	}
}

func isSensitiveRequestLogKey(key string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "-", "_"), " ", "_"))
	compact := strings.ReplaceAll(normalized, "_", "")
	switch compact {
	case "apikey", "authorization", "accesstoken", "refreshtoken", "token", "key", "password", "secret", "clientsecret":
		return true
	default:
		return strings.HasSuffix(normalized, "_secret") || strings.HasSuffix(normalized, "_password")
	}
}

func isInlineBinaryValue(value string) bool {
	lower := strings.ToLower(value)
	return strings.HasPrefix(lower, "data:") && strings.Contains(lower, ";base64,")
}

func redactURLCredentials(value string) (string, bool) {
	parsed, err := url.Parse(value)
	if err != nil {
		if strings.HasPrefix(strings.ToLower(value), "http://") || strings.HasPrefix(strings.ToLower(value), "https://") {
			return "[REDACTED invalid URL]", true
		}
		return value, false
	}
	if (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return value, false
	}
	changed := parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != ""
	parsed.User = nil
	if parsed.RawQuery != "" {
		parsed.RawQuery = "redacted"
	}
	parsed.Fragment = ""
	return parsed.String(), changed
}
