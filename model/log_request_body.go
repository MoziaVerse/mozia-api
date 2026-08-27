package model

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

const (
	requestBodyLogContextKey = "request_body_log_snapshot"
	requestBodyLogLimit      = int64(64 * 1024)
)

// CaptureRequestBodyLog captures the original client JSON before relay
// conversion. It is best-effort and must never change request handling.
func CaptureRequestBodyLog(c *gin.Context) {
	if c == nil || c.Request == nil {
		return
	}
	if _, exists := c.Get(requestBodyLogContextKey); exists {
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
	if size > requestBodyLogLimit {
		c.Set(requestBodyLogContextKey, map[string]any{
			"_omitted":    fmt.Sprintf("request body exceeds %d byte log limit", requestBodyLogLimit),
			"_size_bytes": size,
		})
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
	c.Set(requestBodyLogContextKey, redactRequestLogValue(value))
}

func attachRequestBodyLog(c *gin.Context, other map[string]interface{}) {
	if c == nil || other == nil {
		return
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
