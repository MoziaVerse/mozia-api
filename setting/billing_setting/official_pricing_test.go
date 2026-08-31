package billing_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateOfficialPricingJSONString(t *testing.T) {
	require.NoError(t, ValidateOfficialPricingJSONString(`{
		"model": {
			"currency": "CNY",
			"source_url": "https://example.com/pricing",
			"verified_at": "2026-08-31",
			"items": {"token:input": 3, "token:output": 15}
		}
	}`))
	assert.Error(t, ValidateOfficialPricingJSONString(`{"model":{"currency":"EUR","source_url":"https://example.com","verified_at":"2026-08-31","items":{"token:input":3}}}`))
	assert.Error(t, ValidateOfficialPricingJSONString(`{"model":{"currency":"USD","source_url":"javascript:alert(1)","verified_at":"2026-08-31","items":{"token:input":3}}}`))
	assert.Error(t, ValidateOfficialPricingJSONString(`{"model":{"currency":"USD","source_url":"https://example.com","verified_at":"2026/08/31","items":{"token:input":3}}}`))
}
