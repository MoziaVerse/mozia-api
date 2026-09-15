package openai

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpenaiTTSHandlerSetsContentLengthForBufferedAudio(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/audio/speech", nil)
	body := []byte("complete-audio-payload")
	upstream := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"audio/wav"}},
		Body:       io.NopCloser(bytes.NewReader(body)),
	}

	OpenaiTTSHandler(c, upstream, &relaycommon.RelayInfo{
		Request: &dto.AudioRequest{ResponseFormat: "wav"},
	})

	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "audio/wav", recorder.Header().Get("Content-Type"))
	require.Equal(t, "22", recorder.Header().Get("Content-Length"))
	require.Equal(t, body, recorder.Body.Bytes())
}
