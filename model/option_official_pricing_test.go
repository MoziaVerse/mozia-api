package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeleteOfficialPricingPreservesOtherModels(t *testing.T) {
	originalOfficial, err := common.Marshal(billing_setting.GetOfficialPricingCopy())
	require.NoError(t, err)
	common.OptionMapRWMutex.Lock()
	originalOptionMap := common.OptionMap
	common.OptionMap = map[string]string{}
	common.OptionMapRWMutex.Unlock()
	require.NoError(t, DB.AutoMigrate(&Option{}))
	require.NoError(t, DB.Delete(&Option{}, "key = ?", billing_setting.OfficialPricingOptionKey).Error)
	t.Cleanup(func() {
		DB.Delete(&Option{}, "key = ?", billing_setting.OfficialPricingOptionKey)
		_ = updateOptionMap(billing_setting.OfficialPricingOptionKey, string(originalOfficial))
		common.OptionMapRWMutex.Lock()
		common.OptionMap = originalOptionMap
		common.OptionMapRWMutex.Unlock()
	})

	require.NoError(t, UpsertOfficialPricing("added", billing_setting.OfficialPricing{
		Currency: "USD", SourceURL: "https://example.com/pricing", VerifiedAt: "2026-08-31",
		Items: map[string]float64{"task:second": 0.2},
	}))
	value := `{
		"remove":{"currency":"USD"},
		"keep":{"currency":"CNY","future_field":true},
		"added":{"currency":"USD","source_url":"https://example.com/pricing","verified_at":"2026-08-31","items":{"task:second":0.2}}
	}`
	require.NoError(t, UpdateOption(billing_setting.OfficialPricingOptionKey, value))

	deleted, err := DeleteOfficialPricing("remove")
	require.NoError(t, err)
	assert.True(t, deleted)

	var option Option
	require.NoError(t, DB.First(&option, "key = ?", billing_setting.OfficialPricingOptionKey).Error)
	assert.JSONEq(t, `{
		"keep":{"currency":"CNY","future_field":true},
		"added":{"currency":"USD","source_url":"https://example.com/pricing","verified_at":"2026-08-31","items":{"task:second":0.2}}
	}`, option.Value)

	deleted, err = DeleteOfficialPricing("remove")
	require.NoError(t, err)
	assert.False(t, deleted)
}
