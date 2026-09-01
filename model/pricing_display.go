package model

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/pkg/taskbilling"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

const PricingDisplayVersion = "price-comparison-v1"

type PricingDisplay struct {
	Version  string                  `json:"version"`
	Items    []PricingDisplayItem    `json:"items"`
	Official *PricingDisplayOfficial `json:"official,omitempty"`
}

type PricingDisplayOfficial struct {
	Currency     string `json:"currency"`
	SourceURL    string `json:"source_url"`
	VerifiedAt   string `json:"verified_at"`
	NoteMarkdown string `json:"note_markdown,omitempty"`
}

type PricingDisplayItem struct {
	Key               string   `json:"key"`
	Item              string   `json:"item"`
	Unit              string   `json:"unit"`
	Condition         string   `json:"condition,omitempty"`
	Note              string   `json:"note,omitempty"`
	FreeCount         *int     `json:"free_count,omitempty"`
	OurAmountUSD      *float64 `json:"our_amount_usd,omitempty"`
	OfficialAmountUSD *float64 `json:"official_amount_usd,omitempty"`
}

type PricingOfficialDiscount struct {
	Editable bool   `json:"editable"`
	BaseMin  string `json:"base_min"`
	BaseMax  string `json:"base_max"`
}

type pricingBaseVariant struct {
	price     float64
	suffix    string
	condition string
}

func BuildPricingDisplay(pricing Pricing, customerRatio float64) *PricingDisplay {
	if customerRatio < 0 {
		customerRatio = 0
	}
	var items []PricingDisplayItem
	if pricing.BillingMode == billing_setting.BillingModeTieredExpr && strings.TrimSpace(pricing.BillingExpr) != "" {
		items = []PricingDisplayItem{{
			Key: "dynamic", Item: "动态计费", Unit: "dynamic",
			Condition: "按请求内容、参数与实际用量", Note: "以实际账单为准",
		}}
	} else if pricing.TaskBilling != nil && pricing.TaskBilling.Mode == taskbilling.ModeTokenParametric {
		items = buildTokenParametricPricingItems(pricing.TaskBilling, customerRatio)
	} else if pricing.QuotaType == 1 {
		items = buildFixedPricingItems(pricing, customerRatio)
	} else {
		items = buildTokenPricingItems(pricing, customerRatio)
	}

	display := &PricingDisplay{Version: PricingDisplayVersion, Items: items}
	attachOfficialPricing(display, pricing.ModelName)
	return display
}

func pricingOfficialDiscount(pricing Pricing) (*PricingOfficialDiscount, float64) {
	display := BuildPricingDisplay(pricing, 1)
	if display.Official == nil {
		return nil, 0
	}
	minRate := math.Inf(1)
	maxRate := 0.0
	complete := len(display.Items) > 0
	for _, item := range display.Items {
		if item.OurAmountUSD == nil || *item.OurAmountUSD <= 0 || item.OfficialAmountUSD == nil || *item.OfficialAmountUSD <= 0 {
			complete = false
			continue
		}
		rate := *item.OurAmountUSD / *item.OfficialAmountUSD * 10
		if rate < minRate {
			minRate = rate
		}
		if rate > maxRate {
			maxRate = rate
		}
	}
	if math.IsInf(minRate, 1) {
		return nil, 0
	}
	reference := &PricingOfficialDiscount{
		Editable: complete && maxRate-minRate < 0.000001,
		BaseMin:  formatOfficialDiscount(minRate),
		BaseMax:  formatOfficialDiscount(maxRate),
	}
	return reference, minRate
}

func GetPricingOfficialDiscount(modelName string) *PricingOfficialDiscount {
	for _, pricing := range GetPricing() {
		if pricing.ModelName == modelName {
			reference, _ := pricingOfficialDiscount(pricing)
			return reference
		}
	}
	return nil
}

func ResellerMultiplierFromOfficialDiscount(modelName string, discount string) (int64, error) {
	for _, pricing := range GetPricing() {
		if pricing.ModelName != modelName {
			continue
		}
		return resellerMultiplierFromOfficialDiscount(pricing, discount)
	}
	return 0, ErrInvalidResellerPriceRule
}

func resellerMultiplierFromOfficialDiscount(pricing Pricing, discount string) (int64, error) {
	reference, baseRate := pricingOfficialDiscount(pricing)
	if reference == nil || !reference.Editable || baseRate <= 0 {
		return 0, ErrInvalidResellerPriceRule
	}
	discountPPM, err := ParseResellerMultiplier(discount)
	if err != nil || discountPPM > 10*ResellerDefaultMultiplierPPM {
		return 0, ErrInvalidResellerPriceRule
	}
	multiplier := math.Round(float64(discountPPM) / baseRate)
	if multiplier <= 0 || multiplier > math.MaxInt64 {
		return 0, ErrInvalidResellerPriceRule
	}
	return int64(multiplier), nil
}

func formatOfficialDiscount(rate float64) string {
	return FormatResellerMultiplier(int64(math.Round(rate * float64(ResellerDefaultMultiplierPPM))))
}

func buildTokenParametricPricingItems(config *taskbilling.Config, customerRatio float64) []PricingDisplayItem {
	if config.TokenPrices == nil {
		return nil
	}
	resolutions := make([]string, 0, len(config.TokenPrices.Values))
	for resolution := range config.TokenPrices.Values {
		resolutions = append(resolutions, resolution)
	}
	sort.Strings(resolutions)
	items := make([]PricingDisplayItem, 0, len(resolutions)*2)
	for _, resolution := range resolutions {
		price := config.TokenPrices.Values[resolution]
		items = append(items, pricingAmountItem(
			"task:tokens:resolution="+resolution+":reference_video=false",
			"任务 Token", "million_tokens",
			fmt.Sprintf("resolution = %s；不含参考视频", resolution),
			"按上游返回的实际总 Token 结算", price.Standard*customerRatio,
		))
		if price.ReferenceVideo != nil {
			items = append(items, pricingAmountItem(
				"task:tokens:resolution="+resolution+":reference_video=true",
				"任务 Token", "million_tokens",
				fmt.Sprintf("resolution = %s；包含参考视频", resolution),
				"参考视频由服务端根据有效请求内容识别", *price.ReferenceVideo*customerRatio,
			))
		}
	}
	return items
}

func buildTokenPricingItems(pricing Pricing, customerRatio float64) []PricingDisplayItem {
	inputPrice := pricing.ModelRatio * 2 * customerRatio
	items := make([]PricingDisplayItem, 0, 14)
	appendRates := func(scale float64, suffix string, condition string) {
		items = append(items,
			pricingAmountItem("token:input"+suffix, "输入 Token", "million_tokens", condition, "", inputPrice*scale),
			pricingAmountItem("token:output"+suffix, "输出 Token", "million_tokens", condition, "", inputPrice*pricing.CompletionRatio*scale),
		)
		optional := []struct {
			key   string
			item  string
			ratio *float64
		}{
			{key: "token:cache_read", item: "缓存读取", ratio: pricing.CacheRatio},
			{key: "token:cache_write", item: "缓存写入", ratio: pricing.CreateCacheRatio},
			{key: "token:image_input", item: "图片输入", ratio: pricing.ImageRatio},
			{key: "token:audio_input", item: "音频输入", ratio: pricing.AudioRatio},
			{key: "token:audio_output", item: "音频输出", ratio: pricing.AudioCompletionRatio},
		}
		for _, rate := range optional {
			if rate.ratio == nil {
				continue
			}
			items = append(items, pricingAmountItem(rate.key+suffix, rate.item, "million_tokens", condition, "", inputPrice**rate.ratio*scale))
		}
	}

	if videoRatio, ok := ratio_setting.GetVideoInputRatio(pricing.ModelName); ok {
		appendRates(1, ":reference_video=false", "不含参考视频")
		appendRates(videoRatio, ":reference_video=true", "包含参考视频")
	} else {
		appendRates(1, "", "全部请求")
	}
	return items
}

func buildFixedPricingItems(pricing Pricing, customerRatio float64) []PricingDisplayItem {
	resellerMultiplier := pricing.customerPriceMultiplier
	if resellerMultiplier <= 0 {
		resellerMultiplier = 1
	}
	variants := []pricingBaseVariant{{price: pricing.ModelPrice}}
	if referencePrice, ok := ratio_setting.GetReferenceVideoPrice(pricing.ModelName); ok {
		variants = []pricingBaseVariant{
			{price: pricing.ModelPrice, suffix: ":reference_video=false", condition: "不含参考视频"},
			{price: referencePrice * resellerMultiplier, suffix: ":reference_video=true", condition: "包含参考视频"},
		}
	}

	mode := taskbilling.ModePerRequest
	if pricing.TaskBilling != nil {
		mode = pricing.TaskBilling.Mode
	}
	items := make([]PricingDisplayItem, 0, 8)
	switch mode {
	case taskbilling.ModePerSecond:
		for _, variant := range variants {
			items = append(items, pricingAmountItem("task:second"+variant.suffix, "视频生成", "second", variant.condition, durationNote(pricing.TaskBilling), variant.price*customerRatio))
		}
	case taskbilling.ModeParametric:
		items = append(items, buildParametricPricingItems(pricing.TaskBilling, variants, customerRatio)...)
	default:
		for _, variant := range variants {
			items = append(items, pricingAmountItem("task:request"+variant.suffix, "模型调用", "request", variant.condition, "", variant.price*customerRatio))
		}
	}

	if pricing.TaskBilling != nil && pricing.TaskBilling.Surcharge != nil {
		surcharge := pricing.TaskBilling.Surcharge
		freeCount := surcharge.FreeCount
		condition := "按实际素材数量计费"
		if freeCount > 0 {
			condition = fmt.Sprintf("超过 %d 个后开始计费", freeCount)
		}
		item := pricingAmountItem(
			"surcharge:"+strings.ToLower(strings.TrimSpace(surcharge.Name)),
			pricingItemName(surcharge.Name), "item", condition, "超出部分逐个收费",
			surcharge.UnitPrice*resellerMultiplier*customerRatio,
		)
		item.FreeCount = &freeCount
		items = append(items, item)
	}
	return items
}

func buildParametricPricingItems(config *taskbilling.Config, variants []pricingBaseVariant, customerRatio float64) []PricingDisplayItem {
	if config == nil {
		return nil
	}
	var duration *taskbilling.Dimension
	enumDimensions := make([]taskbilling.Dimension, 0, 1)
	unsupported := false
	for index := range config.Dimensions {
		dimension := &config.Dimensions[index]
		switch dimension.Kind {
		case taskbilling.DimensionNumber:
			if duration != nil || !isDurationDimension(dimension.Name) {
				unsupported = true
				continue
			}
			duration = dimension
		case taskbilling.DimensionEnum:
			enumDimensions = append(enumDimensions, *dimension)
		default:
			unsupported = true
		}
	}
	if unsupported || duration == nil || len(enumDimensions) > 1 {
		return []PricingDisplayItem{{
			Key: "dynamic", Item: "参数计费", Unit: "dynamic",
			Condition: "按请求参数计算", Note: "以实际账单为准",
		}}
	}
	if len(enumDimensions) == 0 {
		items := make([]PricingDisplayItem, 0, len(variants))
		for _, variant := range variants {
			items = append(items, pricingAmountItem("task:second"+variant.suffix, "视频生成", "second", variant.condition, dimensionNote(*duration), variant.price*customerRatio))
		}
		return items
	}

	dimension := enumDimensions[0]
	values := make([]string, 0, len(dimension.Values))
	for value := range dimension.Values {
		values = append(values, value)
	}
	sort.Strings(values)
	items := make([]PricingDisplayItem, 0, len(values)*len(variants))
	for _, value := range values {
		for _, variant := range variants {
			condition := fmt.Sprintf("%s = %s", dimension.Name, value)
			if variant.condition != "" {
				condition += "；" + variant.condition
			}
			key := fmt.Sprintf("task:second:%s=%s%s", strings.ToLower(strings.TrimSpace(dimension.Name)), strings.ToLower(strings.TrimSpace(value)), variant.suffix)
			items = append(items, pricingAmountItem(key, value, "second", condition, dimensionNote(*duration), variant.price*dimension.Values[value]*customerRatio))
		}
	}
	return items
}

func attachOfficialPricing(display *PricingDisplay, modelName string) {
	official, ok := billing_setting.GetOfficialPricing(modelName)
	if !ok {
		return
	}
	display.Official = &PricingDisplayOfficial{
		Currency: strings.ToUpper(strings.TrimSpace(official.Currency)), SourceURL: official.SourceURL,
		VerifiedAt: official.VerifiedAt, NoteMarkdown: official.NoteMarkdown,
	}
	for index := range display.Items {
		item := &display.Items[index]
		amount, ok := official.Items[item.Key]
		if !ok || item.OurAmountUSD == nil {
			continue
		}
		officialUSD, ok := officialPriceToUSD(amount, official.Currency)
		if !ok {
			continue
		}
		item.OfficialAmountUSD = &officialUSD
	}
}

func officialPriceToUSD(amount float64, currency string) (float64, bool) {
	switch strings.ToUpper(strings.TrimSpace(currency)) {
	case "USD":
		return amount, true
	case "CNY":
		if operation_setting.USDExchangeRate <= 0 {
			return 0, false
		}
		return amount / operation_setting.USDExchangeRate, true
	default:
		return 0, false
	}
}

func pricingAmountItem(key string, item string, unit string, condition string, note string, amount float64) PricingDisplayItem {
	return PricingDisplayItem{
		Key: key, Item: item, Unit: unit, Condition: condition, Note: note, OurAmountUSD: &amount,
	}
}

func durationNote(config *taskbilling.Config) string {
	if config == nil || config.Duration == nil {
		return ""
	}
	return dimensionNote(*config.Duration)
}

func isDurationDimension(name string) bool {
	return strings.EqualFold(strings.TrimSpace(name), "duration") || strings.EqualFold(strings.TrimSpace(name), "seconds")
}

func dimensionNote(dimension taskbilling.Dimension) string {
	parts := make([]string, 0, 2)
	if dimension.Default != nil {
		parts = append(parts, fmt.Sprintf("未传时默认 %v 秒", dimension.Default))
	}
	switch dimension.Round {
	case taskbilling.RoundCeil:
		parts = append(parts, "时长按整秒向上取整")
	case taskbilling.RoundFloor:
		parts = append(parts, "时长按整秒向下取整")
	case taskbilling.RoundNearest:
		parts = append(parts, "时长按整秒四舍五入")
	}
	return strings.Join(parts, "；")
}

func pricingItemName(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "input_images", "images", "image_count":
		return "附加图片素材"
	case "input_videos", "videos", "video_count":
		return "附加视频素材"
	case "input_audios", "audios", "audio_count":
		return "附加音频素材"
	default:
		if strings.TrimSpace(name) == "" {
			return "附加项"
		}
		return strings.TrimSpace(name)
	}
}
