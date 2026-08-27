package model

import (
	"fmt"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/pkg/taskbilling"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

type PricingDisplay struct {
	Rows []PricingDisplayRow `json:"rows"`
}

type PricingDisplayRow struct {
	Item       string   `json:"item"`
	AmountUSD  *float64 `json:"amount_usd,omitempty"`
	Multiplier *float64 `json:"multiplier,omitempty"`
	Unit       string   `json:"unit,omitempty"`
	Condition  string   `json:"condition,omitempty"`
	Note       string   `json:"note,omitempty"`
}

// BuildPricingDisplay derives customer-facing rows from the same settings used
// by the relay. customerRatio contains the effective group and model-scoped
// user ratios; reseller projection has already been applied to pricing.
func BuildPricingDisplay(pricing Pricing, customerRatio float64) *PricingDisplay {
	if customerRatio < 0 {
		customerRatio = 0
	}
	if pricing.BillingMode == "tiered_expr" && strings.TrimSpace(pricing.BillingExpr) != "" {
		return &PricingDisplay{Rows: []PricingDisplayRow{{
			Item:      "动态计费",
			Unit:      "dynamic",
			Condition: "按请求内容、参数与实际用量",
			Note:      "价格由阶梯或请求条件动态计算，以实际账单为准",
		}}}
	}

	if pricing.QuotaType == 1 {
		return buildFixedPricingDisplay(pricing, customerRatio)
	}
	return buildTokenPricingDisplay(pricing, customerRatio)
}

func buildTokenPricingDisplay(pricing Pricing, customerRatio float64) *PricingDisplay {
	inputPrice := pricing.ModelRatio * 2 * customerRatio
	rows := make([]PricingDisplayRow, 0, 14)
	appendRates := func(priceScale float64, condition string) {
		rows = append(rows,
			amountRow("输入 Token", inputPrice*priceScale, "million_tokens", condition, "文本及未单独计价的输入"),
			amountRow("输出 Token", inputPrice*pricing.CompletionRatio*priceScale, "million_tokens", condition, "文本及未单独计价的输出"),
		)
		optional := []struct {
			item  string
			ratio *float64
			note  string
		}{
			{item: "缓存读取", ratio: pricing.CacheRatio, note: "命中缓存的输入"},
			{item: "缓存写入", ratio: pricing.CreateCacheRatio, note: "新写入缓存的输入"},
			{item: "图片输入", ratio: pricing.ImageRatio, note: "图片输入相关消耗"},
			{item: "音频输入", ratio: pricing.AudioRatio, note: "音频输入相关消耗"},
			{item: "音频输出", ratio: pricing.AudioCompletionRatio, note: "音频输出相关消耗"},
		}
		for _, rate := range optional {
			if rate.ratio == nil {
				continue
			}
			rows = append(rows, amountRow(rate.item, inputPrice**rate.ratio*priceScale, "million_tokens", condition, rate.note))
		}
	}

	if videoRatio, ok := ratio_setting.GetVideoInputRatio(pricing.ModelName); ok {
		appendRates(1, "不含参考视频")
		appendRates(videoRatio, "包含参考视频")
	} else {
		appendRates(1, "全部请求")
	}
	return &PricingDisplay{Rows: rows}
}

func buildFixedPricingDisplay(pricing Pricing, customerRatio float64) *PricingDisplay {
	resellerMultiplier := pricing.customerPriceMultiplier
	if resellerMultiplier <= 0 {
		resellerMultiplier = 1
	}

	mode := taskbilling.ModePerRequest
	if pricing.TaskBilling != nil {
		mode = pricing.TaskBilling.Mode
	}
	item, unit, condition, note := fixedBasePresentation(pricing, mode)
	rows := make([]PricingDisplayRow, 0, 8)
	appendBase := func(basePrice float64, referenceCondition string) {
		rowCondition := condition
		if referenceCondition != "" {
			rowCondition = referenceCondition
			if condition != "全部请求" {
				rowCondition += "；" + condition
			}
		}
		rows = append(rows, amountRow(item, basePrice*customerRatio, unit, rowCondition, note))
	}

	if referencePrice, ok := ratio_setting.GetReferenceVideoPrice(pricing.ModelName); ok {
		appendBase(pricing.ModelPrice, "不含参考视频")
		appendBase(referencePrice*resellerMultiplier, "包含参考视频")
	} else {
		appendBase(pricing.ModelPrice, "")
	}

	if pricing.TaskBilling != nil && pricing.TaskBilling.Mode == taskbilling.ModeParametric {
		rows = append(rows, parametricRows(pricing.TaskBilling.Dimensions)...)
	}
	if pricing.TaskBilling != nil && pricing.TaskBilling.Surcharge != nil {
		surcharge := pricing.TaskBilling.Surcharge
		condition := "按实际素材数量计费"
		note := ""
		if surcharge.FreeCount > 0 {
			condition = fmt.Sprintf("超过 %d 个后开始计费", surcharge.FreeCount)
			note = fmt.Sprintf("前 %d 个免费，超出部分逐个收费", surcharge.FreeCount)
		}
		rows = append(rows, amountRow(
			pricingItemName(surcharge.Name),
			surcharge.UnitPrice*resellerMultiplier*customerRatio,
			"item",
			condition,
			note,
		))
	}
	return &PricingDisplay{Rows: rows}
}

func fixedBasePresentation(pricing Pricing, mode string) (item string, unit string, condition string, note string) {
	switch mode {
	case taskbilling.ModePerSecond:
		item, unit, condition = "视频生成", "second", "按生成时长计费"
		if pricing.TaskBilling != nil && pricing.TaskBilling.Duration != nil {
			note = dimensionNote(*pricing.TaskBilling.Duration)
		}
	case taskbilling.ModeParametric:
		item, unit, condition, note = "基础调用", "request", "基础单价", "最终价格为基础单价乘以请求参数倍率"
	default:
		item, unit, condition = "模型调用", "request", "全部请求"
		if pricing.TaskBilling == nil {
			note = "未配置参数化计费规则；实际费用可能随渠道请求参数调整"
		}
	}
	return
}

func parametricRows(dimensions []taskbilling.Dimension) []PricingDisplayRow {
	rows := make([]PricingDisplayRow, 0, len(dimensions))
	for _, dimension := range dimensions {
		name := pricingItemName(dimension.Name)
		if dimension.Kind == taskbilling.DimensionEnum {
			values := make([]string, 0, len(dimension.Values))
			for value := range dimension.Values {
				values = append(values, value)
			}
			sort.Strings(values)
			for _, value := range values {
				multiplier := dimension.Values[value]
				note := ""
				if dimension.Default != nil && strings.EqualFold(strings.TrimSpace(fmt.Sprint(dimension.Default)), strings.TrimSpace(value)) {
					note = "默认值"
				}
				rows = append(rows, PricingDisplayRow{
					Item: name, Multiplier: &multiplier, Unit: "multiplier",
					Condition: fmt.Sprintf("%s = %s", dimension.Name, value), Note: note,
				})
			}
			continue
		}
		rows = append(rows, PricingDisplayRow{
			Item: name, Unit: "dynamic",
			Condition: fmt.Sprintf("按参数 %s 的数值", dimension.Name), Note: dimensionNote(dimension),
		})
	}
	return rows
}

func dimensionNote(dimension taskbilling.Dimension) string {
	parts := make([]string, 0, 3)
	isDuration := strings.EqualFold(dimension.Name, "duration") || strings.EqualFold(dimension.Name, "seconds")
	if dimension.Default != nil {
		defaultValue := fmt.Sprint(dimension.Default)
		if isDuration {
			defaultValue += " 秒"
		}
		parts = append(parts, "未传时默认 "+defaultValue)
	}
	unit := dimension.Unit
	if unit <= 0 {
		unit = 1
	}
	switch dimension.Round {
	case taskbilling.RoundCeil:
		if isDuration {
			parts = append(parts, "时长按整秒向上取整")
		} else {
			parts = append(parts, "参数值向上取整")
		}
	case taskbilling.RoundFloor:
		if isDuration {
			parts = append(parts, "时长按整秒向下取整")
		} else {
			parts = append(parts, "参数值向下取整")
		}
	case taskbilling.RoundNearest:
		if isDuration {
			parts = append(parts, "时长按整秒四舍五入")
		} else {
			parts = append(parts, "参数值四舍五入")
		}
	}
	if unit != 1 {
		unitName := "个单位"
		if isDuration {
			unitName = "秒"
		}
		parts = append(parts, fmt.Sprintf("每 %v %s折算 1 倍", unit, unitName))
	}
	return strings.Join(parts, "；")
}

func pricingItemName(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "duration", "seconds":
		return "生成时长"
	case "input_images", "images", "image_count":
		return "附加图片素材"
	case "input_videos", "videos", "video_count":
		return "附加视频素材"
	case "resolution", "size":
		return "分辨率"
	default:
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			return "附加项"
		}
		return trimmed
	}
}

func amountRow(item string, amount float64, unit string, condition string, note string) PricingDisplayRow {
	return PricingDisplayRow{
		Item: item, AmountUSD: &amount, Unit: unit, Condition: condition, Note: note,
	}
}
