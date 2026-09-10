package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/stretchr/testify/assert"
)

func TestResellerUsageQuotaDisplayPrecision(t *testing.T) {
	settings := operation_setting.GetGeneralSetting()
	original := *settings
	originalQuotaPerUnit, originalRate := common.QuotaPerUnit, operation_setting.USDExchangeRate
	t.Cleanup(func() {
		*settings = original
		common.QuotaPerUnit, operation_setting.USDExchangeRate = originalQuotaPerUnit, originalRate
	})
	common.QuotaPerUnit = 500_000
	operation_setting.USDExchangeRate = 7
	settings.CustomCurrencySymbol = "€"
	settings.CustomCurrencyExchangeRate = 2
	for _, test := range []struct {
		displayType string
		quota       int64
		want        string
	}{
		{operation_setting.QuotaDisplayTypeUSD, 9_007_199_254_740_993, "＄18014398509.481986"},
		{operation_setting.QuotaDisplayTypeUSD, 1, "＄0.000002"},
		{operation_setting.QuotaDisplayTypeCNY, -1, "¥-0.000014"},
		{operation_setting.QuotaDisplayTypeCustom, 1, "€0.000004"},
		{operation_setting.QuotaDisplayTypeTokens, 9_007_199_254_740_993, "9007199254740993"},
	} {
		t.Run(test.displayType+test.want, func(t *testing.T) {
			settings.QuotaDisplayType = test.displayType
			assert.Equal(t, test.want, formatResellerUsageQuota(test.quota))
		})
	}
	t.Run("custom currency defaults", func(t *testing.T) {
		settings.QuotaDisplayType = operation_setting.QuotaDisplayTypeCustom
		settings.CustomCurrencySymbol = ""
		settings.CustomCurrencyExchangeRate = 0
		assert.Equal(t, "¤0.000002", formatResellerUsageQuota(1))
	})
	assert.Nil(t, resellerUsageEarnings(100, 80, false))
	assert.Equal(t, "—", resellerUsageEarnings(0, 0, true).MarginRate)
	assert.Equal(t, "-50.00%", resellerUsageEarnings(100, 150, true).MarginRate)
}
