package claude

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestClaudeBackportRequestContentAndTools(t *testing.T) {
	var request dto.GeneralOpenAIRequest
	require.NoError(t, common.Unmarshal([]byte(`{
		"model":"claude-opus-4-8-high","reasoning_effort":"high",
		"response_format":{"type":"json_schema","json_schema":{"name":"result","strict":true,"schema":{"type":"object","properties":{"ok":{"type":"boolean"}},"required":["ok"],"additionalProperties":false}}},
		"tools":[{"type":"function","function":{"name":"read_file","strict":false}}],
		"messages":[
			{"role":"developer","content":[{"type":"text","text":"instructions","cache_control":{"type":"ephemeral"}}]},
			{"role":"user","content":[{"type":"text","text":"Read A and B","cache_control":{"type":"ephemeral"}}]},
			{"role":"assistant","content":"Reading A","tool_calls":[{"id":"a","type":"function","function":{"name":"read_file","arguments":"{}"}}]},
			{"role":"assistant","content":"Reading B","tool_calls":[{"id":"b","type":"function","function":{"name":"read_file","arguments":"{}"}}]},
			{"role":"tool","tool_call_id":"a","content":"A"},
			{"role":"tool","tool_call_id":"b","content":"B"}
		]}`), &request))
	before, err := common.Marshal(request)
	require.NoError(t, err)
	converted, err := RequestOpenAI2ClaudeMessage(nil, request)
	require.NoError(t, err)
	body, err := common.Marshal(converted)
	require.NoError(t, err)
	assert.Equal(t, "claude-opus-4-8", converted.Model)
	assert.Equal(t, "adaptive", converted.Thinking.Type)
	assert.Equal(t, "high", gjson.GetBytes(body, "output_config.effort").String())
	assert.Equal(t, "json_schema", gjson.GetBytes(body, "output_config.format.type").String())
	assert.False(t, gjson.GetBytes(body, "output_config.format.schema.additionalProperties").Bool())
	assert.Equal(t, "ephemeral", gjson.GetBytes(body, "system.0.cache_control.type").String())
	assert.Equal(t, "ephemeral", gjson.GetBytes(body, "messages.0.content.0.cache_control.type").String())
	assert.Equal(t, "a", gjson.GetBytes(body, "messages.1.content.1.id").String())
	assert.Equal(t, "b", gjson.GetBytes(body, "messages.2.content.1.id").String())
	assert.Equal(t, "a", gjson.GetBytes(body, "messages.3.content.0.tool_use_id").String())
	assert.Equal(t, "b", gjson.GetBytes(body, "messages.3.content.1.tool_use_id").String())
	assert.Equal(t, "object", gjson.GetBytes(body, "tools.0.input_schema.type").String())
	assert.True(t, gjson.GetBytes(body, "tools.0.strict").Exists(), "explicit false must survive")
	assert.False(t, gjson.GetBytes(body, "tools.0.strict").Bool())
	after, err := common.Marshal(request)
	require.NoError(t, err)
	assert.JSONEq(t, string(before), string(after), "conversion must not mutate retry input")
}

func TestClaudeBackportStreamLifecycle(t *testing.T) {
	previousTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 10
	t.Cleanup(func() { constant.StreamingTimeout = previousTimeout })
	frames := []string{
		`{"type":"message_start","message":{"id":"msg_test","model":"claude-test","usage":{"input_tokens":10,"cache_read_input_tokens":20,"cache_creation_input_tokens":5,"output_tokens":0}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"Consider files"}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"opaque_signature"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"content_block_start","index":1,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":1,"delta":{"type":"text_delta","text":"Reading files"}}`,
		`{"type":"content_block_stop","index":1}`,
		`{"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"call_a","name":"read_file","input":{}}}`,
		`{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{\"path\":"}}`,
		`{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"\"a\"}"}}`,
		`{"type":"content_block_stop","index":2}`,
		`{"type":"content_block_start","index":3,"content_block":{"type":"tool_use","id":"call_b","name":"read_file","input":{}}}`,
		`{"type":"content_block_delta","index":3,"delta":{"type":"input_json_delta","partial_json":"{\"path\":\"b\"}"}}`,
		`{"type":"content_block_stop","index":3}`,
		`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":7}}`,
		`{"type":"message_stop"}`,
	}
	input := "data: " + strings.Join(frames, "\n\ndata: ") + "\n\n"
	for _, format := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses, types.RelayFormatClaude} {
		t.Run(string(format), func(t *testing.T) {
			info := &relaycommon.RelayInfo{RelayFormat: format, IsStream: true, ShouldIncludeUsage: true, DisablePing: true, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-test"}}
			// Reusing RelayInfo simulates a retry: tool and Responses event indexes must restart.
			for attempt := 0; attempt < 2; attempt++ {
				w := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(w)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
				resp := &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(input))}
				usage, err := (&Adaptor{}).DoResponse(c, resp, info)
				require.Nil(t, err)
				assert.Equal(t, 10, usage.(*dto.Usage).PromptTokens)
				assert.Equal(t, 7, usage.(*dto.Usage).CompletionTokens)
				var events []gjson.Result
				for _, line := range strings.Split(w.Body.String(), "\n") {
					if strings.HasPrefix(line, "data: ") && strings.TrimPrefix(line, "data: ") != "[DONE]" {
						events = append(events, gjson.Parse(strings.TrimPrefix(line, "data: ")))
					}
				}
				require.NotEmpty(t, events)
				switch format {
				case types.RelayFormatClaude:
					assert.Equal(t, "message_start", events[0].Get("type").String())
					assert.Equal(t, "message_stop", events[len(events)-1].Get("type").String())
					assert.Contains(t, w.Body.String(), `"signature":"opaque_signature"`)
				case types.RelayFormatOpenAI:
					var indices []int64
					for _, event := range events {
						tool := event.Get("choices.0.delta.tool_calls.0")
						if tool.Get("id").Exists() {
							indices = append(indices, tool.Get("index").Int())
						}
					}
					assert.Equal(t, []int64{0, 1}, indices)
					assert.Equal(t, int64(35), events[len(events)-1].Get("usage.prompt_tokens").Int())
				case types.RelayFormatOpenAIResponses:
					textPartAdded := false
					var arguments []string
					for i, event := range events {
						assert.True(t, event.Get("sequence_number").Exists())
						assert.Equal(t, int64(i), event.Get("sequence_number").Int())
						switch event.Get("type").String() {
						case "response.content_part.added":
							textPartAdded = true
							assert.True(t, event.Get("part.annotations").IsArray())
						case "response.output_text.delta":
							assert.True(t, textPartAdded, "SDK indexes content before applying text deltas")
						case "response.output_text.done":
							assert.Equal(t, "Reading files", event.Get("text").String())
						case "response.reasoning_summary_text.done":
							assert.Equal(t, "Consider files", event.Get("text").String())
						case "response.function_call_arguments.done":
							arguments = append(arguments, event.Get("arguments").String())
						}
					}
					assert.Equal(t, []string{`{"path":"a"}`, `{"path":"b"}`}, arguments)
					final := events[len(events)-1]
					assert.Equal(t, "response.completed", final.Get("type").String())
					assert.Equal(t, "Consider files", final.Get("response.output.0.summary.0.text").String())
					assert.Equal(t, int64(35), final.Get("response.usage.input_tokens").Int())
					assert.Equal(t, int64(42), final.Get("response.usage.total_tokens").Int())
				}
			}
		})
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAIResponses, IsStream: true, DisablePing: true, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-test"}}
	truncated := strings.TrimSuffix(input, "data: "+frames[len(frames)-1]+"\n\n")
	_, err := (&Adaptor{}).DoResponse(c, &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(truncated))}, info)
	require.NotNil(t, err)
	assert.Contains(t, err.Error(), "message_stop")
	assert.NotContains(t, w.Body.String(), "response.completed")
}

func TestClaudeBackportReasoningBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, input, thinking string
		budget                int
		invalid               bool
	}{
		{"manual budget fits explicit output limit", `{"model":"claude-sonnet-4-5","max_tokens":2000,"reasoning_effort":"high"}`, "enabled", 1600, false},
		{"small manual output limit", `{"model":"claude-sonnet-4-5","max_tokens":256,"reasoning_effort":"high"}`, "", 0, true},
		{"explicit excessive budget", `{"model":"claude-sonnet-4-5","max_tokens":2000,"reasoning":{"max_tokens":4096}}`, "", 0, true},
		{"explicit budget preserved", `{"model":"claude-sonnet-4-5","max_tokens":2000,"reasoning":{"max_tokens":1024}}`, "enabled", 1024, false},
		{"adaptive does not use manual budget floor", `{"model":"claude-opus-4-8-high","max_tokens":256,"reasoning_effort":"high"}`, "adaptive", 0, false},
		{"unknown effort rejected", `{"model":"claude-sonnet-4-5","reasoning_effort":"invalid"}`, "", 0, true},
		{"conflicting effort rejected", `{"model":"claude-opus-4-8-high","reasoning_effort":"low"}`, "", 0, true},
		{"unsupported candidate count", `{"model":"claude-sonnet-4-5","n":2}`, "", 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var request dto.GeneralOpenAIRequest
			require.NoError(t, common.Unmarshal([]byte(tc.input), &request))
			out, err := RequestOpenAI2ClaudeMessage(nil, request)
			if tc.invalid {
				var apiError *types.NewAPIError
				require.ErrorAs(t, err, &apiError)
				assert.Equal(t, http.StatusBadRequest, types.NewError(err, types.ErrorCodeConvertRequestFailed).StatusCode)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, out.Thinking)
			assert.Equal(t, tc.thinking, out.Thinking.Type)
			assert.Equal(t, tc.budget, out.Thinking.GetBudgetTokens())
			assert.Equal(t, request.MaxTokens, out.MaxTokens)
		})
	}
}

func TestClaudeBackportResponsesRequest(t *testing.T) {
	var request dto.OpenAIResponsesRequest
	require.NoError(t, common.Unmarshal([]byte(`{"model":"claude-sonnet-4-5","max_output_tokens":256,"instructions":"Read carefully","input":[{"type":"message","role":"user","content":"Read A"},{"type":"function_call","call_id":"call_a","name":"read_file","arguments":"{}"},{"type":"function_call_output","call_id":"call_a","output":"file contents"}],"tools":[{"type":"function","name":"read_file","parameters":{"type":"object","properties":{}},"strict":true}],"tool_choice":"auto","parallel_tool_calls":false}`), &request))
	converted, err := (&Adaptor{}).ConvertOpenAIResponsesRequest(nil, nil, request)
	require.NoError(t, err)
	out := converted.(*dto.ClaudeRequest)
	body, err := common.Marshal(out)
	require.NoError(t, err)
	assert.Equal(t, uint(256), *out.MaxTokens)
	assert.True(t, gjson.GetBytes(body, "tools.0.strict").Bool())
	assert.True(t, gjson.GetBytes(body, "tool_choice.disable_parallel_tool_use").Bool())
	assert.Equal(t, "call_a", gjson.GetBytes(body, "messages.1.content.0.id").String())
	assert.Equal(t, "file contents", gjson.GetBytes(body, "messages.2.content.0.content").String())
	request.PreviousResponseID = "resp_previous"
	_, err = (&Adaptor{}).ConvertOpenAIResponsesRequest(nil, nil, request)
	var apiError *types.NewAPIError
	require.ErrorAs(t, err, &apiError)
	assert.Equal(t, http.StatusBadRequest, apiError.StatusCode, "unsupported server-side state must not silently disappear")
}

func TestClaudeBackportNonStreamingResponses(t *testing.T) {
	input := `{"id":"msg_test","model":"claude-test","type":"message","role":"assistant","stop_reason":"tool_use","content":[{"type":"thinking","thinking":"Reason A"},{"type":"text","text":"Text A"},{"type":"thinking","thinking":"Reason B"},{"type":"text","text":"Text B"},{"type":"tool_use","id":"call_a","name":"read_file","input":{}}],"usage":{"input_tokens":10,"cache_read_input_tokens":20,"cache_creation_input_tokens":5,"output_tokens":7}}`
	for _, format := range []types.RelayFormat{types.RelayFormatOpenAI, types.RelayFormatOpenAIResponses, types.RelayFormatClaude} {
		t.Run(string(format), func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			info := &relaycommon.RelayInfo{RelayFormat: format, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "claude-test"}}
			resp := &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(input))}
			usage, err := (&Adaptor{}).DoResponse(c, resp, info)
			require.Nil(t, err)
			assert.Equal(t, 10, usage.(*dto.Usage).PromptTokens, "billing retains Anthropic uncached input semantics")
			body := w.Body.String()
			switch format {
			case types.RelayFormatClaude:
				assert.JSONEq(t, input, body)
			case types.RelayFormatOpenAI:
				assert.Equal(t, "Text AText B", gjson.Get(body, "choices.0.message.content").String())
				assert.Equal(t, "Reason AReason B", gjson.Get(body, "choices.0.message.reasoning_content").String())
				assert.Equal(t, int64(35), gjson.Get(body, "usage.prompt_tokens").Int())
			case types.RelayFormatOpenAIResponses:
				assert.Equal(t, "response", gjson.Get(body, "object").String())
				assert.Equal(t, "completed", gjson.Get(body, "status").String())
				assert.Equal(t, "Text AText B", gjson.Get(body, "output.1.content.0.text").String())
				assert.Equal(t, "Reason AReason B", gjson.Get(body, "output.0.summary.0.text").String())
				assert.Equal(t, "call_a", gjson.Get(body, "output.2.call_id").String())
				assert.Equal(t, int64(35), gjson.Get(body, "usage.input_tokens").Int())
			}
		})
	}
}
