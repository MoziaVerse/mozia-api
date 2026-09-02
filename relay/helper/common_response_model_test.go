package helper

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStringDataOverridesResponseModel(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	common.SetContextKey(c, constant.ContextKeyUserVisibleModel, "source")

	require.NoError(t, StringData(c, `{"model":"target","extra":{"model":"nested"}}`))
	assert.Contains(t, recorder.Body.String(), `"model":"source"`)
	assert.Contains(t, recorder.Body.String(), `"extra":{"model":"nested"}`)
}
