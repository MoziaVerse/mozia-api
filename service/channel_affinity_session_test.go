package service

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newClaudeAffinityTestContext(t *testing.T, userID, tokenID int, header, body string) *gin.Context {
	t.Helper()
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
	ctx.Request.Header.Set("X-Claude-Code-Session-Id", header)
	ctx.Set("id", userID)
	ctx.Set("token_id", tokenID)
	t.Cleanup(func() { common.CleanupBodyStorage(ctx) })
	return ctx
}

func TestExtractChannelAffinityValue_ClaudeSession(t *testing.T) {
	for _, tt := range []struct {
		name, header, body, want string
	}{
		{"header takes precedence", " session-a ", `{"metadata":{"user_id":"user_device_session_session-b"}}`, "session-a"},
		{"legacy", "", `{"metadata":{"user_id":"user_device_account_account-id_session_session-a"}}`, "session-a"},
		{"JSON string", "", `{"metadata":{"user_id":"{\"device_id\":\"device\",\"account_uuid\":\"account\",\"session_id\":\"session-a\"}"}}`, "session-a"},
		{"missing metadata", "", `{}`, ""},
		{"ordinary user ID", "", `{"metadata":{"user_id":"customer-123"}}`, ""},
		{"missing legacy session", "", `{"metadata":{"user_id":"user_device_session_"}}`, ""},
		{"invalid JSON string", "", `{"metadata":{"user_id":"{\"session_id\":\"session-a\""}}`, ""},
		{"missing JSON session", "", `{"metadata":{"user_id":"{\"device_id\":\"device\"}"}}`, ""},
		{"non-string JSON session", "", `{"metadata":{"user_id":"{\"session_id\":123}"}}`, ""},
		{"non-string metadata", "", `{"metadata":{"user_id":123}}`, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := newClaudeAffinityTestContext(t, 7727, 123, tt.header, tt.body)
			assert.Equal(t, tt.want, extractChannelAffinityValue(ctx, operation_setting.ChannelAffinityKeySource{Type: "claude_session"}))
		})
	}
}

func TestChannelAffinity_ClaudeSessionIsolationAndFallback(t *testing.T) {
	setting := operation_setting.GetChannelAffinitySetting()
	original, originalRedisEnabled := *setting, common.RedisEnabled
	common.RedisEnabled = false
	*setting = operation_setting.ChannelAffinitySetting{
		Enabled: true, SwitchOnSuccess: true, DefaultTTLSeconds: 3600,
		Rules: []operation_setting.ChannelAffinityRule{{
			Name: "test-claude-session", UserIDs: []int{7727},
			ModelRegex: []string{"^(deepseek|kimi)$"}, PathRegex: []string{"^/v1/messages$"},
			KeySources: []operation_setting.ChannelAffinityKeySource{
				{Type: "claude_session"}, {Type: "context_int", Key: "token_id"},
			},
			IncludeTokenID: true, IncludeModelName: true, IncludeUsingGroup: true, IncludeRuleName: true,
		}},
	}
	t.Cleanup(func() { *setting, common.RedisEnabled = original, originalRedisEnabled })

	// Successful requests establish separate session and non-session bindings.
	for _, seed := range []struct {
		header  string
		channel int
	}{{"session-a", 180}, {"", 178}} {
		ctx := newClaudeAffinityTestContext(t, 7727, 123, seed.header, `{}`)
		_, found := GetPreferredChannelByAffinity(ctx, "deepseek", "default")
		require.False(t, found)
		_, _, configured := getChannelAffinityContext(ctx)
		require.True(t, configured)
		RecordChannelAffinity(ctx, seed.channel)
		t.Cleanup(func() { ClearCurrentChannelAffinityCache(ctx) })
	}

	for _, tt := range []struct {
		name, header, body, model, group string
		userID, tokenID, channel         int
		configured                       bool
	}{
		{"same session", "session-a", `{}`, "deepseek", "default", 7727, 123, 180, true},
		{"legacy metadata same session", "", `{"metadata":{"user_id":"user_other-device_session_session-a"}}`, "deepseek", "default", 7727, 123, 180, true},
		{"JSON metadata same session", "", `{"metadata":{"user_id":"{\"session_id\":\"session-a\"}"}}`, "deepseek", "default", 7727, 123, 180, true},
		{"other session", "session-b", `{}`, "deepseek", "default", 7727, 123, 0, true},
		{"other API key", "session-a", `{}`, "deepseek", "default", 7727, 124, 0, true},
		{"other model", "session-a", `{}`, "kimi", "default", 7727, 123, 0, true},
		{"other group", "session-a", `{}`, "deepseek", "vip", 7727, 123, 0, true},
		{"other user", "session-a", `{}`, "deepseek", "default", 7728, 123, 0, false},
		{"unauthenticated", "session-a", `{}`, "deepseek", "default", 0, 123, 0, false},
		{"missing API key", "session-a", `{}`, "deepseek", "default", 7727, 0, 0, false},
		{"non-session fallback", "", `{}`, "deepseek", "default", 7727, 123, 178, true},
		{"unrecognized metadata fallback", "", `{"metadata":{"user_id":"customer-123"}}`, "deepseek", "default", 7727, 123, 178, true},
		{"fallback other API key", "", `{}`, "deepseek", "default", 7727, 124, 0, true},
		{"session does not collide with fallback", "123", `{}`, "deepseek", "default", 7727, 123, 0, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := newClaudeAffinityTestContext(t, tt.userID, tt.tokenID, tt.header, tt.body)
			channelID, found := GetPreferredChannelByAffinity(ctx, tt.model, tt.group)
			assert.Equal(t, tt.channel > 0, found)
			assert.Equal(t, tt.channel, channelID)
			_, _, configured := getChannelAffinityContext(ctx)
			assert.Equal(t, tt.configured, configured)
			assert.False(t, ShouldSkipRetryAfterChannelAffinityFailure(ctx))
		})
	}
}
