package service

import (
	"testing"

	"github.com/QuantumNous/new-api/dto"
)

func TestBuildClaudeUsageFromOpenAIUsage_SubtractsCachedTokensFromInput(t *testing.T) {
	// OpenAI 口径：prompt_tokens 是总量，cached_tokens 是其中命中缓存的部分。
	oai := &dto.Usage{PromptTokens: 13367, CompletionTokens: 8}
	oai.PromptTokensDetails.CachedTokens = 12288

	got := buildClaudeUsageFromOpenAIUsage(oai)

	if got.InputTokens != 13367-12288 {
		t.Fatalf("InputTokens = %d, want %d (prompt minus cached)", got.InputTokens, 13367-12288)
	}
	if got.CacheReadInputTokens != 12288 {
		t.Fatalf("CacheReadInputTokens = %d, want 12288", got.CacheReadInputTokens)
	}
	if got.InputTokens+got.CacheReadInputTokens != 13367 {
		t.Fatalf("input + cache_read = %d, must equal the OpenAI prompt total 13367", got.InputTokens+got.CacheReadInputTokens)
	}
}

func TestBuildClaudeUsageFromOpenAIUsage_NoCacheLeavesInputUntouched(t *testing.T) {
	oai := &dto.Usage{PromptTokens: 45294, CompletionTokens: 1098}

	got := buildClaudeUsageFromOpenAIUsage(oai)

	if got.InputTokens != 45294 || got.CacheReadInputTokens != 0 {
		t.Fatalf("got input=%d cache_read=%d, want 45294/0", got.InputTokens, got.CacheReadInputTokens)
	}
}

func TestBuildClaudeUsageFromOpenAIUsage_AnthropicSemanticIsNotSubtractedTwice(t *testing.T) {
	// 已是 Anthropic 口径的用量（input 本就不含缓存）不能再扣一次。
	oai := &dto.Usage{PromptTokens: 1079, CompletionTokens: 8, UsageSemantic: "anthropic"}
	oai.PromptTokensDetails.CachedTokens = 12288

	got := buildClaudeUsageFromOpenAIUsage(oai)

	if got.InputTokens != 1079 {
		t.Fatalf("InputTokens = %d, want 1079 unchanged", got.InputTokens)
	}
}

func TestBuildClaudeUsageFromOpenAIUsage_ClampsAtZero(t *testing.T) {
	// 上游偶尔会报 cached_tokens > prompt_tokens，不能出现负数。
	oai := &dto.Usage{PromptTokens: 100}
	oai.PromptTokensDetails.CachedTokens = 120

	if got := buildClaudeUsageFromOpenAIUsage(oai); got.InputTokens != 0 {
		t.Fatalf("InputTokens = %d, want 0", got.InputTokens)
	}
}
