package openai

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaudeCodeOpenAICompatibility(t *testing.T) {
	for _, models := range [][2]string{
		{"deepseek/deepseek-v4-flash", "deepseek-v4-flash"},
		{"deepseek/deepseek-v4-pro", "deepseek-v4-pro"},
		{"moonshotai/kimi-k3", "kimi-k3"},
		{"moonshotai/kimi-k3-new", "kimi-k3"},
	} {
		t.Run(models[0], func(t *testing.T) {
			info := &relaycommon.RelayInfo{
				OriginModelName: models[0], RelayFormat: types.RelayFormatClaude, IsStream: true,
				ChannelMeta: &relaycommon.ChannelMeta{
					ChannelType: constant.ChannelTypeOpenAI, UpstreamModelName: models[1],
					ChannelBaseUrl: "https://upstream.invalid", SupportStreamOptions: true,
				},
			}
			var req dto.ClaudeRequest
			require.NoError(t, common.Unmarshal([]byte(`{
				"max_tokens":4096,"stream":true,
				"thinking":{"type":"adaptive"},"output_config":{"effort":"max"},
				"tool_choice":{"type":"auto","disable_parallel_tool_use":true},
				"tools":[{"name":"read_file","input_schema":{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}}],
				"messages":[
					{"role":"user","content":"Read example.txt."},
					{"role":"assistant","content":[
						{"type":"thinking","thinking":"I need to read the file."},
						{"type":"text","text":"Reading the file.","cache_control":{"type":"ephemeral"}},
						{"type":"tool_use","id":"call_1","name":"read_file","input":{"path":"example.txt"}}
					]},
					{"role":"user","content":[{"type":"tool_result","tool_use_id":"call_1","content":"Example contents."}]}
				]
			}`), &req))
			req.Model = models[1]
			adaptor := &Adaptor{}
			adaptor.Init(info)
			converted, err := adaptor.ConvertClaudeRequest(nil, info, &req)
			require.NoError(t, err)
			body, err := common.Marshal(converted)
			require.NoError(t, err)
			var upstream dto.GeneralOpenAIRequest
			require.NoError(t, common.Unmarshal(body, &upstream))
			url, err := adaptor.GetRequestURL(info)
			require.NoError(t, err)
			assert.Equal(t, "https://upstream.invalid/v1/chat/completions", url)
			assert.Equal(t, models[1], upstream.Model)
			assert.Equal(t, common.GetPointer(true), upstream.Stream)
			assert.Equal(t, &dto.StreamOptions{IncludeUsage: true}, upstream.StreamOptions)
			assert.Equal(t, "max", upstream.ReasoningEffort)
			if models[1] == "kimi-k3" {
				assert.Empty(t, upstream.THINKING, "Kimi K3 OpenAI API always thinks and does not accept thinking")
			} else {
				assert.JSONEq(t, `{"type":"enabled"}`, string(upstream.THINKING))
			}
			assert.Equal(t, "auto", upstream.ToolChoice)
			assert.Equal(t, common.GetPointer(false), upstream.ParallelTooCalls)
			require.Len(t, upstream.Tools, 1)
			assert.Equal(t, "read_file", upstream.Tools[0].Function.Name)
			assert.Equal(t, []any{"path"}, upstream.Tools[0].Function.Parameters.(map[string]any)["required"])
			require.Len(t, upstream.Messages, 3)
			assistant := upstream.Messages[1]
			assert.Equal(t, "assistant", assistant.Role)
			assert.Equal(t, "I need to read the file.", assistant.GetReasoningContent())
			assert.Equal(t, "Reading the file.", assistant.Content, "assistant text must be a string for strict upstreams")
			calls := assistant.ParseToolCalls()
			require.Len(t, calls, 1)
			assert.Equal(t, "call_1", calls[0].ID)
			assert.JSONEq(t, `{"path":"example.txt"}`, calls[0].Function.Arguments)
			assert.Equal(t, "tool", upstream.Messages[2].Role)
			assert.Equal(t, "call_1", upstream.Messages[2].ToolCallId)
			assert.Equal(t, "Example contents.", upstream.Messages[2].StringContent())

			// Non-streaming responses must also preserve everything needed by the next tool turn.
			response := service.ResponseOpenAI2Claude(&dto.OpenAITextResponse{
				Model: models[1], Choices: []dto.OpenAITextResponseChoice{{Message: assistant, FinishReason: "tool_calls"}},
			}, info)
			assert.Equal(t, "tool_use", response.StopReason)
			require.Len(t, response.Content, 3)
			assert.Equal(t, "thinking", response.Content[0].Type)
			assert.Equal(t, assistant.ReasoningContent, response.Content[0].Thinking)
			req.Messages[1].Content = response.Content
			roundTrip, err := adaptor.ConvertClaudeRequest(nil, info, &req)
			require.NoError(t, err)
			roundTripBody, err := common.Marshal(roundTrip)
			require.NoError(t, err)
			assert.JSONEq(t, string(body), string(roundTripBody))
		})
	}

	t.Run("text normalization preserves media and cache hints", func(t *testing.T) {
		var req dto.ClaudeRequest
		require.NoError(t, common.Unmarshal([]byte(`{"messages":[
			{"role":"assistant","content":[{"type":"thinking","thinking":"First."},{"type":"thinking","thinking":"Second."}]},
			{"role":"user","content":[{"type":"text","text":"one"},{"type":"text","text":"two"}]},
			{"role":"user","content":[{"type":"text","text":"Describe this."},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"aGVsbG8="}}]},
			{"role":"assistant","content":[{"type":"text","text":"Cached context.","cache_control":{"type":"ephemeral"}}]}
		]}`), &req))
		out, err := service.ClaudeToOpenAIRequest(req, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenRouter}})
		require.NoError(t, err)
		require.Len(t, out.Messages, 4)
		assert.Equal(t, "First.Second.", out.Messages[0].GetReasoningContent())
		assert.Equal(t, "onetwo", out.Messages[1].Content)
		media := out.Messages[2].ParseContent()
		require.Len(t, media, 2)
		assert.Equal(t, "Describe this.", media[0].Text)
		image, err := common.Any2Type[dto.MessageImageUrl](media[1].ImageUrl)
		require.NoError(t, err)
		assert.Equal(t, "data:image/png;base64,aGVsbG8=", image.Url)
		cached := out.Messages[3].ParseContent()
		require.Len(t, cached, 1)
		assert.JSONEq(t, `{"type":"ephemeral"}`, string(cached[0].CacheControl))
	})

	t.Run("tool choice and optional parallel control", func(t *testing.T) {
		for _, tc := range []struct {
			input, want string
			parallel    *bool
			invalid     bool
		}{
			{input: `null`, want: `null`},
			{input: `{"type":"auto"}`, want: `"auto"`},
			{input: `{"type":"none"}`, want: `"none"`},
			{input: `{"type":"any","disable_parallel_tool_use":false}`, want: `"required"`, parallel: common.GetPointer(true)},
			{input: `{"type":"tool","name":"read_file","disable_parallel_tool_use":true}`, want: `{"type":"function","function":{"name":"read_file"}}`, parallel: common.GetPointer(false)},
			{input: `{"type":"tool"}`, invalid: true},
			{input: `{"type":"invalid"}`, invalid: true},
			{input: `"auto"`, invalid: true},
		} {
			t.Run(tc.input, func(t *testing.T) {
				var req dto.ClaudeRequest
				require.NoError(t, common.Unmarshal([]byte(`{"tool_choice":`+tc.input+`}`), &req))
				out, err := service.ClaudeToOpenAIRequest(req, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{}})
				if tc.invalid {
					require.Error(t, err)
					return
				}
				require.NoError(t, err)
				choice, err := common.Marshal(out.ToolChoice)
				require.NoError(t, err)
				assert.JSONEq(t, tc.want, string(choice))
				assert.Equal(t, tc.parallel, out.ParallelTooCalls)
			})
		}
	})

	t.Run("provider thinking controls", func(t *testing.T) {
		for _, tc := range []struct{ model, thinking, effort, wantThinking, wantEffort string }{
			{"deepseek-v4-flash", "enabled", "low", `{"type":"enabled"}`, "low"},
			{"deepseek/deepseek-v4-pro", "adaptive", "medium", `{"type":"enabled"}`, "high"},
			{"deepseek-v4-pro", "disabled", "max", `{"type":"disabled"}`, ""},
			{"deepseek-v4-pro", "", "xhigh", "", "high"},
			{"kimi-k3", "enabled", "low", "", "low"},
			{"kimi-k3", "disabled", "medium", "", "high"},
			{"kimi-k3", "", "", "", ""},
			{"gpt-4.1", "adaptive", "max", "", ""},
		} {
			t.Run(tc.model+"/"+tc.thinking+"/"+tc.effort, func(t *testing.T) {
				req := dto.ClaudeRequest{Model: tc.model}
				if tc.thinking != "" {
					req.Thinking = &dto.Thinking{Type: tc.thinking, BudgetTokens: common.GetPointer(2048)}
				}
				var err error
				req.OutputConfig, err = common.Marshal(dto.OutputConfigForEffort{Effort: tc.effort})
				require.NoError(t, err)
				out, err := service.ClaudeToOpenAIRequest(req, &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenAI}})
				require.NoError(t, err)
				assert.Equal(t, tc.wantEffort, out.ReasoningEffort)
				if tc.wantThinking == "" {
					assert.Empty(t, out.THINKING)
				} else {
					assert.JSONEq(t, tc.wantThinking, string(out.THINKING))
				}
			})
		}
	})
}
