package controller

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestStripThinkingForModelRedirect(t *testing.T) {
	request := &dto.GeneralOpenAIRequest{THINKING: json.RawMessage(`{"type":"disabled"}`)}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	stripThinkingForModelRedirect(c, request)
	assert.NotNil(t, request.THINKING)

	common.SetContextKey(c, constant.ContextKeyModelRedirectApplied, true)
	stripThinkingForModelRedirect(c, request)
	assert.Nil(t, request.THINKING)
}
