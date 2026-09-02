package service

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestIOCopyBytesGracefullyOverridesResponseModel(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	common.SetContextKey(c, constant.ContextKeyUserVisibleModel, "source")

	IOCopyBytesGracefully(c, &http.Response{StatusCode: http.StatusOK}, []byte(`{"model":"target","extra":{"model":"nested"}}`))

	assert.JSONEq(t, `{"model":"source","extra":{"model":"nested"}}`, recorder.Body.String())
}
