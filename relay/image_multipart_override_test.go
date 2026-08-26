package relay

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyImageMultipartParamOverride(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "public-model"))
	require.NoError(t, writer.WriteField("prompt", "original prompt"))
	require.NoError(t, writer.WriteField("url", "https://example.com/one.png"))
	require.NoError(t, writer.WriteField("url", "https://example.com/two.png"))
	require.NoError(t, writer.WriteField("num_inference_steps", "20"))
	require.NoError(t, writer.Close())

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", &body)
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	form, err := common.ParseMultipartFormReusable(c)
	require.NoError(t, err)
	c.Request.MultipartForm = form

	request := &dto.ImageRequest{
		Model:             "upstream-model",
		Prompt:            "original prompt",
		N:                 common.GetPointer(uint(1)),
		NumInferenceSteps: common.GetPointer(20),
	}
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ParamOverride: map[string]interface{}{
		"response_format":     "b64_json",
		"num_inference_steps": 0,
	}}}

	require.NoError(t, applyImageMultipartParamOverride(c, info, request))
	formValues := url.Values(form.Value)
	assert.Equal(t, "upstream-model", request.Model)
	assert.Equal(t, "b64_json", request.ResponseFormat)
	require.NotNil(t, request.NumInferenceSteps)
	assert.Zero(t, *request.NumInferenceSteps)
	assert.Equal(t, "upstream-model", formValues.Get("model"))
	assert.Equal(t, "b64_json", formValues.Get("response_format"))
	assert.Equal(t, "0", formValues.Get("num_inference_steps"))
	assert.Equal(t, []string{"https://example.com/one.png", "https://example.com/two.png"}, form.Value["url"])
}

func TestApplyImageMultipartParamOverrideRequiresParsedForm(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", nil)

	err := applyImageMultipartParamOverride(c, &relaycommon.RelayInfo{}, &dto.ImageRequest{})
	assert.EqualError(t, err, "multipart form is not parsed")
}
