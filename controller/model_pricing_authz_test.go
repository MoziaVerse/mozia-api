package controller

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/billing_setting"
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
		"ModelPrice":          `{ "gpt-test": 1 }`,
		"VideoInputRatio":     `{ "video-test": 0.6 }`,
		"ReferenceVideoPrice": `{ "video-test": 0.08 }`,
		billing_setting.OfficialPricingOptionKey: `{
			"video-test": {
				"currency": "USD",
				"source_url": "https://example.com/pricing",
				"verified_at": "2026-08-31",
				"items": {"task:second": 0.12}
			}
		}`,
		"billing_setting.billing_mode": `{ "tiered-test": "tiered_expr" }`,
		"billing_setting.billing_expr": `{ "tiered-test": "tier(\"base\", p * 1.5 + c * 6)" }`,
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
	foundReferenceVideoPrice := false
	foundBillingMode := false
	foundBillingExpr := false
	foundOfficialPricing := false
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
		if option.Key == "ReferenceVideoPrice" {
			foundReferenceVideoPrice = true
			assert.JSONEq(t, `{ "video-test": 0.08 }`, option.Value)
		}
		if option.Key == "billing_setting.billing_mode" {
			foundBillingMode = true
			assert.JSONEq(t, `{ "tiered-test": "tiered_expr" }`, option.Value)
		}
		if option.Key == "billing_setting.billing_expr" {
			foundBillingExpr = true
			assert.JSONEq(t, `{ "tiered-test": "tier(\"base\", p * 1.5 + c * 6)" }`, option.Value)
		}
		if option.Key == billing_setting.OfficialPricingOptionKey {
			foundOfficialPricing = true
		}
	}
	assert.True(t, foundTaskBilling)
	assert.True(t, foundVideoInputRatio)
	assert.True(t, foundReferenceVideoPrice)
	assert.True(t, foundBillingMode)
	assert.True(t, foundBillingExpr)
	assert.True(t, foundOfficialPricing)
}

func TestUpdateModelPricingOptionRejectsInvalidBillingExpression(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodPut,
		"/api/mozia/model-pricing/",
		bytes.NewBufferString(`{"key":"billing_setting.billing_expr","value":"{\"m3\":\"p *\"}"}`),
	)

	UpdateModelPricingOption(ctx)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.False(t, response.Success)
	assert.Contains(t, response.Message, "模型计费表达式配置失败")
}

func TestValidateModelPricingOptionValue(t *testing.T) {
	assert.NoError(t, validateModelPricingOptionValue("ModelPrice", `{"video-model":0.9}`))
	assert.NoError(t, validateModelPricingOptionValue("VideoInputRatio", `{"video-model":1.5}`))
	assert.Error(t, validateModelPricingOptionValue("VideoInputRatio", `{"video-model":0}`))
	assert.Error(t, validateModelPricingOptionValue("ModelRatio", `{"video-model":-1}`))
	assert.Error(t, validateModelPricingOptionValue(billing_setting.BillingExprOptionKey, `{"video-model":"p *"}`))
	assert.Error(t, validateModelPricingOptionValue(billing_setting.OfficialPricingOptionKey, `{}`))
}
