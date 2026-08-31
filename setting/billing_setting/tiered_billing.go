package billing_setting

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/QuantumNous/new-api/pkg/taskbilling"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/samber/lo"
)

const (
	BillingModeRatio         = "ratio"
	BillingModeTieredExpr    = "tiered_expr"
	BillingModeField         = "billing_mode"
	BillingExprField         = "billing_expr"
	BillingModeOptionKey     = "billing_setting.billing_mode"
	BillingExprOptionKey     = "billing_setting.billing_expr"
	TaskBillingField         = "task_billing"
	TaskBillingOptionKey     = "billing_setting.task_billing"
	OfficialPricingField     = "official_pricing"
	OfficialPricingOptionKey = "billing_setting.official_pricing"
)

// BillingSetting is managed by config.GlobalConfig.Register.
// DB keys: billing_setting.billing_mode, billing_setting.billing_expr,
// billing_setting.task_billing, billing_setting.official_pricing.
type BillingSetting struct {
	BillingMode     map[string]string             `json:"billing_mode"`
	BillingExpr     map[string]string             `json:"billing_expr"`
	TaskBilling     map[string]taskbilling.Config `json:"task_billing"`
	OfficialPricing map[string]OfficialPricing    `json:"official_pricing"`
}

var billingSetting = BillingSetting{
	BillingMode:     make(map[string]string),
	BillingExpr:     make(map[string]string),
	TaskBilling:     make(map[string]taskbilling.Config),
	OfficialPricing: make(map[string]OfficialPricing),
}

func init() {
	config.GlobalConfig.Register("billing_setting", &billingSetting)
}

// ---------------------------------------------------------------------------
// Read accessors (hot path, must be fast)
// ---------------------------------------------------------------------------

func GetBillingMode(model string) string {
	if mode, ok := billingSetting.BillingMode[model]; ok {
		return mode
	}
	return BillingModeRatio
}

func GetBillingExpr(model string) (string, bool) {
	expr, ok := billingSetting.BillingExpr[model]
	return expr, ok
}

func GetBillingModeCopy() map[string]string {
	return lo.Assign(billingSetting.BillingMode)
}

func GetBillingExprCopy() map[string]string {
	return lo.Assign(billingSetting.BillingExpr)
}

func GetTaskBilling(model string) (taskbilling.Config, bool) {
	config, ok := billingSetting.TaskBilling[model]
	return config, ok
}

func GetTaskBillingCopy() map[string]taskbilling.Config {
	return lo.Assign(billingSetting.TaskBilling)
}

func GetPricingSyncData(base map[string]any) map[string]any {
	extra := make(map[string]any, 4)
	if modes := GetBillingModeCopy(); len(modes) > 0 {
		extra[BillingModeField] = modes
	}
	if exprs := GetBillingExprCopy(); len(exprs) > 0 {
		extra[BillingExprField] = exprs
	}
	if configs := GetTaskBillingCopy(); len(configs) > 0 {
		extra[TaskBillingField] = configs
	}
	if configs := GetOfficialPricingCopy(); len(configs) > 0 {
		extra[OfficialPricingField] = configs
	}
	return lo.Assign(base, extra)
}

// ValidateTaskBillingJSONString validates the full model -> task billing map
// before the option is persisted. Empty maps are valid and preserve legacy
// adaptor-based EstimateBilling behavior.
func ValidateTaskBillingJSONString(raw string) error {
	configs := make(map[string]taskbilling.Config)
	if err := common.UnmarshalJsonStr(raw, &configs); err != nil {
		return fmt.Errorf("invalid task billing JSON: %w", err)
	}
	for model, config := range configs {
		if strings.TrimSpace(model) == "" {
			return fmt.Errorf("task billing model name is required")
		}
		if err := taskbilling.Validate(config); err != nil {
			return fmt.Errorf("task billing for model %q: %w", model, err)
		}
	}
	return nil
}

func ValidateBillingModeJSONString(raw string) error {
	modes := make(map[string]string)
	if err := common.UnmarshalJsonStr(raw, &modes); err != nil {
		return fmt.Errorf("invalid billing mode JSON: %w", err)
	}
	for model, mode := range modes {
		if strings.TrimSpace(model) == "" {
			return fmt.Errorf("billing mode model name is required")
		}
		if mode != BillingModeRatio && mode != BillingModeTieredExpr {
			return fmt.Errorf("billing mode for model %q is invalid: %q", model, mode)
		}
	}
	return nil
}

func ValidateBillingExprJSONString(raw string) error {
	expressions := make(map[string]string)
	if err := common.UnmarshalJsonStr(raw, &expressions); err != nil {
		return fmt.Errorf("invalid billing expression JSON: %w", err)
	}
	for model, expression := range expressions {
		if strings.TrimSpace(model) == "" {
			return fmt.Errorf("billing expression model name is required")
		}
		if strings.TrimSpace(expression) == "" {
			return fmt.Errorf("billing expression for model %q is required", model)
		}
		if err := SmokeTestExpr(expression); err != nil {
			return fmt.Errorf("billing expression for model %q: %w", model, err)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Smoke test (called externally for validation before save)
// ---------------------------------------------------------------------------

func SmokeTestExpr(exprStr string) error {
	return smokeTestExpr(exprStr)
}

func smokeTestExpr(exprStr string) error {
	vectors := []billingexpr.TokenParams{
		{P: 0, C: 0, Len: 0},
		{P: 1000, C: 1000, Len: 1000},
		{P: 100000, C: 100000, Len: 100000},
		{P: 1000000, C: 1000000, Len: 1000000},
	}
	requests := []billingexpr.RequestInput{
		{},
		{
			Headers: map[string]string{
				"anthropic-beta": "fast-mode-2026-02-01",
			},
			Body: []byte(`{"service_tier":"fast","stream_options":{"include_usage":true},"messages":[1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21]}`),
		},
	}

	for _, v := range vectors {
		for _, request := range requests {
			result, _, err := billingexpr.RunExprWithRequest(exprStr, v, request)
			if err != nil {
				return fmt.Errorf("vector {p=%g, c=%g}: run failed: %w", v.P, v.C, err)
			}
			if result < 0 {
				return fmt.Errorf("vector {p=%g, c=%g}: result %f < 0", v.P, v.C, result)
			}
		}
	}
	return nil
}
