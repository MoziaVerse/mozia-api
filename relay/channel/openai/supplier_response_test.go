package openai

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSupplierResponseCompletionAndOriginalUsage(t *testing.T) {
	oldTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = oldTimeout })
	for _, test := range []struct {
		name     string
		stream   bool
		body     string
		complete bool
		usage    bool
		content  bool
	}{
		{"stream-success", true, "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"OK\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":1,\"total_tokens\":4}}\n\ndata: [DONE]\n\n", true, true, true},
		{"stream-eof-interrupted", true, "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n", false, false, true},
		{"done-without-finish", true, "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\n\ndata: [DONE]\n\n", false, false, false},
		{"nonstream-success", false, `{"choices":[{"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`, true, true, false},
		{"nonstream-no-usage", false, `{"choices":[{"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}]}`, true, false, false},
		{"nonstream-empty", false, `{"choices":[],"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`, false, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			state := &service.SupplierRouteState{Current: &model.SupplierAttempt{}, Started: time.Now(), Sent: true}
			c.Set("supplier_routing_state", state)
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenAI, UpstreamModelName: "gpt-4o"}, RelayFormat: types.RelayFormatOpenAI, IsStream: test.stream, DisablePing: true, ShouldIncludeUsage: true}
			resp := &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(test.body))}
			var apiErr *types.NewAPIError
			if test.stream {
				_, apiErr = OaiStreamHandler(c, info, resp)
			} else {
				_, apiErr = OpenaiHandler(c, info, resp)
			}
			if test.complete {
				require.Nil(t, apiErr)
			} else {
				require.NotNil(t, apiErr)
				assert.True(t, types.IsSkipRetryError(apiErr))
			}
			assert.Equal(t, test.content, !state.FirstContent.IsZero(), "role-only chunks must not count as TTFT")
			if test.usage {
				require.NotNil(t, state.Usage)
				assert.Equal(t, 3, state.Usage.PromptTokens)
			} else {
				assert.Nil(t, state.Usage, "customer fallback token counting must not invent procurement usage")
			}
			if !test.complete && !test.stream {
				assert.Empty(t, recorder.Body.String(), "invalid upstream success must not be sent to the customer")
			}
		})
	}
}
