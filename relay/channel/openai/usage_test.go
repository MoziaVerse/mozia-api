package openai

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCacheUsageReportingRequiresAnUpstreamField(t *testing.T) {
	for _, test := range []struct {
		name     string
		channel  int
		body     string
		reported bool
		cached   int
	}{
		{"missing", constant.ChannelTypeOpenAI, `{"usage":{"prompt_tokens":100}}`, false, 0},
		{"null", constant.ChannelTypeOpenAI, `{"usage":{"prompt_tokens_details":{"cached_tokens":null}}}`, false, 0},
		{"explicit-zero", constant.ChannelTypeOpenAI, `{"usage":{"prompt_tokens_details":{"cached_tokens":0}}}`, true, 0},
		{"positive", constant.ChannelTypeOpenAI, `{"usage":{"prompt_tokens_details":{"cached_tokens":80}}}`, true, 80},
		{"input-details-zero", constant.ChannelTypeOpenAI, `{"usage":{"input_tokens_details":{"cached_tokens":0}}}`, true, 0},
		{"deepseek-zero", constant.ChannelTypeDeepSeek, `{"usage":{"prompt_cache_hit_tokens":0}}`, true, 0},
		{"moonshot-zero", constant.ChannelTypeMoonshot, `{"choices":[{"usage":{"cached_tokens":0}}]}`, true, 0},
		{"moonshot-positive", constant.ChannelTypeMoonshot, `{"choices":[{"usage":{"cached_tokens":80}}]}`, true, 80},
		{"uncovered-adapter", constant.ChannelTypeOpenRouter, `{"usage":{"cached_tokens":80}}`, false, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			var response struct {
				Usage dto.Usage `json:"usage"`
			}
			require.NoError(t, common.Unmarshal([]byte(test.body), &response))
			applyUsagePostProcessing(&relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelType: test.channel}}, &response.Usage, []byte(test.body))
			assert.Equal(t, test.reported, response.Usage.CacheUsageReported)
			assert.Equal(t, test.cached, response.Usage.PromptTokensDetails.CachedTokens)
		})
	}
}
