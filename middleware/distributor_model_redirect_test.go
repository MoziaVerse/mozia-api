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

func TestApplyUserModelRedirect(t *testing.T) {
	original := mozia_setting.UserModelRedirects2JSONString()
	t.Cleanup(func() {
		require.NoError(t, mozia_setting.UpdateUserModelRedirectsByJSONString(original))
	})
	require.NoError(t, mozia_setting.UpdateUserModelRedirectsByJSONString(`{
		"6218:vendor/source": {
			"user_id": 6218,
			"source_model": "vendor/source",
			"target_model": "vendor/target",
			"only_thinking_disabled": false
		},
		"6218:vendor/conditional": {
			"user_id": 6218,
			"source_model": "vendor/conditional",
			"target_model": "vendor/target",
			"only_thinking_disabled": true
		},
		"6218:vendor/visible-source": {
			"user_id": 6218,
			"source_model": "vendor/visible-source",
			"target_model": "vendor/target",
			"only_thinking_disabled": false,
			"seamless": true
		}
	}`))

	tests := []struct {
		name              string
		userID            int
		path              string
		thinkingType      string
		model             string
		wantModel         string
		wantRedirect      bool
		wantStripThinking bool
		wantVisibleModel  string
	}{
		{"always redirect", 6218, "/v1/chat/completions", "enabled", "vendor/source", "vendor/target", true, false, ""},
		{"conditional redirect", 6218, "/v1/chat/completions", "disabled", "vendor/conditional", "vendor/target", true, true, ""},
		{"conditional redirect skipped", 6218, "/v1/chat/completions", "enabled", "vendor/conditional", "vendor/conditional", false, false, ""},
		{"seamless redirect", 6218, "/v1/chat/completions", "", "vendor/visible-source", "vendor/target", true, false, "vendor/visible-source"},
		{"other user", 6219, "/v1/chat/completions", "disabled", "vendor/source", "vendor/source", false, false, ""},
		{"different model", 6218, "/v1/chat/completions", "disabled", "vendor/other", "vendor/other", false, false, ""},
		{"other endpoint", 6218, "/v1/responses", "", "vendor/source", "vendor/target", true, false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("POST", tt.path, nil)
			c.Set("id", tt.userID)
			request := &ModelRequest{Model: tt.model, ThinkingType: tt.thinkingType}

			applyUserModelRedirect(c, request)

			assert.Equal(t, tt.wantModel, request.Model)
			assert.Equal(t, tt.wantStripThinking, common.GetContextKeyBool(c, constant.ContextKeyStripRedirectThinking))
			assert.Equal(t, tt.wantVisibleModel, common.GetContextKeyString(c, constant.ContextKeyUserVisibleModel))
			if tt.wantRedirect {
				assert.Equal(t, tt.model, common.GetContextKeyString(c, constant.ContextKeyRequestedModel))
			}
		})
	}
}
