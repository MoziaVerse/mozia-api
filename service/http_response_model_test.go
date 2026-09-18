package service

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestIOCopyBytesGracefullyOverridesResponseModel(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	common.SetContextKey(c, constant.ContextKeyUserVisibleModel, "vendor/public-model")

	IOCopyBytesGracefully(c, &http.Response{StatusCode: http.StatusCreated, Header: http.Header{"Content-Length": {"1"}}}, []byte(`{"model":"target","extra":{"model":"nested"}}`))

	assert.JSONEq(t, `{"model":"vendor/public-model","extra":{"model":"nested"}}`, recorder.Body.String())
	assert.Equal(t, http.StatusCreated, recorder.Code)
	assert.Equal(t, strconv.Itoa(recorder.Body.Len()), recorder.Result().Header.Get("Content-Length"))
}
