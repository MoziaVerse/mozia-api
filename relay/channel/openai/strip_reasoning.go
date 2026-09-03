package openai

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
)

// stripStreamReasoning removes reasoning_content / reasoning from every choice
// delta of one SSE data payload. It returns skip=true when the chunk carried
// nothing but reasoning, so the caller can drop it instead of forwarding an
// empty delta to the client. Chunks that fail to parse are forwarded untouched.
func stripStreamReasoning(data string) (stripped string, skip bool, err error) {
	if data == "" || data == "[DONE]" {
		return data, false, nil
	}
	var resp dto.ChatCompletionsStreamResponse
	if err := common.UnmarshalJsonStr(data, &resp); err != nil {
		// Not a chat chunk we understand (e.g. a bare comment); pass through.
		return data, false, nil
	}
	hadReasoning := false
	for i := range resp.Choices {
		delta := &resp.Choices[i].Delta
		if delta.ReasoningContent != nil || delta.Reasoning != nil {
			hadReasoning = true
			delta.ReasoningContent = nil
			delta.Reasoning = nil
		}
	}
	if !hadReasoning {
		return data, false, nil
	}
	if resp.Usage == nil && len(resp.Choices) > 0 && allDeltasEmpty(resp.Choices) {
		return "", true, nil
	}
	out, err := common.Marshal(resp)
	if err != nil {
		return "", false, err
	}
	return string(out), false, nil
}

func allDeltasEmpty(choices []dto.ChatCompletionsStreamResponseChoice) bool {
	for _, choice := range choices {
		if choice.FinishReason != nil || choice.Logprobs != nil {
			return false
		}
		d := choice.Delta
		if d.Content != nil || d.Role != "" || len(d.ToolCalls) > 0 {
			return false
		}
	}
	return true
}

// stripResponseReasoning removes reasoning_content / reasoning from every
// choices[].message of a non-stream chat completion body, preserving all other
// fields verbatim.
func stripResponseReasoning(body []byte) ([]byte, error) {
	var root map[string]any
	if err := common.Unmarshal(body, &root); err != nil {
		return nil, err
	}
	choices, ok := root["choices"].([]any)
	if !ok {
		return body, nil
	}
	changed := false
	for _, ch := range choices {
		choice, ok := ch.(map[string]any)
		if !ok {
			continue
		}
		msg, ok := choice["message"].(map[string]any)
		if !ok {
			continue
		}
		for _, k := range []string{"reasoning_content", "reasoning"} {
			if _, exists := msg[k]; exists {
				delete(msg, k)
				changed = true
			}
		}
	}
	if !changed {
		return body, nil
	}
	return common.Marshal(root)
}
