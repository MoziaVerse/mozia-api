package claude

import (
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/setting/reasoning"
)

// Backport the model-aware rendering in upstream 0ed497f06 without moving the
// host's DTOs, billing or model mapping into RelayKit. Native Messages bypass
// this conversion and retain their provider-specific controls.
func applyClaudeReasoning(source dto.GeneralOpenAIRequest, target *dto.ClaudeRequest) error {
	effort := source.ReasoningEffort
	enabled := effort != ""
	var budget *int
	var exclude *bool
	if len(source.Reasoning) > 0 {
		var config struct {
			Enabled   *bool  `json:"enabled"`
			Effort    string `json:"effort"`
			MaxTokens *int   `json:"max_tokens"`
			Exclude   *bool  `json:"exclude"`
		}
		if err := common.Unmarshal(source.Reasoning, &config); err != nil {
			return fmt.Errorf("invalid reasoning: %w", err)
		}
		if config.Effort != "" {
			if effort != "" && effort != config.Effort {
				return fmt.Errorf("reasoning.effort conflicts with reasoning_effort")
			}
			effort = config.Effort
		}
		budget, exclude = config.MaxTokens, config.Exclude
		enabled = enabled || effort != "" || budget != nil
		if config.Enabled != nil {
			if !*config.Enabled {
				if effort != "" && effort != "none" || budget != nil {
					return fmt.Errorf("disabled reasoning cannot specify an effort or budget")
				}
				effort = "none"
			}
			enabled = true
		}
	}
	if !model_setting.ShouldPreserveThinkingSuffix(source.Model) {
		if base, suffixEffort, ok := reasoning.TrimEffortSuffix(source.Model); ok && strings.HasPrefix(source.Model[strings.LastIndex(source.Model, "/")+1:], "claude-") {
			if effort != "" && effort != suffixEffort {
				return fmt.Errorf("reasoning effort conflicts with model suffix")
			}
			target.Model, effort, enabled = base, suffixEffort, true
		} else if model_setting.GetClaudeSettings().ThinkingAdapterEnabled && strings.HasSuffix(source.Model, "-thinking") {
			target.Model = strings.TrimSuffix(source.Model, "-thinking")
			enabled = true
		}
	}
	if !enabled {
		return nil
	}
	if effort == "none" {
		target.Thinking = &dto.Thinking{Type: "disabled"}
		return nil
	}
	switch effort {
	case "", "minimal", "low", "medium", "high", "xhigh", "max":
	default:
		return fmt.Errorf("unsupported reasoning effort %q", effort)
	}
	model := target.Model[strings.LastIndex(target.Model, "/")+1:]
	adaptiveOnly := strings.HasPrefix(model, "claude-opus-4-7") || strings.HasPrefix(model, "claude-opus-4-8") ||
		strings.HasPrefix(model, "claude-opus-5") || strings.HasPrefix(model, "claude-sonnet-5")
	adaptive := adaptiveOnly || strings.HasPrefix(model, "claude-opus-4-6") || strings.HasPrefix(model, "claude-sonnet-4-6")
	if adaptive && (adaptiveOnly || budget == nil) {
		if effort == "" {
			effort = "high"
		}
		if effort == "minimal" {
			effort = "low"
		}
		if effort == "xhigh" && !adaptiveOnly {
			effort = "max"
		}
		target.Thinking = &dto.Thinking{Type: "adaptive"}
		target.OutputConfig, _ = common.Marshal(map[string]string{"effort": effort})
		if adaptiveOnly {
			// Keep the local visible-summary fix for Claude Code.
			target.Thinking.Display = "summarized"
			target.Temperature, target.TopP, target.TopK = nil, nil, nil
		}
	} else {
		if target.MaxTokens == nil || *target.MaxTokens <= 1024 {
			return fmt.Errorf("max_tokens must be greater than 1024 for manual Claude thinking")
		}
		if budget == nil {
			percentage := model_setting.GetClaudeSettings().ThinkingAdapterBudgetTokensPercentage
			switch effort {
			case "minimal":
				percentage = 0.05
			case "low":
				percentage = 0.2
			case "medium":
				percentage = 0.5
			case "high":
				percentage = 0.8
			case "xhigh", "max":
				percentage = 0.95
			}
			value := int(float64(*target.MaxTokens) * percentage)
			budget = &value
		} else if *budget < 1024 || uint(*budget) >= *target.MaxTokens {
			return fmt.Errorf("thinking budget must satisfy 1024 <= budget_tokens < max_tokens")
		}
		value := max(1024, min(*budget, int(*target.MaxTokens)-1))
		target.Thinking = &dto.Thinking{Type: "enabled", BudgetTokens: &value}
		target.Temperature, target.TopK = nil, nil
		if target.TopP != nil && (*target.TopP < 0.95 || *target.TopP > 1) {
			target.TopP = nil
		}
	}
	if exclude != nil {
		if *exclude {
			target.Thinking.Display = "omitted"
		} else {
			target.Thinking.Display = "summarized"
		}
	}
	return nil
}
