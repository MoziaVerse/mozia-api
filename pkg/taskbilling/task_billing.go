// Package taskbilling defines versioned, request-parameter based billing for
// asynchronous tasks. It deliberately models only request quantities: token
// billing remains in billingexpr.
package taskbilling

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/tidwall/gjson"
)

const (
	Version1 = 1

	ModePerRequest = "per_request"
	ModePerSecond  = "per_second"
	ModeParametric = "parametric"

	DimensionNumber    = "number"
	DimensionEnum      = "enum"
	SurchargeItemCount = "item_count"

	RoundNone    = "none"
	RoundCeil    = "ceil"
	RoundFloor   = "floor"
	RoundNearest = "nearest"
)

// Config describes how a task ModelPrice is multiplied. ModelPrice is always
// the base price; this config only defines the request-derived quantities.
//
// Per-request has no dimensions. Per-second has one required Duration
// dimension. Parametric accepts one or more dimensions and multiplies all of
// their values together.
type Config struct {
	Version    int         `json:"version"`
	Mode       string      `json:"mode"`
	Duration   *Dimension  `json:"duration,omitempty"`
	Dimensions []Dimension `json:"dimensions,omitempty"`
	Surcharge  *Surcharge  `json:"surcharge,omitempty"`
}

// Surcharge adds a fixed price for each billable item after FreeCount. The
// first present array in Paths is used. Arrays of objects can be restricted by
// their type field; arrays of strings count every item.
type Surcharge struct {
	Name      string   `json:"name"`
	Kind      string   `json:"kind"`
	Paths     []string `json:"paths"`
	ItemTypes []string `json:"item_types,omitempty"`
	FreeCount int      `json:"free_count,omitempty"`
	UnitPrice float64  `json:"unit_price"`
}

type SurchargeResult struct {
	Name          string  `json:"name"`
	Count         int     `json:"count"`
	FreeCount     int     `json:"free_count"`
	BillableCount int     `json:"billable_count"`
	UnitPrice     float64 `json:"unit_price"`
	Price         float64 `json:"price"`
}

type Evaluation struct {
	Ratios    map[string]float64 `json:"ratios,omitempty"`
	Surcharge *SurchargeResult   `json:"surcharge,omitempty"`
}

// Dimension reads the first present JSON path from Paths. A number dimension
// contributes value / Unit. An enum dimension contributes the configured
// multiplier for the normalized value. Default is used when no path is set.
type Dimension struct {
	Name    string             `json:"name"`
	Kind    string             `json:"kind"`
	Paths   []string           `json:"paths"`
	Default any                `json:"default,omitempty"`
	Unit    float64            `json:"unit,omitempty"`
	Round   string             `json:"round,omitempty"`
	Values  map[string]float64 `json:"values,omitempty"`
}

// Validate rejects unsupported or ambiguous rules before they reach the relay
// hot path. This keeps a configured ModelPrice's unit explicit.
func Validate(config Config) error {
	if config.Version != Version1 {
		return fmt.Errorf("unsupported task billing version %d", config.Version)
	}

	var modeErr error
	switch config.Mode {
	case ModePerRequest:
		if config.Duration != nil || len(config.Dimensions) != 0 {
			modeErr = fmt.Errorf("per_request task billing cannot define dimensions")
		}
	case ModePerSecond:
		if config.Duration == nil {
			modeErr = fmt.Errorf("per_second task billing requires duration")
		} else if len(config.Dimensions) != 0 {
			modeErr = fmt.Errorf("per_second task billing cannot define dimensions")
		} else {
			dimension := *config.Duration
			if dimension.Name == "" {
				dimension.Name = "duration"
			}
			if dimension.Kind == "" {
				dimension.Kind = DimensionNumber
			}
			modeErr = validateDimension(dimension)
		}
	case ModeParametric:
		if config.Duration != nil {
			modeErr = fmt.Errorf("parametric task billing must use dimensions instead of duration")
		} else if len(config.Dimensions) == 0 {
			modeErr = fmt.Errorf("parametric task billing requires at least one dimension")
		} else {
			seen := make(map[string]struct{}, len(config.Dimensions))
			for _, dimension := range config.Dimensions {
				if err := validateDimension(dimension); err != nil {
					modeErr = err
					break
				}
				name := strings.TrimSpace(dimension.Name)
				if _, ok := seen[name]; ok {
					modeErr = fmt.Errorf("duplicate task billing dimension %q", name)
					break
				}
				seen[name] = struct{}{}
			}
		}
	default:
		modeErr = fmt.Errorf("unsupported task billing mode %q", config.Mode)
	}
	if modeErr != nil {
		return modeErr
	}
	return validateSurcharge(config.Surcharge)
}

func validateSurcharge(surcharge *Surcharge) error {
	if surcharge == nil {
		return nil
	}
	name := strings.TrimSpace(surcharge.Name)
	if name == "" {
		return fmt.Errorf("task billing surcharge name is required")
	}
	if surcharge.Kind != SurchargeItemCount {
		return fmt.Errorf("task billing surcharge %q has unsupported kind %q", name, surcharge.Kind)
	}
	if len(surcharge.Paths) == 0 {
		return fmt.Errorf("task billing surcharge %q requires paths", name)
	}
	for _, path := range surcharge.Paths {
		if strings.TrimSpace(path) == "" {
			return fmt.Errorf("task billing surcharge %q has an empty path", name)
		}
	}
	if surcharge.FreeCount < 0 {
		return fmt.Errorf("task billing surcharge %q has invalid free_count", name)
	}
	if !isFinite(surcharge.UnitPrice) || surcharge.UnitPrice <= 0 {
		return fmt.Errorf("task billing surcharge %q has invalid unit_price", name)
	}
	return nil
}

func validateDimension(dimension Dimension) error {
	name := strings.TrimSpace(dimension.Name)
	if name == "" {
		return fmt.Errorf("task billing dimension name is required")
	}
	if len(dimension.Paths) == 0 {
		return fmt.Errorf("task billing dimension %q requires paths", name)
	}
	for _, path := range dimension.Paths {
		if strings.TrimSpace(path) == "" {
			return fmt.Errorf("task billing dimension %q has an empty path", name)
		}
	}

	switch dimension.Kind {
	case DimensionNumber:
		if dimension.Unit != 0 && (!isFinite(dimension.Unit) || dimension.Unit <= 0) {
			return fmt.Errorf("task billing number dimension %q has invalid unit", name)
		}
		if err := validateRound(dimension.Round); err != nil {
			return fmt.Errorf("task billing number dimension %q: %w", name, err)
		}
		if dimension.Default != nil {
			value, ok := numberValue(dimension.Default)
			if !ok || !isFinite(value) || value <= 0 {
				return fmt.Errorf("task billing number dimension %q has invalid default", name)
			}
		}
		return nil
	case DimensionEnum:
		defaultValue := ""
		if dimension.Default != nil {
			defaultValue = normalizeEnum(fmt.Sprint(dimension.Default))
			if defaultValue == "" {
				return fmt.Errorf("task billing enum dimension %q has invalid default", name)
			}
		}
		if len(dimension.Values) == 0 {
			return fmt.Errorf("task billing enum dimension %q requires values", name)
		}
		seen := make(map[string]struct{}, len(dimension.Values))
		for value, multiplier := range dimension.Values {
			normalized := normalizeEnum(value)
			if normalized == "" {
				return fmt.Errorf("task billing enum dimension %q has an empty value", name)
			}
			if _, ok := seen[normalized]; ok {
				return fmt.Errorf("task billing enum dimension %q has duplicate value %q", name, value)
			}
			if !isFinite(multiplier) || multiplier <= 0 {
				return fmt.Errorf("task billing enum dimension %q has invalid multiplier for %q", name, value)
			}
			seen[normalized] = struct{}{}
		}
		if defaultValue != "" {
			if _, ok := seen[defaultValue]; !ok {
				return fmt.Errorf("task billing enum dimension %q default %q is not configured", name, defaultValue)
			}
		}
		return nil
	default:
		return fmt.Errorf("task billing dimension %q has unsupported kind %q", name, dimension.Kind)
	}
}

func validateRound(round string) error {
	switch normalizeRound(round) {
	case RoundNone, RoundCeil, RoundFloor, RoundNearest:
		return nil
	default:
		return fmt.Errorf("unsupported round mode %q", round)
	}
}

// Evaluate returns named multipliers for the effective JSON request body.
// Callers multiply the returned values with the configured ModelPrice.
func Evaluate(config Config, body []byte) (map[string]float64, error) {
	evaluation, err := EvaluatePricing(config, body)
	if err != nil {
		return nil, err
	}
	return evaluation.Ratios, nil
}

// EvaluatePricing returns both multiplicative quantities and additive prices.
func EvaluatePricing(config Config, body []byte) (Evaluation, error) {
	if err := Validate(config); err != nil {
		return Evaluation{}, err
	}

	var ratios map[string]float64
	switch config.Mode {
	case ModePerRequest:
	case ModePerSecond:
		dimension := *config.Duration
		if dimension.Name == "" {
			dimension.Name = "duration"
		}
		if dimension.Kind == "" {
			dimension.Kind = DimensionNumber
		}
		value, err := evaluateDimension(dimension, body)
		if err != nil {
			return Evaluation{}, err
		}
		ratios = map[string]float64{dimension.Name: value}
	case ModeParametric:
		ratios = make(map[string]float64, len(config.Dimensions))
		for _, dimension := range config.Dimensions {
			value, err := evaluateDimension(dimension, body)
			if err != nil {
				return Evaluation{}, err
			}
			ratios[dimension.Name] = value
		}
	default:
		return Evaluation{}, fmt.Errorf("unsupported task billing mode %q", config.Mode)
	}

	var surchargeResult *SurchargeResult
	if config.Surcharge != nil {
		count, err := evaluateItemCount(*config.Surcharge, body)
		if err != nil {
			return Evaluation{}, err
		}
		billableCount := max(count-config.Surcharge.FreeCount, 0)
		surchargeResult = &SurchargeResult{
			Name:          config.Surcharge.Name,
			Count:         count,
			FreeCount:     config.Surcharge.FreeCount,
			BillableCount: billableCount,
			UnitPrice:     config.Surcharge.UnitPrice,
			Price:         float64(billableCount) * config.Surcharge.UnitPrice,
		}
	}
	return Evaluation{Ratios: ratios, Surcharge: surchargeResult}, nil
}

func evaluateItemCount(surcharge Surcharge, body []byte) (int, error) {
	var raw gjson.Result
	for _, path := range surcharge.Paths {
		candidate := gjson.GetBytes(body, path)
		if !candidate.Exists() || candidate.Value() == nil {
			continue
		}
		if candidate.IsArray() && len(candidate.Array()) == 0 {
			continue
		}
		if candidate.Type == gjson.String && strings.TrimSpace(candidate.String()) == "" {
			continue
		}
		raw = candidate
		break
	}
	if !raw.Exists() {
		return 0, nil
	}
	if !raw.IsArray() {
		if raw.Type == gjson.String {
			return 1, nil
		}
		return 0, fmt.Errorf("task billing surcharge %q requires an array or string", surcharge.Name)
	}
	allowed := make(map[string]struct{}, len(surcharge.ItemTypes))
	for _, itemType := range surcharge.ItemTypes {
		allowed[normalizeEnum(itemType)] = struct{}{}
	}
	count := 0
	for _, item := range raw.Array() {
		if item.Type == gjson.String || len(allowed) == 0 {
			count++
			continue
		}
		if _, ok := allowed[normalizeEnum(item.Get("type").String())]; ok {
			count++
		}
	}
	return count, nil
}

func evaluateDimension(dimension Dimension, body []byte) (float64, error) {
	raw, found := findValue(body, dimension.Paths)
	switch dimension.Kind {
	case DimensionNumber:
		value, ok := numberFromResult(raw, found)
		if !ok {
			if dimension.Default == nil {
				return 0, fmt.Errorf("task billing dimension %q is required", dimension.Name)
			}
			value, ok = numberValue(dimension.Default)
		}
		if !ok || !isFinite(value) || value <= 0 {
			return 0, fmt.Errorf("task billing dimension %q must be a positive number", dimension.Name)
		}
		value = roundValue(value, dimension.Round)
		if value <= 0 {
			return 0, fmt.Errorf("task billing dimension %q rounds to a non-positive number", dimension.Name)
		}
		unit := dimension.Unit
		if unit == 0 {
			unit = 1
		}
		return value / unit, nil
	case DimensionEnum:
		value := ""
		if found {
			value = normalizeEnum(raw.String())
		}
		if value == "" && dimension.Default != nil {
			value = normalizeEnum(fmt.Sprint(dimension.Default))
		}
		if value == "" {
			return 0, fmt.Errorf("task billing dimension %q is required", dimension.Name)
		}
		for candidate, multiplier := range dimension.Values {
			if normalizeEnum(candidate) == value {
				return multiplier, nil
			}
		}
		return 0, fmt.Errorf("task billing dimension %q does not support value %q", dimension.Name, value)
	default:
		return 0, fmt.Errorf("task billing dimension %q has unsupported kind %q", dimension.Name, dimension.Kind)
	}
}

func findValue(body []byte, paths []string) (gjson.Result, bool) {
	for _, path := range paths {
		value := gjson.GetBytes(body, path)
		if value.Exists() && value.Value() != nil {
			return value, true
		}
	}
	return gjson.Result{}, false
}

func numberFromResult(value gjson.Result, found bool) (float64, bool) {
	if !found {
		return 0, false
	}
	if value.Type == gjson.Number {
		return value.Float(), true
	}
	return numberValue(value.String())
}

func numberValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func roundValue(value float64, round string) float64 {
	switch normalizeRound(round) {
	case RoundCeil:
		return math.Ceil(value)
	case RoundFloor:
		return math.Floor(value)
	case RoundNearest:
		return math.Round(value)
	default:
		return value
	}
}

func normalizeRound(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return RoundNone
	}
	return value
}

func normalizeEnum(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func isFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}
