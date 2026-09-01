package middleware

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting/mozia_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetModelRequestPreservesThinkingType(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{
		"model":"moonshotai/kimi-k3",
		"thinking":{"type":"disabled"}
	}`))
	c.Request.Header.Set("Content-Type", "application/json")

	request, _, err := getModelRequest(c)
	require.NoError(t, err)
	assert.Equal(t, "moonshotai/kimi-k3", request.Model)
	assert.Equal(t, "disabled", request.ThinkingType)
}

func TestApplyUserThinkingDisabledRedirect(t *testing.T) {
	original := mozia_setting.UserThinkingDisabledRedirects2JSONString()
	t.Cleanup(func() {
		require.NoError(t, mozia_setting.UpdateUserThinkingDisabledRedirectsByJSONString(original))
	})
	require.NoError(t, mozia_setting.UpdateUserThinkingDisabledRedirectsByJSONString(`{
		"6218:vendor/source": {
			"user_id": 6218,
			"source_model": "vendor/source",
			"target_model": "vendor/target"
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
		{"matching request", 6218, "/v1/chat/completions", "disabled", "vendor/source", "vendor/target", true},
		{"other user", 6219, "/v1/chat/completions", "disabled", "vendor/source", "vendor/source", false},
		{"thinking enabled", 6218, "/v1/chat/completions", "enabled", "vendor/source", "vendor/source", false},
		{"different model", 6218, "/v1/chat/completions", "disabled", "vendor/other", "vendor/other", false},
		{"different endpoint", 6218, "/v1/responses", "disabled", "vendor/source", "vendor/source", false},
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
			}
		})
	}
}
