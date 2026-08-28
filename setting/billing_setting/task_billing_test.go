package billing_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateTaskBillingJSONString(t *testing.T) {
	err := ValidateTaskBillingJSONString(`{
		"doubao/seedance-2.0-720p": {
			"version": 1,
			"mode": "per_second",
			"duration": {
				"paths": ["duration", "seconds", "metadata.duration"],
				"default": 5,
				"round": "ceil"
			}
		}
	}`)
	require.NoError(t, err)

	err = ValidateTaskBillingJSONString(`{
		"broken": {"version": 1, "mode": "per_second"}
	}`)
	assert.Error(t, err)
}

func TestValidateBillingModeJSONString(t *testing.T) {
	require.NoError(t, ValidateBillingModeJSONString(`{"m3":"tiered_expr","legacy":"ratio"}`))
	assert.Error(t, ValidateBillingModeJSONString(`{"m3":"unknown"}`))
	assert.Error(t, ValidateBillingModeJSONString(`{"":"tiered_expr"}`))
}

func TestValidateBillingExprJSONString(t *testing.T) {
	require.NoError(t, ValidateBillingExprJSONString(`{"m3":"len < 512000 ? tier(\"short\", p * 1.47 + c * 5.88 + cr * 0.294) : tier(\"long\", p * 2.94 + c * 11.76 + cr * 0.588)"}`))
	assert.Error(t, ValidateBillingExprJSONString(`{"m3":""}`))
	assert.Error(t, ValidateBillingExprJSONString(`{"m3":"p *"}`))
}
