package channel

import (
	"bytes"
	"crypto/tls"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	apicommon "github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/hpack"
)

type retryTestAdaptor struct {
	Adaptor
	url string
}

func (a retryTestAdaptor) GetRequestURL(*relaycommon.RelayInfo) (string, error) {
	return a.url, nil
}

func (a retryTestAdaptor) SetupRequestHeader(_ *gin.Context, header *http.Header, _ *relaycommon.RelayInfo) error {
	header.Set("Content-Type", "application/json")
	return nil
}

type retryTestTaskAdaptor struct {
	TaskAdaptor
	url string
}

func (a retryTestTaskAdaptor) BuildRequestURL(*relaycommon.RelayInfo) (string, error) {
	return a.url, nil
}

func (a retryTestTaskAdaptor) BuildRequestHeader(_ *gin.Context, req *http.Request, _ *relaycommon.RelayInfo) error {
	req.Header.Set("Content-Type", "application/json")
	return nil
}

func TestDoApiRequestHTTP2Retry(t *testing.T) {
	for _, tc := range []struct {
		name         string
		disk         bool
		task         bool
		reset        http2.ErrCode
		afterHeaders bool
		status       int
		attempts     int
	}{
		{name: "memory body survives protocol reset", reset: http2.ErrCodeProtocol, status: 200, attempts: 2},
		{name: "disk body survives protocol reset", disk: true, reset: http2.ErrCodeProtocol, status: 200, attempts: 2},
		{name: "task preserves native body replay", task: true, reset: http2.ErrCodeProtocol, status: 200, attempts: 2},
		{name: "HTTP 500 is not retried", status: 500, attempts: 1},
		{name: "internal stream error is not retried", reset: http2.ErrCodeInternal, attempts: 1},
		{name: "reset after response headers is not retried", reset: http2.ErrCodeProtocol, afterHeaders: true, status: 200, attempts: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Send real HTTP/2 frames so net/http, rather than a mock retry loop,
			// decides whether a POST may be replayed after its body was sent.
			var attempts atomic.Int32
			bodies := make(chan []byte, 8)
			server := httptest.NewUnstartedServer(nil)
			server.EnableHTTP2 = true
			server.Config.TLSNextProto = map[string]func(*http.Server, *tls.Conn, http.Handler){
				"h2": func(_ *http.Server, conn *tls.Conn, _ http.Handler) {
					defer conn.Close()
					preface := make([]byte, len(http2.ClientPreface))
					if _, err := io.ReadFull(conn, preface); !assert.NoError(t, err) {
						return
					}
					if !assert.Equal(t, http2.ClientPreface, string(preface)) {
						return
					}
					framer := http2.NewFramer(conn, conn)
					if !assert.NoError(t, framer.WriteSettings()) {
						return
					}
					var body bytes.Buffer
					for {
						frame, err := framer.ReadFrame()
						if err != nil {
							return
						}
						switch f := frame.(type) {
						case *http2.SettingsFrame:
							if !f.IsAck() && !assert.NoError(t, framer.WriteSettingsAck()) {
								return
							}
						case *http2.DataFrame:
							body.Write(f.Data())
							if !f.StreamEnded() {
								continue
							}
							bodies <- bytes.Clone(body.Bytes())
							body.Reset()
							attempt := attempts.Add(1)
							if attempt == 1 && tc.reset != http2.ErrCodeNo && !tc.afterHeaders {
								if !assert.NoError(t, framer.WriteRSTStream(f.StreamID, tc.reset)) {
									return
								}
								continue
							}
							var block bytes.Buffer
							encoder := hpack.NewEncoder(&block)
							if !assert.NoError(t, encoder.WriteField(hpack.HeaderField{Name: ":status", Value: strconv.Itoa(tc.status)})) {
								return
							}
							if !assert.NoError(t, framer.WriteHeaders(http2.HeadersFrameParam{
								StreamID: f.StreamID, BlockFragment: block.Bytes(), EndHeaders: true,
							})) {
								return
							}
							if tc.afterHeaders {
								assert.NoError(t, framer.WriteRSTStream(f.StreamID, tc.reset))
							} else {
								assert.NoError(t, framer.WriteData(f.StreamID, true, []byte(`{"ok":true}`)))
							}
						}
					}
				},
			}
			server.StartTLS()
			t.Cleanup(server.Close)

			service.InitHttpClient()
			client := service.GetHttpClient()
			originalClient := *client
			transport := server.Client().Transport.(*http.Transport).Clone()
			transport.ForceAttemptHTTP2 = true
			client.Transport = transport
			client.Timeout = 5 * time.Second
			t.Cleanup(func() {
				transport.CloseIdleConnections()
				*client = originalClient
			})

			originalConfig := apicommon.GetDiskCacheConfig()
			apicommon.SetDiskCacheConfig(apicommon.DiskCacheConfig{
				Enabled: tc.disk, ThresholdMB: 0, MaxSizeMB: 1, Path: t.TempDir(),
			})
			t.Cleanup(func() { apicommon.SetDiskCacheConfig(originalConfig) })
			payload := []byte(`{"model":"kimi-k3-fireworks","messages":[{"role":"user","content":"test"}]}`)
			var body io.Reader = bytes.NewReader(payload)
			size := int64(len(payload))
			if !tc.task {
				storedBody, storedSize, closer, err := relaycommon.NewOutboundJSONBody(payload)
				require.NoError(t, err)
				t.Cleanup(func() { require.NoError(t, closer.Close()) })
				require.Equal(t, tc.disk, closer.(apicommon.BodyStorage).IsDisk())
				body, size = storedBody, storedSize
			}

			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			info := &relaycommon.RelayInfo{
				ChannelMeta: &relaycommon.ChannelMeta{}, UpstreamRequestBodySize: size,
			}
			var resp *http.Response
			var err error
			if tc.task {
				resp, err = DoTaskApiRequest(retryTestTaskAdaptor{url: server.URL + "/v1/tasks"}, ctx, info, body)
			} else {
				resp, err = DoApiRequest(retryTestAdaptor{url: server.URL + "/v1/chat/completions"}, ctx, info, body)
			}
			if tc.reset == http2.ErrCodeInternal {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				defer resp.Body.Close()
				assert.Equal(t, tc.status, resp.StatusCode)
				data, readErr := io.ReadAll(resp.Body)
				if tc.afterHeaders {
					require.Error(t, readErr)
				} else {
					require.NoError(t, readErr)
					assert.JSONEq(t, `{"ok":true}`, string(data))
				}
			}
			require.EqualValues(t, tc.attempts, attempts.Load())
			for range tc.attempts {
				assert.Equal(t, payload, <-bodies)
			}
		})
	}
}
