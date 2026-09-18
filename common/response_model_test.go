package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReplaceResponseModel(t *testing.T) {
	for _, tc := range []struct{ name, input, want string }{
		{"completion", `{"model":"kimi-k3-fireworks","choices":[{"message":{"content":"kimi-k3-fireworks","tool_calls":[{"function":{"arguments":"{\"model\":\"keep\"}"}}]}}],"extra":{"model":"keep"},"seed":9007199254740993}`, `{"model":"moonshotai/kimi-k3","choices":[{"message":{"content":"kimi-k3-fireworks","tool_calls":[{"function":{"arguments":"{\"model\":\"keep\"}"}}]}}],"extra":{"model":"keep"},"seed":9007199254740993}`},
		{"claude start", `{"type":"message_start","message":{"model":"internal","content":[]}}`, `{"type":"message_start","message":{"model":"moonshotai/kimi-k3","content":[]}}`},
		{"responses event", `{"type":"response.completed","response":{"model":"internal","metadata":{"model":"keep"}}}`, `{"type":"response.completed","response":{"model":"moonshotai/kimi-k3","metadata":{"model":"keep"}}}`},
		{"realtime", `{"type":"session.created","session":{"model":"internal"}}`, `{"type":"session.created","session":{"model":"moonshotai/kimi-k3"}}`},
		{"gemini", `{"modelVersion":"internal-version","candidates":[]}`, `{"modelVersion":"moonshotai/kimi-k3","candidates":[]}`},
		{"no metadata", `{"type":"content_block_delta","delta":{"text":"internal"}}`, `{"type":"content_block_delta","delta":{"text":"internal"}}`},
		{"done", `[DONE]`, `[DONE]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, err := ReplaceResponseModel([]byte(tc.input), "moonshotai/kimi-k3")
			require.NoError(t, err)
			assert.Equal(t, tc.want, string(data))
		})
	}
}
