package model

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildPricingDisplayTimeTiers(t *testing.T) {
	const expression = `(weekday("Asia/Shanghai") >= 1 && weekday("Asia/Shanghai") <= 5 && ((hour("Asia/Shanghai") >= 9 && hour("Asia/Shanghai") < 12) || (hour("Asia/Shanghai") >= 14 && hour("Asia/Shanghai") < 18))) ? tier("peak", p * 3 + c * 9 + cr * 0.1) : tier("off_peak", p * 1.5 + c * 4.5 + cr * 0.05)`
	const schedule = "周一、周二、周三、周四、周五 09:00–12:00、14:00–18:00（北京时间）"
	for _, tc := range []struct {
		name, expression string
		customerRatio    float64
		prices           []float64
	}{
		{"flash", expression, 1, []float64{3, 9, 0.1, 1.5, 4.5, 0.05}},
		{"customer discount", expression, 0.5, []float64{1.5, 4.5, 0.05, 0.75, 2.25, 0.025}},
		{"zero customer rate", expression, 0, []float64{0, 0, 0, 0, 0, 0}},
		{"versioned reseller projection", "v1:(" + expression + ") * 1.2", 0.5, []float64{1.8, 5.4, 0.06, 0.9, 2.7, 0.03}},
		{"pro", strings.NewReplacer("p * 3", "p * 9", "c * 9", "c * 27", "cr * 0.1", "cr * 0.3", "p * 1.5", "p * 4.5", "c * 4.5", "c * 13.5", "cr * 0.05", "cr * 0.15").Replace(expression), 1, []float64{9, 27, 0.3, 4.5, 13.5, 0.15}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			display := BuildPricingDisplay(Pricing{
				ModelName: "time-pricing-display-test", BillingMode: billing_setting.BillingModeTieredExpr,
				BillingExpr: tc.expression,
			}, tc.customerRatio)
			require.Len(t, display.Items, 6)
			keys := []string{"input", "output", "cache_read"}
			for i, item := range display.Items {
				require.NotNil(t, item.OurAmountUSD)
				assert.InDelta(t, tc.prices[i], *item.OurAmountUSD, 1e-10)
				assert.Equal(t, "million_tokens", item.Unit)
				assert.Contains(t, item.Key, "token:"+keys[i%3]+":time_tier=")
				if i < 3 {
					assert.Contains(t, item.Item, "高峰")
					assert.Equal(t, schedule, item.Condition)
				} else {
					assert.Contains(t, item.Item, "空闲")
					assert.Equal(t, "除「"+schedule+"」以外的时段", item.Condition)
				}
				assert.Equal(t, "按结算时所在时段计费", item.Note)
			}
		})
	}
}

func TestBuildPricingDisplayRetainsDynamicForUnsupportedExpressions(t *testing.T) {
	for _, expression := range []string{
		`hour("Asia/Shanghai") < 12 ? tier("peak", p * c) : tier("off_peak", p * 1)`,
		`hour("Asia/Shanghai") < 12 ? tier("peak", p * 3 + 1) : tier("off_peak", p * 1)`,
		`hour("Asia/Shanghai") < 12 ? tier("peak", len * 3) : tier("off_peak", p * 1)`,
		`hour("Asia/Shanghai") < 12 ? tier("peak", p * -3) : tier("off_peak", p * 1)`,
		`hour("Asia/Shanghai") < 12 ? tier("peak", p * 3) : tier("off_peak", param("price") * p)`,
		`hour("Asia/Shanghai") < 12 ? tier("peak", p * 3) : tier("off_peak", p * 1)|||when(header("x-fast") has "yes") * 2`,
		`minute("Asia/Shanghai") < 30 ? tier("peak", p * 3) : tier("off_peak", p * 1)`,
		`hour("UTC") < 12 && weekday("Asia/Shanghai") == 1 ? tier("peak", p * 3) : tier("off_peak", p * 1)`,
		`hour("invalid/timezone") < 12 ? tier("peak", p * 3) : tier("off_peak", p * 1)`,
		`len < 512000 ? tier("short", p * 3) : tier("long", p * 6)`,
	} {
		t.Run(expression, func(t *testing.T) {
			display := BuildPricingDisplay(Pricing{
				ModelName: "dynamic-pricing-display-test", BillingMode: billing_setting.BillingModeTieredExpr,
				BillingExpr: expression,
			}, 1)
			require.Len(t, display.Items, 1)
			assert.Equal(t, "dynamic", display.Items[0].Key)
			assert.Nil(t, display.Items[0].OurAmountUSD)
		})
	}
}
