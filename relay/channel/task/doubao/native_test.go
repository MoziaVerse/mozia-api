package doubao

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNativeVideoSubmitPreservesArkContract(t *testing.T) {
	const input = `{"model":"public-model","content":[{"type":"text","text":"animate"},{"type":"video_url","video_url":{"url":"https://example.com/a.mp4"},"role":"reference_video"}],"duration":-1,"seed":9007199254740993,"generate_audio":false,"watermark":false,"priority":0,"future_field":{"enabled":false,"values":[0,null]}}`
	var upstreamBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, constant.VolcengineVideoTaskPath, r.URL.Path)
		assert.Equal(t, "Bearer submission-key", r.Header.Get("Authorization"))
		var err error
		upstreamBody, err = io.ReadAll(r.Body)
		assert.NoError(t, err)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"cgt-native","future_response":true}`)
	}))
	defer server.Close()
	service.InitHttpClient()

	for _, suffix := range []string{"", "/v1/", "/api/v3/"} {
		t.Run(suffix, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, constant.VolcengineVideoTaskPath, strings.NewReader(input))
			c.Request.Header.Set("Content-Type", "application/json")
			info := &relaycommon.RelayInfo{
				UserId: 42, TaskRelayInfo: &relaycommon.TaskRelayInfo{},
				ChannelMeta: &relaycommon.ChannelMeta{
					ChannelType: constant.ChannelTypeMoziaArtsapi, ChannelBaseUrl: server.URL + suffix,
					ApiKey: "submission-key", UpstreamModelName: "upstream-model",
				},
			}
			adaptor := &NativeTaskAdaptor{}
			adaptor.Init(info)
			require.Nil(t, adaptor.ValidateRequestAndSetAction(c, info))
			body, err := adaptor.BuildRequestBody(c, info)
			require.NoError(t, err)
			resp, err := adaptor.DoRequest(c, info, body)
			require.NoError(t, err)
			id, raw, taskErr := adaptor.DoResponse(c, resp, info)
			require.Nil(t, taskErr)
			assert.Equal(t, "cgt-native", id)
			assert.Equal(t, `{"id":"cgt-native","future_response":true}`, string(raw))
			assert.Empty(t, recorder.Body.String(), "controller must persist before writing success")
			task := model.InitTask(constant.TaskPlatformVolcengineVideo, info)
			assert.Equal(t, id, task.TaskID)
			assert.Equal(t, "submission-key", task.PrivateData.Key)

			var got, want map[string]json.RawMessage
			require.NoError(t, common.Unmarshal(upstreamBody, &got))
			require.NoError(t, common.Unmarshal([]byte(input), &want))
			want["model"] = json.RawMessage(`"upstream-model"`)
			assert.Equal(t, want, got, "only the model mapping may change the request")
		})
	}
}

func TestNativeVideoValidationAndStatus(t *testing.T) {
	for _, tc := range []struct {
		body  string
		valid bool
	}{
		{`{"model":"m","content":[{"type":"image_url","image_url":{"url":"https://example.com/f.png"}}]}`, true},
		{`{"model":"m","content":[{"type":"text","text":"hi"}],"seed":0,"duration":-1}`, true},
		{`{"model":"m","content":[]}`, false},
		{`{"model":" ","content":[{"type":"text","text":"hi"}]}`, false},
		{`{"model":"m","content":[{}]}`, false},
		{`{"model":"m","content":[{"type":"text"}],"duration":"wrong"}`, false},
	} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, constant.VolcengineVideoTaskPath, strings.NewReader(tc.body))
		c.Request.Header.Set("Content-Type", "application/json")
		info := &relaycommon.RelayInfo{TaskRelayInfo: &relaycommon.TaskRelayInfo{}, ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeMoziaArtsapi}}
		err := (&NativeTaskAdaptor{}).ValidateRequestAndSetAction(c, info)
		if tc.valid {
			require.Nil(t, err, tc.body)
			body, buildErr := (&NativeTaskAdaptor{}).BuildRequestBody(c, info)
			require.NoError(t, buildErr)
			data, readErr := io.ReadAll(body)
			require.NoError(t, readErr)
			if !strings.Contains(tc.body, "duration") {
				assert.NotContains(t, string(data), "duration")
			}
		} else {
			require.NotNil(t, err, tc.body)
			assert.Equal(t, http.StatusBadRequest, err.StatusCode)
		}
	}
	for upstream, expected := range map[string]string{
		"queued": model.TaskStatusQueued, "running": model.TaskStatusInProgress,
		"succeeded": model.TaskStatusSuccess, "failed": model.TaskStatusFailure,
		"cancelled": model.TaskStatusFailure, "expired": model.TaskStatusFailure,
	} {
		raw := []byte(`{"id":"cgt-native","status":"` + upstream + `","content":{"video_url":"https://example.com/video.mp4"},"usage":{"completion_tokens":123}}`)
		result, err := (&NativeTaskAdaptor{}).ParseTaskResult(raw)
		require.NoError(t, err)
		assert.Equal(t, expected, result.Status)
		if upstream == "succeeded" {
			assert.Equal(t, 123, result.TotalTokens)
			assert.Equal(t, "https://example.com/video.mp4", result.Url)
		}
	}
	_, err := (&NativeTaskAdaptor{}).ParseTaskResult([]byte(`{"error":{"code":"429"}}`))
	require.Error(t, err, "an API error must not trigger a task refund")
}
