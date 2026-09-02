package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/taskbilling"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildPricingDisplayShowsFinalPerSecondAndOfficialPrices(t *testing.T) {
	originalOfficial, err := common.Marshal(billing_setting.GetOfficialPricingCopy())
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, config.UpdateConfigFromMap(config.GlobalConfig.Get("billing_setting"), map[string]string{
			"official_pricing": string(originalOfficial),
		}))
	})
	require.NoError(t, config.UpdateConfigFromMap(config.GlobalConfig.Get("billing_setting"), map[string]string{
		"official_pricing": `{
			"video-model": {
				"currency": "USD",
				"source_url": "https://example.com/pricing",
				"verified_at": "2026-08-31",
				"items": {
					"task:second:resolution=480p": 0.6,
					"task:second:resolution=720p": 1.3
				}
			}
		}`,
	}))

	display := BuildPricingDisplay(Pricing{
		ModelName:  "video-model",
		QuotaType:  1,
		ModelPrice: 0.462,
		TaskBilling: &taskbilling.Config{
			Version: 1,
			Mode:    taskbilling.ModeParametric,
			Dimensions: []taskbilling.Dimension{
				{Name: "duration", Kind: taskbilling.DimensionNumber, Default: 5, Unit: 1, Round: taskbilling.RoundCeil},
				{Name: "resolution", Kind: taskbilling.DimensionEnum, Default: "720p", Values: map[string]float64{
					"480p": 1,
					"720p": 2.1515151515151514,
				}},
			},
		},
	}, 1)

	require.Equal(t, PricingDisplayVersion, display.Version)
	require.Len(t, display.Items, 2)
	assert.Equal(t, "task:second:resolution=480p", display.Items[0].Key)
	assert.InDelta(t, 0.462, *display.Items[0].OurAmountUSD, 0.000001)
	assert.InDelta(t, 0.6, *display.Items[0].OfficialAmountUSD, 0.000001)
	assert.Equal(t, "task:second:resolution=720p", display.Items[1].Key)
	assert.InDelta(t, 0.994, *display.Items[1].OurAmountUSD, 0.000001)
}

func TestBuildPricingDisplayShowsTokenParametricMatrix(t *testing.T) {
	referencePrice := 21.0
	display := BuildPricingDisplay(Pricing{
		ModelName: "seedance-token-parametric-display-test",
		TaskBilling: &taskbilling.Config{
			Version: taskbilling.Version1,
			Mode:    taskbilling.ModeTokenParametric,
			TokenPrices: &taskbilling.TokenPriceTable{
				Paths: []string{"resolution"},
				Values: map[string]taskbilling.TokenUnitPrice{
					"480p": {Standard: 34.5, ReferenceVideo: &referencePrice},
				},
			},
		},
	}, 0.5)

	require.Equal(t, PricingDisplayVersion, display.Version)
	require.Len(t, display.Items, 2)
	assert.Equal(t, "task:tokens:resolution=480p:reference_video=false", display.Items[0].Key)
	assert.Empty(t, display.Items[0].Note)
	assert.InDelta(t, 17.25, *display.Items[0].OurAmountUSD, 0.000001)
	assert.Equal(t, "task:tokens:resolution=480p:reference_video=true", display.Items[1].Key)
	assert.Empty(t, display.Items[1].Note)
	assert.InDelta(t, 10.5, *display.Items[1].OurAmountUSD, 0.000001)
}

func TestOfficialPriceToUSDUsesConfiguredExchangeRate(t *testing.T) {
	originalRate := operation_setting.USDExchangeRate
	operation_setting.USDExchangeRate = 7.2
	t.Cleanup(func() { operation_setting.USDExchangeRate = originalRate })

	amount, ok := officialPriceToUSD(7.2, "CNY")
	require.True(t, ok)
	assert.InDelta(t, 1, amount, 0.000001)
}

func TestBuildPricingDisplaySupportsTokenVideoAndSurchargeItems(t *testing.T) {
	originalVideoRatios := ratio_setting.VideoInputRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateVideoInputRatioByJSONString(originalVideoRatios))
	})
	require.NoError(t, ratio_setting.UpdateVideoInputRatioByJSONString(`{"token-video":0.5}`))

	tokenDisplay := BuildPricingDisplay(Pricing{
		ModelName: "token-video", QuotaType: 0, ModelRatio: 2, CompletionRatio: 3,
	}, 1)
	require.Len(t, tokenDisplay.Items, 4)
	assert.Equal(t, "token:input:reference_video=false", tokenDisplay.Items[0].Key)
	assert.InDelta(t, 4, *tokenDisplay.Items[0].OurAmountUSD, 0.000001)
	assert.Equal(t, "token:input:reference_video=true", tokenDisplay.Items[2].Key)
	assert.InDelta(t, 2, *tokenDisplay.Items[2].OurAmountUSD, 0.000001)

	surchargeDisplay := BuildPricingDisplay(Pricing{
		ModelName: "h3", QuotaType: 1, ModelPrice: 0.9,
		TaskBilling: &taskbilling.Config{
			Version: 1, Mode: taskbilling.ModePerSecond,
			Duration:  &taskbilling.Dimension{Name: "duration", Kind: taskbilling.DimensionNumber, Default: 5, Round: taskbilling.RoundCeil},
			Surcharge: &taskbilling.Surcharge{Name: "input_images", Kind: taskbilling.SurchargeItemCount, FreeCount: 5, UnitPrice: 0.2},
		},
	}, 1)
	require.Len(t, surchargeDisplay.Items, 2)
	assert.Equal(t, "task:second", surchargeDisplay.Items[0].Key)
	assert.Equal(t, "surcharge:input_images", surchargeDisplay.Items[1].Key)
	assert.Equal(t, 5, *surchargeDisplay.Items[1].FreeCount)
	assert.InDelta(t, 0.2, *surchargeDisplay.Items[1].OurAmountUSD, 0.000001)
}

func TestPricingOfficialDiscountRequiresOneUniformCompleteRate(t *testing.T) {
	originalOfficial, err := common.Marshal(billing_setting.GetOfficialPricingCopy())
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, config.UpdateConfigFromMap(config.GlobalConfig.Get("billing_setting"), map[string]string{
			"official_pricing": string(originalOfficial),
		}))
	})
	require.NoError(t, config.UpdateConfigFromMap(config.GlobalConfig.Get("billing_setting"), map[string]string{
		"official_pricing": `{
			"discount-model": {
				"currency":"USD",
				"source_url":"https://example.com/pricing",
				"verified_at":"2026-08-31",
				"items":{"token:input":2,"token:output":4}
			}
		}`,
	}))
	pricing := Pricing{ModelName: "discount-model", QuotaType: 0, ModelRatio: 0.5, CompletionRatio: 2}

	reference, baseRate := pricingOfficialDiscount(pricing)
	require.NotNil(t, reference)
	assert.True(t, reference.Editable)
	assert.Equal(t, "5", reference.BaseMin)
	assert.InDelta(t, 5, baseRate, 0.000001)
	multiplier, err := resellerMultiplierFromOfficialDiscount(pricing, "7.5")
	require.NoError(t, err)
	assert.Equal(t, int64(1_500_000), multiplier)

	require.NoError(t, config.UpdateConfigFromMap(config.GlobalConfig.Get("billing_setting"), map[string]string{
		"official_pricing": `{
			"discount-model": {
				"currency":"USD",
				"source_url":"https://example.com/pricing",
				"verified_at":"2026-08-31",
				"items":{"token:input":2,"token:output":5}
			}
		}`,
	}))
	reference, _ = pricingOfficialDiscount(pricing)
	assert.False(t, reference.Editable)
}
