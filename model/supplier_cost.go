package model

import (
	"fmt"
	"math"
	"slices"

	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// Internal snapshot mode, never an editable procurement quote or a currency.
const SupplierCostModeSelfHosted = "self_hosted"

func SupplierSelfHostedPrice(channelID int, model string) ChannelCostPricing {
	return ChannelCostPricing{ChannelId: channelID, ModelName: model, Mode: SupplierCostModeSelfHosted}
}

func DefaultSupplierAdaptiveHealth() SupplierHealthPolicy {
	return SupplierHealthPolicy{WindowSeconds: 300, MinSamples: 20, FailurePercent: 20, SuccessPercent: 95, MaxTTFTMs: 5000, MinThroughput: 10, CooldownSeconds: 30, TrialPercent: 5, TrialConcurrency: 1, ReferenceOutputTokens: 512}
}

func ValidateSupplierAdaptiveRule(r *SupplierRoutingRule) error {
	if r.Mode != "adaptive" {
		return nil
	}
	if r.MaxSupplierPercent != 0 {
		return SupplierFieldError("max_supplier_percent", "adaptive routing does not use supplier share caps")
	}
	for _, t := range r.Targets {
		if t.Weight != 100 {
			return SupplierFieldError("targets", "adaptive targets use neutral weight 100; procurement costs determine traffic")
		}
	}
	h := r.Health
	if h.WindowSeconds != 300 {
		return SupplierFieldError("health.window_seconds", "adaptive metrics use a five-minute window")
	}
	if h.SuccessPercent < 1 || h.SuccessPercent > 100 || h.SuccessPercent <= 100-h.FailurePercent {
		return SupplierFieldError("health.success_percent", "success threshold must be above the circuit-breaker threshold and at most 100")
	}
	if h.MinThroughput < 1 || h.MinThroughput > 100000 {
		return SupplierFieldError("health.min_throughput", "minimum throughput must be 1..100000 tokens/s")
	}
	if h.ReferenceOutputTokens < 1 || h.ReferenceOutputTokens > 10000000 {
		return SupplierFieldError("health.reference_output_tokens", "reference output must be 1..10000000 tokens")
	}
	if h.TrialConcurrency < 1 || h.TrialConcurrency > 100 {
		return SupplierFieldError("health.trial_concurrency", "trial concurrency must be 1..100")
	}
	return nil
}

// Procurement amounts stay decimal until conversion to a bounded scheduling weight.
func SupplierPriceAmount(cost ChannelCostPricing, input, output, cached int64) (decimal.Decimal, error) {
	if input < 0 || output < 0 || cached < 0 || cached > input {
		return decimal.Zero, fmt.Errorf("invalid supplier usage")
	}
	if cost.Mode == SupplierCostModeSelfHosted {
		return decimal.Zero, nil
	}
	if cost.Currency != "CNY" && cost.Currency != "USD" {
		return decimal.Zero, fmt.Errorf("supplier currency must be CNY or USD")
	}
	valid := func(v float64) bool { return v >= 0 && !math.IsNaN(v) && !math.IsInf(v, 0) }
	if cost.Mode == ChannelCostModePerRequest && cost.Config.BasePrice != nil && valid(*cost.Config.BasePrice) {
		return decimal.NewFromFloat(*cost.Config.BasePrice), nil
	}
	if cost.Mode != ChannelCostModePerToken {
		return decimal.Zero, fmt.Errorf("supplier cost mode needs reconciliation")
	}
	for _, name := range []string{"input", "output"} {
		if v, ok := cost.Config.Items[name]; !ok || !valid(v) {
			return decimal.Zero, fmt.Errorf("supplier %s price missing or invalid", name)
		}
	}
	for name, v := range cost.Config.Items {
		if !slices.Contains([]string{"input", "output", "cache_read"}, name) || !valid(v) {
			return decimal.Zero, fmt.Errorf("supplier price category needs reconciliation")
		}
	}
	amount := decimal.NewFromInt(output).Mul(decimal.NewFromFloat(cost.Config.Items["output"]))
	if price, ok := cost.Config.Items["cache_read"]; ok {
		amount = amount.Add(decimal.NewFromInt(cached).Mul(decimal.NewFromFloat(price)))
		input -= cached
	}
	return amount.Add(decimal.NewFromInt(input).Mul(decimal.NewFromFloat(cost.Config.Items["input"]))).Shift(-6), nil
}

func LoadSupplierPrices(db *gorm.DB, cfg *SupplierRoutingConfig) error {
	ids := []int{}
	for _, b := range cfg.Bindings {
		ids = append(ids, b.ChannelID)
	}
	cfg.Prices = nil
	if len(ids) == 0 {
		return nil
	}
	var channels []Channel
	if err := db.Select("id", "deployment_type").Where("id IN ?", ids).Find(&channels).Error; err != nil {
		return err
	}
	selfHosted := map[int]bool{}
	for _, channel := range channels {
		selfHosted[channel.Id] = channel.DeploymentType == ChannelDeploymentSelfHosted
	}
	var prices []ChannelCostPricing
	if err := db.Where("channel_id IN ?", ids).Find(&prices).Error; err != nil {
		return err
	}
	for _, price := range prices {
		if selfHosted[price.ChannelId] {
			continue // Old quotes remain stored but cannot override self-hosted routing.
		}
		if err := common.UnmarshalJsonStr(price.ConfigJson, &price.Config); err != nil {
			return err
		}
		cfg.Prices = append(cfg.Prices, price)
	}
	for _, binding := range cfg.Bindings {
		if selfHosted[binding.ChannelID] {
			cfg.Prices = append(cfg.Prices, SupplierSelfHostedPrice(binding.ChannelID, binding.Model))
		}
	}
	return nil
}

func ValidateSupplierRulePrices(cfg *SupplierRoutingConfig, r SupplierRoutingRule) error {
	currency := ""
	usable := false
	for _, price := range cfg.Prices {
		if price.ModelName != r.Model {
			continue
		}
		binding := slices.IndexFunc(cfg.Bindings, func(b SupplierBinding) bool { return b.ChannelID == price.ChannelId && b.Model == r.Model })
		if binding < 0 {
			continue
		}
		pool := slices.IndexFunc(cfg.Pools, func(p SupplierPool) bool { return p.ID == cfg.Bindings[binding].PoolID })
		if pool < 0 || !slices.ContainsFunc(r.Targets, func(t SupplierTarget) bool { return t.SupplierID == cfg.Pools[pool].SupplierID }) {
			continue
		}
		if _, err := SupplierPriceAmount(price, 1, 1, 0); err != nil {
			continue
		}
		usable = true
		if price.Mode == SupplierCostModeSelfHosted {
			continue
		}
		if currency != "" && currency != price.Currency {
			return SupplierFieldError("targets", "adaptive routing requires procurement quotes in one currency")
		}
		currency = price.Currency
	}
	if !usable {
		return SupplierFieldError("targets", "associate a self-hosted channel or configure a complete text-token or per-request procurement quote first")
	}
	return nil
}
