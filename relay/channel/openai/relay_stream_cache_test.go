package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOaiStreamHandlerNormalizesCachedTokens(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	body := strings.Join([]string{
		`data: {"id":"chatcmpl-test","object":"chat.completion.chunk","created":1710000000,"model":"kimi-k3","choices":[{"index":0,"delta":{"content":"OK"},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl-test","object":"chat.completion.chunk","created":1710000000,"model":"kimi-k3","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":3760,"completion_tokens":2,"total_tokens":3762,"prompt_tokens_details":{"cached_tokens":1280}}}`,
		`data: [DONE]`,
		``,
	}, "\n")
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	resp := &http.Response{Body: io.NopCloser(strings.NewReader(body))}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeOpenAI,
			UpstreamModelName: "kimi-k3",
		},
		RelayFormat:        types.RelayFormatOpenAI,
		ShouldIncludeUsage: true,
		DisablePing:        true,
	}

	usage, err := OaiStreamHandler(c, info, resp)
	require.Nil(t, err)
	require.Equal(t, 1280, usage.PromptTokensDetails.CachedTokens)
	assert.True(t, usage.CacheUsageReported)
	require.Contains(t, recorder.Body.String(), `"prompt_tokens_details":{"cached_tokens":1280},"cached_tokens":1280`)
}

func TestOpenaiHandlerNormalizesMissingCachedTokens(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })
	body := `{"id":"chatcmpl-test","object":"chat.completion","model":"kimi-k3","choices":[{"index":0,"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}],"usage":{"prompt_tokens":378,"completion_tokens":1,"total_tokens":379,"prompt_tokens_details":null}}`
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	resp := &http.Response{Body: io.NopCloser(strings.NewReader(body))}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeOpenAI,
			UpstreamModelName: "kimi-k3",
		},
		RelayFormat: types.RelayFormatOpenAI,
	}

	usage, err := OpenaiHandler(c, info, resp)
	require.Nil(t, err)
	require.Zero(t, usage.PromptTokensDetails.CachedTokens)
	assert.False(t, usage.CacheUsageReported, "normalizing an absent field to zero must not invent upstream reporting")
	require.Contains(t, recorder.Body.String(), `"prompt_tokens_details":{"cached_tokens":0}`)
	require.Contains(t, recorder.Body.String(), `"cached_tokens":0`)
}

func TestOaiStreamHandlerPreservesEarlierCacheReport(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
	for _, test := range []struct {
		name      string
		channel   int
		cacheData string
		reported  bool
		cached    int
		estimated bool
	}{
		{"missing", constant.ChannelTypeOpenAI, `"choices":[{"delta":{"content":"OK"}}]`, false, 0, false},
		{"explicit-zero", constant.ChannelTypeOpenAI, `"usage":{"prompt_tokens_details":{"cached_tokens":0}}`, true, 0, false},
		{"positive", constant.ChannelTypeOpenAI, `"usage":{"prompt_tokens_details":{"cached_tokens":80}}`, true, 80, false},
		{"moonshot-zero", constant.ChannelTypeMoonshot, `"choices":[{"usage":{"cached_tokens":0}}]`, true, 0, false},
		{"estimated", constant.ChannelTypeOpenAI, `"usage":{"prompt_tokens_details":{"cached_tokens":80}}`, true, 80, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			finalUsage := `,"usage":{"prompt_tokens":100,"completion_tokens":1,"total_tokens":101}`
			if test.estimated {
				finalUsage = ""
			}
			body := "data: {" + test.cacheData + "}\n\n" +
				`data: {"id":"cache-test","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]` + finalUsage + "}\n\ndata: [DONE]\n\n"
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			info := &relaycommon.RelayInfo{
				ChannelMeta: &relaycommon.ChannelMeta{ChannelType: test.channel, UpstreamModelName: "kimi-k3"},
				RelayFormat: types.RelayFormatOpenAI, IsStream: true, ShouldIncludeUsage: true, DisablePing: true,
			}
			info.SetEstimatePromptTokens(100)
			usage, apiErr := OaiStreamHandler(c, info, &http.Response{Body: io.NopCloser(strings.NewReader(body))})
			require.Nil(t, apiErr)
			require.NotNil(t, usage)
			assert.Equal(t, 100, usage.PromptTokens)
			assert.Equal(t, test.reported, usage.CacheUsageReported)
			assert.Equal(t, test.cached, usage.PromptTokensDetails.CachedTokens)
		})
	}
}
