package dto

import (
	"encoding/json"
	"testing"
)

func TestGeneralOpenAIRequest_IsThinkingDisabled(t *testing.T) {
	cases := map[string]bool{
		`{"model":"m","reasoning_effort":"none"}`:                        true,
		`{"model":"m","reasoning_effort":"None"}`:                        true,
		`{"model":"m","thinking":{"type":"disabled"}}`:                   true,
		`{"model":"m","enable_thinking":false}`:                          true,
		`{"model":"m","reasoning_effort":"low"}`:                         false,
		`{"model":"m","thinking":{"type":"enabled","budget_tokens":10}}`: false,
		`{"model":"m","enable_thinking":true}`:                           false,
		`{"model":"m"}`:                                                  false,
	}
	for body, want := range cases {
		var req GeneralOpenAIRequest
		if err := json.Unmarshal([]byte(body), &req); err != nil {
			t.Fatalf("%s: %v", body, err)
		}
		if got := req.IsThinkingDisabled(); got != want {
			t.Errorf("%s: got %v want %v", body, got, want)
		}
	}
	var nilReq *GeneralOpenAIRequest
	if nilReq.IsThinkingDisabled() {
		t.Error("nil request must report false")
	}
}
