package dto

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"testing"
)

func TestGeneralOpenAIRequestPreserveMessageLevelTools(t *testing.T) {
	raw := []byte(`{
		"model":"kimi-k3",
		"tool_choice":"required",
		"tools":[{"type":"function","function":{"name":"get_weather","description":"Get the weather","parameters":{"type":"object","properties":{"city":{"type":"string"}}}}}],
		"messages":[
			{"role":"system","content":"You are Kimi."},
			{"role":"user","content":"What time is it in Beijing?"},
			{"role":"system","tools":[{"type":"function","function":{"name":"get_current_time","description":"Get the current time of a city","parameters":{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}}}]},
			{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"get_current_time","arguments":"{\"city\":\"Beijing\"}"}}]},
			{"role":"system","content":"","tools":[{"type":"function","function":{"name":"lookup_order","parameters":{"type":"object"}}}]}
		]
	}`)

	var req GeneralOpenAIRequest
	require.NoError(t, common.Unmarshal(raw, &req))
	require.Len(t, req.Messages, 5)

	encoded, err := common.Marshal(req)
	require.NoError(t, err)

	messages := gjson.GetBytes(encoded, "messages").Array()
	require.Len(t, messages, 5)
	assert.Equal(t, "required", gjson.GetBytes(encoded, "tool_choice").String())
	assert.JSONEq(t, gjson.GetBytes(raw, "tools").Raw, gjson.GetBytes(encoded, "tools").Raw)

	// Regular messages keep their content untouched.
	assert.Equal(t, "You are Kimi.", messages[0].Get("content").String())
	assert.False(t, messages[0].Get("tools").Exists())
	assert.Equal(t, "What time is it in Beijing?", messages[1].Get("content").String())

	// Kimi K3 dynamic tool loading message: tools preserved byte-for-byte, no content key at all.
	assert.JSONEq(t, gjson.GetBytes(raw, "messages.2.tools").Raw, messages[2].Get("tools").Raw)
	assert.False(t, messages[2].Get("content").Exists())
	assert.Equal(t, "system", messages[2].Get("role").String())

	// Assistant tool call replay still emits an explicit "content": null.
	assistantContent := messages[3].Get("content")
	assert.True(t, assistantContent.Exists())
	assert.Equal(t, gjson.Null, assistantContent.Type)
	assert.JSONEq(t, gjson.GetBytes(raw, "messages.3.tool_calls").Raw, messages[3].Get("tool_calls").Raw)

	// Explicit empty content next to tools is forwarded as-is for the upstream to judge.
	emptyContent := messages[4].Get("content")
	assert.True(t, emptyContent.Exists())
	assert.Equal(t, gjson.String, emptyContent.Type)
	assert.Equal(t, "", emptyContent.String())
	assert.JSONEq(t, gjson.GetBytes(raw, "messages.4.tools").Raw, messages[4].Get("tools").Raw)

	// Token estimation sees message-level tools alongside the top-level ones.
	meta := req.GetTokenCountMeta()
	assert.Equal(t, 3, meta.ToolsCount)
	assert.Equal(t, 5, meta.MessagesCount)
	assert.Contains(t, meta.CombineText, "get_weather")
	assert.Contains(t, meta.CombineText, "get_current_time")
	assert.Contains(t, meta.CombineText, "Get the current time of a city")
	assert.Contains(t, meta.CombineText, "lookup_order")
}
