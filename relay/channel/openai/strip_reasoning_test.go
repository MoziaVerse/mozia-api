package openai

import (
	"encoding/json"
	"testing"
)

func TestStripStreamReasoning_DropsReasoningOnlyChunk(t *testing.T) {
	in := `{"id":"x","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"reasoning_content":"thinking..."},"finish_reason":null,"logprobs":null}]}`
	_, skip, err := stripStreamReasoning(in)
	if err != nil {
		t.Fatal(err)
	}
	if !skip {
		t.Fatalf("expected reasoning-only chunk to be skipped")
	}
}

func TestStripStreamReasoning_KeepsContentAndRemovesReasoning(t *testing.T) {
	in := `{"id":"x","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","content":"hi","reasoning_content":"r","reasoning":"r2"},"finish_reason":null,"logprobs":null}]}`
	out, skip, err := stripStreamReasoning(in)
	if err != nil || skip {
		t.Fatalf("unexpected skip=%v err=%v", skip, err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatal(err)
	}
	delta := m["choices"].([]any)[0].(map[string]any)["delta"].(map[string]any)
	if _, ok := delta["reasoning_content"]; ok {
		t.Fatalf("reasoning_content should be removed: %s", out)
	}
	if _, ok := delta["reasoning"]; ok {
		t.Fatalf("reasoning should be removed: %s", out)
	}
	if delta["content"] != "hi" || delta["role"] != "assistant" {
		t.Fatalf("content/role must be preserved: %s", out)
	}
}

func TestStripStreamReasoning_KeepsFinishAndUsageChunks(t *testing.T) {
	finish := `{"id":"x","choices":[{"index":0,"delta":{"reasoning_content":""},"finish_reason":"stop","logprobs":null}]}`
	out, skip, err := stripStreamReasoning(finish)
	if err != nil || skip {
		t.Fatalf("finish chunk must not be skipped: skip=%v err=%v", skip, err)
	}
	if !json.Valid([]byte(out)) {
		t.Fatalf("invalid json: %s", out)
	}
	usage := `{"id":"x","choices":[],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`
	out, skip, err = stripStreamReasoning(usage)
	if err != nil || skip || out != usage {
		t.Fatalf("usage chunk without reasoning must pass through untouched: %q skip=%v err=%v", out, skip, err)
	}
}

func TestStripStreamReasoning_PassThroughNonReasoning(t *testing.T) {
	for _, in := range []string{"[DONE]", "", `{"id":"x","choices":[{"index":0,"delta":{"content":"a"},"finish_reason":null,"logprobs":null}]}`, `not json`} {
		out, skip, err := stripStreamReasoning(in)
		if err != nil || skip || out != in {
			t.Fatalf("expected untouched pass-through for %q, got %q skip=%v err=%v", in, out, skip, err)
		}
	}
}

func TestStripResponseReasoning(t *testing.T) {
	body := []byte(`{"id":"x","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"2","reasoning_content":"1+1","extra":true},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2},"vendor_field":"keep"}`)
	out, err := stripResponseReasoning(body)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	msg := m["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)
	if _, ok := msg["reasoning_content"]; ok {
		t.Fatalf("reasoning_content not removed: %s", out)
	}
	if msg["content"] != "2" || msg["extra"] != true || m["vendor_field"] != "keep" {
		t.Fatalf("other fields must be preserved: %s", out)
	}
	// no reasoning -> body returned as-is
	plain := []byte(`{"choices":[{"message":{"content":"x"}}]}`)
	out, err = stripResponseReasoning(plain)
	if err != nil || string(out) != string(plain) {
		t.Fatalf("expected untouched body, got %s err=%v", out, err)
	}
}
