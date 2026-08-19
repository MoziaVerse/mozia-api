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
