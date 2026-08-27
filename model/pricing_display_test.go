package model

import (
	"testing"

	"github.com/QuantumNous/new-api/pkg/taskbilling"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildPricingDisplayShowsReferenceVideoTokenVariants(t *testing.T) {
	original := ratio_setting.VideoInputRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateVideoInputRatioByJSONString(original))
	})
	require.NoError(t, ratio_setting.UpdateVideoInputRatioByJSONString(`{"seedance-display-test":0.6}`))

	cacheRatio := 0.1
	display := BuildPricingDisplay(Pricing{
		ModelName:       "seedance-display-test",
		QuotaType:       0,
		ModelRatio:      10,
		CompletionRatio: 2,
		CacheRatio:      &cacheRatio,
	}, 0.5)

	require.Len(t, display.Rows, 6)
	assert.Equal(t, PricingDisplayVersion, display.Version)
	assert.Equal(t, "不含参考视频", display.Rows[0].Condition)
	assert.InDelta(t, 10, *display.Rows[0].AmountUSD, 0.000001)
	assert.InDelta(t, 20, *display.Rows[1].AmountUSD, 0.000001)
	assert.InDelta(t, 1, *display.Rows[2].AmountUSD, 0.000001)
	assert.Equal(t, "包含参考视频", display.Rows[3].Condition)
	assert.InDelta(t, 6, *display.Rows[3].AmountUSD, 0.000001)
	assert.InDelta(t, 12, *display.Rows[4].AmountUSD, 0.000001)
	assert.InDelta(t, 0.6, *display.Rows[5].AmountUSD, 0.000001)
}

func TestBuildPricingDisplayShowsPerSecondDefaultAndSurcharge(t *testing.T) {
	display := BuildPricingDisplay(Pricing{
		ModelName:  "minimax-h3-2k-display-test",
		QuotaType:  1,
		ModelPrice: 0.9,
		TaskBilling: &taskbilling.Config{
			Version: taskbilling.Version1,
			Mode:    taskbilling.ModePerSecond,
			Duration: &taskbilling.Dimension{
				Name: "duration", Kind: taskbilling.DimensionNumber,
				Paths: []string{"duration"}, Default: float64(5), Unit: 1, Round: taskbilling.RoundCeil,
			},
			Surcharge: &taskbilling.Surcharge{
				Name: "input_images", Kind: taskbilling.SurchargeItemCount,
				Paths: []string{"input_images"}, FreeCount: 5, UnitPrice: 0.2,
			},
		},
	}, 2)

	require.Len(t, display.Rows, 2)
	base := display.Rows[0]
	assert.Equal(t, "视频生成", base.Item)
	assert.Equal(t, "second", base.Unit)
	assert.InDelta(t, 1.8, *base.AmountUSD, 0.000001)
	assert.Contains(t, base.Note, "未传时默认 5")
	assert.Contains(t, base.Note, "向上取整")

	surcharge := display.Rows[1]
	assert.Equal(t, "附加图片素材", surcharge.Item)
	assert.Equal(t, "超过 5 个后开始计费", surcharge.Condition)
	assert.Equal(t, "前 5 个免费，超出部分逐个收费", surcharge.Note)
	assert.InDelta(t, 0.4, *surcharge.AmountUSD, 0.000001)
}

func TestBuildPricingDisplayProjectsAbsoluteReferenceVideoPrice(t *testing.T) {
	original := ratio_setting.ReferenceVideoPrice2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateReferenceVideoPriceByJSONString(original))
	})
	require.NoError(t, ratio_setting.UpdateReferenceVideoPriceByJSONString(`{"fixed-video-display-test":0.1}`))

	display := BuildPricingDisplay(Pricing{
		ModelName:               "fixed-video-display-test",
		QuotaType:               1,
		ModelPrice:              0.3,
		customerPriceMultiplier: 1.5,
	}, 0.5)

	require.Len(t, display.Rows, 2)
	assert.Equal(t, "不含参考视频", display.Rows[0].Condition)
	assert.InDelta(t, 0.15, *display.Rows[0].AmountUSD, 0.000001)
	assert.Equal(t, "包含参考视频", display.Rows[1].Condition)
	assert.InDelta(t, 0.075, *display.Rows[1].AmountUSD, 0.000001)
}

func TestBuildPricingDisplayKeepsExpressionPricingExplicitlyDynamic(t *testing.T) {
	display := BuildPricingDisplay(Pricing{
		ModelName:   "tiered-display-test",
		BillingMode: "tiered_expr",
		BillingExpr: "tier('long', len > 1000, p * 2 + c * 8)",
	}, 1)

	require.Len(t, display.Rows, 1)
	assert.Equal(t, "dynamic", display.Rows[0].Kind)
	assert.Nil(t, display.Rows[0].AmountUSD)
	assert.Contains(t, display.Rows[0].Note, "实际账单")
}
