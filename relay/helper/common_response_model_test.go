package helper

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStreamResponseUsesPublicModel(t *testing.T) {
	for _, protocol := range []string{"openai", "claude raw", "claude converted", "responses"} {
		t.Run(protocol, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
			common.SetContextKey(c, constant.ContextKeyUserVisibleModel, "public-model")
			switch protocol {
			case "openai":
				require.NoError(t, StringData(c, `{"model":"upstream-alias","extra":{"model":"nested"}}`))
				assert.Equal(t, "data: {\"model\":\"public-model\",\"extra\":{\"model\":\"nested\"}}\n\n", recorder.Body.String())
			case "claude raw", "claude converted":
				data := `{"type":"message_start","message":{"model":"upstream-alias"}}`
				var response dto.ClaudeResponse
				require.NoError(t, common.Unmarshal([]byte(data), &response))
				if protocol == "claude raw" {
					ClaudeChunkData(c, response, data)
				} else {
					require.NoError(t, ClaudeData(c, response))
				}
				assert.Contains(t, recorder.Body.String(), "event: message_start\ndata: ")
			case "responses":
				ResponseChunkData(c, dto.ResponsesStreamResponse{Type: "response.completed"}, `{"type":"response.completed","response":{"model":"upstream-alias"}}`)
				assert.Contains(t, recorder.Body.String(), "event: response.completed\ndata: ")
			}
			assert.Contains(t, recorder.Body.String(), `"model":"public-model"`)
			assert.NotContains(t, recorder.Body.String(), "upstream-alias")
			Done(c)
			assert.Contains(t, recorder.Body.String(), "\n\ndata: [DONE]\n\n")
		})
	}
}
