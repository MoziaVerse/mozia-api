package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateModelPricingOptionRejectsNonPricingKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodPut,
		"/api/mozia/model-pricing/",
		bytes.NewBufferString(`{"key":"SMTPToken","value":"secret"}`),
	)

	UpdateModelPricingOption(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool `json:"success"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.False(t, response.Success)
}

func TestGetModelPricingOptionsExposesOnlyPricingKeys(t *testing.T) {
	common.OptionMapRWMutex.Lock()
	original := common.OptionMap
	common.OptionMap = map[string]string{
		"ModelPrice":                   `{ "gpt-test": 1 }`,
		"VideoInputRatio":              `{ "video-test": 0.6 }`,
		"billing_setting.task_billing": `{ "video-test": { "version": 1, "mode": "per_request" } }`,
		"SMTPToken":                    "must-not-leak",
	}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = original
		common.OptionMapRWMutex.Unlock()
	})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/mozia/model-pricing/", nil)

	GetModelPricingOptions(ctx)

	var response struct {
		Success bool            `json:"success"`
		Data    []*model.Option `json:"data"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	require.True(t, response.Success)
	foundTaskBilling := false
	foundVideoInputRatio := false
	for _, option := range response.Data {
		assert.NotEqual(t, "SMTPToken", option.Key)
		assert.NotEqual(t, "must-not-leak", option.Value)
		if option.Key == "billing_setting.task_billing" {
			foundTaskBilling = true
			assert.JSONEq(t, `{ "video-test": { "version": 1, "mode": "per_request" } }`, option.Value)
		}
		if option.Key == "VideoInputRatio" {
			foundVideoInputRatio = true
			assert.JSONEq(t, `{ "video-test": 0.6 }`, option.Value)
		}
	}
	assert.True(t, foundTaskBilling)
	assert.True(t, foundVideoInputRatio)
}
