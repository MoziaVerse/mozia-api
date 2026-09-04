package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
)

func streamChunk(delta dto.ChatCompletionsStreamResponseChoiceDelta, finish string) *dto.ChatCompletionsStreamResponse {
	choice := dto.ChatCompletionsStreamResponseChoice{Delta: delta}
	if finish != "" {
		choice.FinishReason = common.GetPointer[string](finish)
	}
	return &dto.ChatCompletionsStreamResponse{Id: "chatcmpl-x", Model: "qwen", Choices: []dto.ChatCompletionsStreamResponseChoice{choice}}
}

func toolChunk(index int, id, name, args string) dto.ChatCompletionsStreamResponseChoiceDelta {
	tc := dto.ToolCallResponse{ID: id, Type: "function"}
	tc.Index = common.GetPointer[int](index)
	tc.Function.Name = name
	tc.Function.Arguments = args
	return dto.ChatCompletionsStreamResponseChoiceDelta{ToolCalls: []dto.ToolCallResponse{tc}}
}

// 把整段 OpenAI 流喂给转换器，返回全部 Claude 事件。SendResponseCount 与真实调用方一致：从 1 起逐块递增。
func convertStream(t *testing.T, chunks []*dto.ChatCompletionsStreamResponse) []*dto.ClaudeResponse {
	t.Helper()
	info := &relaycommon.RelayInfo{}
	var out []*dto.ClaudeResponse
	for _, chunk := range chunks {
		info.SendResponseCount++
		if chunk.Usage != nil {
			info.ClaudeConvertInfo.Usage = chunk.Usage
		}
		out = append(out, StreamResponseOpenAI2Claude(chunk, info)...)
	}
	return out
}

// 每个 delta / stop 引用的索引都必须先有 content_block_start，否则 Claude Code 报 "Content block not found"。
func assertBlockIndexesConsistent(t *testing.T, events []*dto.ClaudeResponse) {
	t.Helper()
	started := map[int]bool{}
	for _, ev := range events {
		switch ev.Type {
		case "content_block_start":
			started[*ev.Index] = true
		case "content_block_delta", "content_block_stop":
			if ev.Index == nil || !started[*ev.Index] {
				t.Fatalf("%s references block index %v that was never started", ev.Type, ev.Index)
			}
		}
	}
}

func TestStreamResponseOpenAI2Claude_ToolTextToolKeepsBlockIndexesContiguous(t *testing.T) {
	// 生产抓包（Qwen3.8-27B）：思考 → 文本 → 工具 A → 文本 → 工具 B。
	// 工具 B 在 OpenAI 侧的 index 是 1（整条消息累计），不是新段的 0。
	reasoning := "plan"
	text1 := "I will list first."
	text2 := "Now reading."
	usage := &dto.Usage{PromptTokens: 10, CompletionTokens: 5}
	chunks := []*dto.ChatCompletionsStreamResponse{
		streamChunk(dto.ChatCompletionsStreamResponseChoiceDelta{ReasoningContent: &reasoning}, ""),
		streamChunk(dto.ChatCompletionsStreamResponseChoiceDelta{Content: &text1}, ""),
		streamChunk(toolChunk(0, "call_a", "list_dir", `{"path":"/app"}`), ""),
		streamChunk(dto.ChatCompletionsStreamResponseChoiceDelta{Content: &text2}, ""),
		streamChunk(toolChunk(1, "call_b", "read_file", `{"path":"/app/README.md"}`), ""),
		streamChunk(dto.ChatCompletionsStreamResponseChoiceDelta{}, "tool_calls"),
	}
	chunks[len(chunks)-1].Usage = usage

	events := convertStream(t, chunks)

	assertBlockIndexesConsistent(t, events)
	var starts []int
	for _, ev := range events {
		if ev.Type == "content_block_start" {
			starts = append(starts, *ev.Index)
		}
	}
	want := []int{0, 1, 2, 3, 4}
	if len(starts) != len(want) {
		t.Fatalf("content_block_start indexes = %v, want %v", starts, want)
	}
	for i := range want {
		if starts[i] != want[i] {
			t.Fatalf("content_block_start indexes = %v, want %v", starts, want)
		}
	}
}

func TestStreamResponseOpenAI2Claude_ParallelToolCallsInOneSegmentStayDistinct(t *testing.T) {
	// 同一段里并行两个工具（index 0、1）仍要各占一个块，且收尾各自 stop。
	usage := &dto.Usage{PromptTokens: 10, CompletionTokens: 5}
	chunks := []*dto.ChatCompletionsStreamResponse{
		streamChunk(toolChunk(0, "call_a", "list_dir", `{"path":"/a"}`), ""),
		streamChunk(toolChunk(1, "call_b", "read_file", `{"path":"/b"}`), ""),
		streamChunk(dto.ChatCompletionsStreamResponseChoiceDelta{}, "tool_calls"),
	}
	chunks[len(chunks)-1].Usage = usage

	events := convertStream(t, chunks)

	assertBlockIndexesConsistent(t, events)
	stops := 0
	for _, ev := range events {
		if ev.Type == "content_block_stop" {
			stops++
		}
	}
	if stops != 2 {
		t.Fatalf("content_block_stop count = %d, want 2", stops)
	}
}
