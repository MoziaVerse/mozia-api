package model

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/vm"
)

func buildTimePricingItems(expression string, customerRatio float64) []PricingDisplayItem {
	program, err := billingexpr.CompileFromCache(expression)
	if err != nil {
		return nil
	}
	node := program.Node()
	// Reseller prices wrap the complete expression in a constant multiplier.
	for {
		product, ok := node.(*ast.BinaryNode)
		if !ok || product.Operator != "*" {
			break
		}
		ratio, ok := pricingNumber(product.Right)
		if !ok {
			return nil
		}
		customerRatio *= ratio
		node = product.Left
	}
	conditional, ok := node.(*ast.ConditionalNode)
	if !ok {
		return nil
	}
	schedule := pricingTimeSchedule(conditional.Cond)
	if schedule == "" {
		return nil
	}

	items := make([]PricingDisplayItem, 0, 6)
	for index, branch := range []ast.Node{conditional.Exp1, conditional.Exp2} {
		tier, ok := branch.(*ast.CallNode)
		if !ok || len(tier.Arguments) != 2 || tier.Callee.String() != "tier" {
			return nil
		}
		name, ok := tier.Arguments[0].(*ast.StringNode)
		if !ok {
			return nil
		}
		rates := make(map[string]float64)
		if !collectPricingTokenRates(tier.Arguments[1], rates) {
			return nil
		}
		label := name.Value
		switch label {
		case "peak":
			label = "高峰"
		case "off_peak":
			label = "空闲"
		}
		condition := schedule
		if index == 1 {
			condition = "除「" + schedule + "」以外的时段"
		}
		for _, dimension := range []struct{ variable, key, label string }{
			{"p", "input", "输入 Token"}, {"c", "output", "输出 Token"},
			{"cr", "cache_read", "缓存读取"}, {"cc", "cache_write", "缓存写入"},
			{"cc1h", "cache_write_1h", "缓存写入（1 小时）"},
			{"img", "image_input", "图片输入"}, {"img_o", "image_output", "图片输出"},
			{"ai", "audio_input", "音频输入"}, {"ao", "audio_output", "音频输出"},
		} {
			rate, exists := rates[dimension.variable]
			if !exists {
				continue
			}
			amount := rate * customerRatio
			if math.IsNaN(amount) || math.IsInf(amount, 0) || amount < 0 {
				return nil
			}
			items = append(items, pricingAmountItem(
				fmt.Sprintf("token:%s:time_tier=%d", dimension.key, index), label+" · "+dimension.label,
				"million_tokens", condition, "按结算时所在时段计费", amount,
			))
			delete(rates, dimension.variable)
		}
		if len(rates) > 0 {
			return nil
		}
	}
	return items
}

// Only sums of token * constant terms have an unambiguous per-token price.
func collectPricingTokenRates(node ast.Node, rates map[string]float64) bool {
	binary, ok := node.(*ast.BinaryNode)
	if !ok {
		return false
	}
	if binary.Operator == "+" {
		return collectPricingTokenRates(binary.Left, rates) && collectPricingTokenRates(binary.Right, rates)
	}
	if binary.Operator != "*" {
		return false
	}
	variable, ok := binary.Left.(*ast.IdentifierNode)
	price, valid := pricingNumber(binary.Right)
	if !ok || !valid {
		return false
	}
	rates[variable.Value] += price
	return true
}

func pricingNumber(node ast.Node) (float64, bool) {
	switch number := node.(type) {
	case *ast.IntegerNode:
		return float64(number.Value), number.Value >= 0
	case *ast.FloatNode:
		return number.Value, number.Value >= 0 && !math.IsNaN(number.Value) && !math.IsInf(number.Value, 0)
	default:
		return 0, false
	}
}

func pricingTimeSchedule(condition ast.Node) string {
	zone := ""
	// ponytail: hour/weekday predicates have exactly 168 states; other time or
	// request functions retain dynamic display until their schedules are supported.
	unsupported := ast.Find(condition, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.BinaryNode:
			switch n.Operator {
			case "&&", "and", "||", "or", "<", "<=", ">", ">=", "==", "!=":
				return false
			default:
				return true
			}
		case *ast.UnaryNode:
			return n.Operator != "!" && n.Operator != "not"
		case *ast.CallNode:
			if len(n.Arguments) != 1 {
				return true
			}
			name := n.Callee.String()
			if name != "hour" && name != "weekday" {
				return true
			}
			tz, ok := n.Arguments[0].(*ast.StringNode)
			if !ok || tz.Value == "" || (zone != "" && zone != tz.Value) {
				return true
			}
			zone = tz.Value
		case *ast.IdentifierNode:
			return n.Value != "hour" && n.Value != "weekday"
		case *ast.IntegerNode, *ast.FloatNode, *ast.StringNode, *ast.BoolNode:
		default:
			return true
		}
		return false
	})
	if unsupported != nil || zone == "" {
		return ""
	}
	if _, err := time.LoadLocation(zone); err != nil {
		return ""
	}
	day, hour := 0, 0
	env := map[string]interface{}{
		"weekday": func(string) int { return day },
		"hour":    func(string) int { return hour },
	}
	program, err := expr.Compile(condition.String(), expr.Env(env), expr.AsBool())
	if err != nil {
		return ""
	}
	weekdays := []string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}
	daysByHours := make(map[string][]string)
	var schedules []string
	for d := 1; d <= 7; d++ {
		day = d % 7
		start := -1
		var intervals []string
		for hour = 0; hour <= 24; hour++ {
			active := false
			if hour < 24 {
				result, err := vm.Run(program, env)
				if err != nil {
					return ""
				}
				active = result.(bool)
			}
			if active && start < 0 {
				start = hour
			} else if !active && start >= 0 {
				intervals = append(intervals, fmt.Sprintf("%02d:00–%02d:00", start, hour))
				start = -1
			}
		}
		if len(intervals) == 0 {
			continue
		}
		hours := strings.Join(intervals, "、")
		if _, exists := daysByHours[hours]; !exists {
			schedules = append(schedules, hours)
		}
		daysByHours[hours] = append(daysByHours[hours], weekdays[day])
	}
	for i, hours := range schedules {
		schedules[i] = strings.Join(daysByHours[hours], "、") + " " + hours
	}
	if len(schedules) == 0 {
		return ""
	}
	if zone == "Asia/Shanghai" {
		zone = "北京时间"
	}
	return strings.Join(schedules, "；") + "（" + zone + "）"
}
