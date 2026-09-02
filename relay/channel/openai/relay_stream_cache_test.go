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
	"github.com/stretchr/testify/require"
)

func TestOaiStreamHandlerNormalizesMoonshotCachedTokens(t *testing.T) {
	oldMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() { gin.SetMode(oldMode) })
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })

	body := strings.Join([]string{
		`data: {"id":"chatcmpl-test","object":"chat.completion.chunk","created":1710000000,"model":"kimi-k3","choices":[{"index":0,"delta":{"content":"OK"},"finish_reason":null}]}`,
		`data: {"id":"chatcmpl-test","object":"chat.completion.chunk","created":1710000000,"model":"kimi-k3","choices":[{"index":0,"delta":{},"finish_reason":"stop","usage":{"cached_tokens":1280}}],"usage":{"prompt_tokens":3760,"completion_tokens":2,"total_tokens":3762}}`,
		`data: [DONE]`,
		``,
	}, "\n")
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	resp := &http.Response{Body: io.NopCloser(strings.NewReader(body))}
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:       constant.ChannelTypeMoonshot,
			UpstreamModelName: "kimi-k3",
		},
		RelayFormat:        types.RelayFormatOpenAI,
		ShouldIncludeUsage: true,
		DisablePing:        true,
	}

	usage, err := OaiStreamHandler(c, info, resp)
	require.Nil(t, err)
	require.Equal(t, 1280, usage.PromptTokensDetails.CachedTokens)
	require.Contains(t, recorder.Body.String(), `"prompt_tokens_details":{"cached_tokens":1280},"cached_tokens":1280`)
}
