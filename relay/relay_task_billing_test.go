package relay

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/taskbilling"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
)

func TestConfiguredTaskBillingQuotaAddsSurchargeBeforeGroupRatio(t *testing.T) {
	priceData := types.PriceData{
		ModelPrice:           0.1,
		OtherRatios:          map[string]float64{"duration": 15},
		TaskBillingSurcharge: &taskbilling.SurchargeResult{Price: 0.4},
		GroupRatioInfo:       types.GroupRatioInfo{GroupRatio: 1.2},
	}

	quota := configuredTaskBillingQuota(priceData)

	expectedPrice := (0.1*15 + 0.4) * 1.2
	assert.Equal(t, int(expectedPrice*common.QuotaPerUnit), quota)
}
