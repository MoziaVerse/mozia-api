package openai

import (
	"context"
	"github.com/QuantumNous/new-api/common"
	"github.com/go-redis/redis/v8"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
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
		{"nonstream-error-finish", false, `{"choices":[{"message":{"role":"assistant","content":"error"},"finish_reason":"error"}]}`, false, false, false},
		{"stream-error-finish", true, "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"},\"finish_reason\":\"error\"}]}\n\ndata: [DONE]\n\n", false, false, true},
		{"nonstream-empty", false, `{"choices":[],"usage":{"prompt_tokens":3,"completion_tokens":1,"total_tokens":4}}`, false, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			common.SetContextKey(c, constant.ContextKeyUserVisibleModel, "moonshotai/kimi-k3")
			state := &service.SupplierRouteState{Current: &model.SupplierAttempt{}, Started: time.Now(), Sent: true}
			c.Set("supplier_routing_state", state)
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenAI, UpstreamModelName: "gpt-4o"}, RelayFormat: types.RelayFormatOpenAI, IsStream: test.stream, DisablePing: true, ShouldIncludeUsage: true}
			resp := &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(strings.ReplaceAll(test.body, `{"choices":`, `{"model":"kimi-k3-fireworks","choices":`)))}
			var apiErr *types.NewAPIError
			if test.stream {
				_, apiErr = OaiStreamHandler(c, info, resp)
			} else {
				_, apiErr = OpenaiHandler(c, info, resp)
			}
			if test.complete {
				require.Nil(t, apiErr)
				assert.Contains(t, recorder.Body.String(), `"model":"moonshotai/kimi-k3"`)
				assert.NotContains(t, recorder.Body.String(), "kimi-k3-fireworks")
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

type supplierCompletionWriter struct {
	gin.ResponseWriter
	onWrite func()
}

func (w *supplierCompletionWriter) Write(data []byte) (int, error) {
	w.onWrite()
	return w.ResponseWriter.Write(data)
}

func TestSupplierSettlesBeforeSuccessfulResponseDelivery(t *testing.T) {
	address := os.Getenv("SUPPLIER_TEST_REDIS")
	if address == "" {
		t.Skip("set SUPPLIER_TEST_REDIS to a disposable Redis instance")
	}
	client := redis.NewClient(&redis.Options{Addr: address, DB: 15})
	oldClient := common.RDB
	common.RDB = client
	t.Cleanup(func() { common.RDB = oldClient; require.NoError(t, client.Close()) })
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(ctx)
	state := &service.SupplierRouteState{ReservationID: "completed-http-response", Sent: true, Started: time.Now(), Excluded: map[int]bool{}, Current: &model.SupplierAttempt{PriceJSON: `{"currency":"CNY","mode":"per_token","config":{"items":{"input":1,"output":2}}}`}}
	c.Set("supplier_routing_state", state)
	written := false
	c.Writer = &supplierCompletionWriter{ResponseWriter: c.Writer, onWrite: func() {
		written = true
		assert.True(t, state.Finished, "settle and release trial admission before the client can issue its next request")
		cancel() // A client may disconnect as soon as its successful response is delivered.
	}}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenAI, UpstreamModelName: "test"}, RelayFormat: types.RelayFormatOpenAI}
	resp := &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"OK"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":20,"total_tokens":30}}`))}
	_, apiErr := OpenaiHandler(c, info, resp)
	require.Nil(t, apiErr)
	require.True(t, written)
	service.FinishSupplierAttempt(c, info, nil, false)
	assert.Equal(t, "success", state.Current.Status)
	assert.Equal(t, "calculated", state.Current.CostStatus)
	assert.Equal(t, "0.00005000", state.Current.Cost)
}
