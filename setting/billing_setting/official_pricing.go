package billing_setting

import (
	"fmt"
	"maps"
	"math"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
)

type OfficialPricing struct {
	Currency     string             `json:"currency"`
	SourceURL    string             `json:"source_url"`
	VerifiedAt   string             `json:"verified_at"`
	Items        map[string]float64 `json:"items"`
	NoteMarkdown string             `json:"note_markdown,omitempty"`
}

func GetOfficialPricing(model string) (OfficialPricing, bool) {
	pricing, ok := billingSetting.OfficialPricing[model]
	if !ok {
		return OfficialPricing{}, false
	}
	pricing.Items = maps.Clone(pricing.Items)
	return pricing, true
}

func GetOfficialPricingCopy() map[string]OfficialPricing {
	result := make(map[string]OfficialPricing, len(billingSetting.OfficialPricing))
	for model, pricing := range billingSetting.OfficialPricing {
		pricing.Items = maps.Clone(pricing.Items)
		result[model] = pricing
	}
	return result
}

func ValidateOfficialPricingJSONString(raw string) error {
	configs := make(map[string]OfficialPricing)
	if err := common.UnmarshalJsonStr(raw, &configs); err != nil {
		return fmt.Errorf("invalid official pricing JSON: %w", err)
	}
	for model, pricing := range configs {
		if strings.TrimSpace(model) == "" {
			return fmt.Errorf("official pricing model name is required")
		}
		currency := strings.ToUpper(strings.TrimSpace(pricing.Currency))
		if currency != "USD" && currency != "CNY" {
			return fmt.Errorf("official pricing currency for model %q must be USD or CNY", model)
		}
		parsedURL, err := url.ParseRequestURI(strings.TrimSpace(pricing.SourceURL))
		if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") || parsedURL.Host == "" {
			return fmt.Errorf("official pricing source URL for model %q is invalid", model)
		}
		if _, err := time.Parse(time.DateOnly, strings.TrimSpace(pricing.VerifiedAt)); err != nil {
			return fmt.Errorf("official pricing verification date for model %q must use YYYY-MM-DD", model)
		}
		if len(pricing.Items) == 0 {
			return fmt.Errorf("official pricing for model %q requires at least one item", model)
		}
		for key, amount := range pricing.Items {
			if strings.TrimSpace(key) == "" {
				return fmt.Errorf("official pricing item key for model %q is required", model)
			}
			if math.IsNaN(amount) || math.IsInf(amount, 0) || amount < 0 {
				return fmt.Errorf("official pricing item %q for model %q must be a non-negative finite number", key, model)
			}
		}
		if len(pricing.NoteMarkdown) > 20000 {
			return fmt.Errorf("official pricing note for model %q is too long", model)
		}
	}
	return nil
}
