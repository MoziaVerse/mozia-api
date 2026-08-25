package middleware

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting/mozia_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyUserThinkingDisabledRedirect(t *testing.T) {
	original := mozia_setting.UserThinkingDisabledRedirects2JSONString()
	t.Cleanup(func() {
		require.NoError(t, mozia_setting.UpdateUserThinkingDisabledRedirectsByJSONString(original))
	})
	require.NoError(t, mozia_setting.UpdateUserThinkingDisabledRedirectsByJSONString(`{
		"6218:moonshotai/kimi-k3": {
			"user_id": 6218,
			"source_model": "moonshotai/kimi-k3",
			"target_model": "moonshotai/kimi-k2.6",
			"enabled": true
		}
	}`))

	tests := []struct {
		name         string
		userID       int
		path         string
		thinkingType string
		model        string
		wantModel    string
		wantApplied  bool
	}{
		{"matching request", 6218, "/v1/chat/completions", "disabled", "moonshotai/kimi-k3", "moonshotai/kimi-k2.6", true},
		{"other user", 6219, "/v1/chat/completions", "disabled", "moonshotai/kimi-k3", "moonshotai/kimi-k3", false},
		{"thinking enabled", 6218, "/v1/chat/completions", "enabled", "moonshotai/kimi-k3", "moonshotai/kimi-k3", false},
		{"different model", 6218, "/v1/chat/completions", "disabled", "moonshotai/kimi-k2.6", "moonshotai/kimi-k2.6", false},
		{"different endpoint", 6218, "/v1/responses", "disabled", "moonshotai/kimi-k3", "moonshotai/kimi-k3", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", tt.path, nil)
			c.Set("id", tt.userID)
			request := &ModelRequest{Model: tt.model, ThinkingType: tt.thinkingType}

			applyUserThinkingDisabledRedirect(c, request)

			assert.Equal(t, tt.wantModel, request.Model)
			assert.Equal(t, tt.wantApplied, common.GetContextKeyBool(c, constant.ContextKeyModelRedirectApplied))
			if tt.wantApplied {
				assert.Equal(t, tt.model, common.GetContextKeyString(c, constant.ContextKeyRequestedModel))
				assert.Equal(t, "thinking_disabled", common.GetContextKeyString(c, constant.ContextKeyModelRedirectReason))
			}
		})
	}
}
