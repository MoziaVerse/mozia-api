package model

import (
	"fmt"
	"math"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/taskbilling"
	"github.com/QuantumNous/new-api/setting/billing_setting"

	"gorm.io/gorm"
)

const (
	ChannelCostModePerToken   = "per_token"
	ChannelCostModePerRequest = "per_request"
	ChannelCostModePerSecond  = "per_second"
	ChannelCostModeParametric = "parametric"
	ChannelCostModeTieredExpr = "tiered_expr"
)

type ChannelCostConfig struct {
	Items               map[string]float64  `json:"items,omitempty"`
	BasePrice           *float64            `json:"base_price,omitempty"`
	ReferenceVideoPrice *float64            `json:"reference_video_price,omitempty"`
	BillingExpr         string              `json:"billing_expr,omitempty"`
	TaskBilling         *taskbilling.Config `json:"task_billing,omitempty"`
}

type ChannelCostPricing struct {
	Id         int64             `json:"id" gorm:"primaryKey;autoIncrement"`
	ChannelId  int               `json:"channel_id" gorm:"not null;uniqueIndex:uq_channel_cost_pricing,priority:1"`
	ModelName  string            `json:"model_name" gorm:"type:varchar(191);not null;uniqueIndex:uq_channel_cost_pricing,priority:2"`
	Currency   string            `json:"currency" gorm:"type:varchar(3);not null"`
	Mode       string            `json:"mode" gorm:"type:varchar(32);not null"`
	ConfigJson string            `json:"-" gorm:"type:text;not null"`
	Note       string            `json:"note" gorm:"type:text;not null"`
	Config     ChannelCostConfig `json:"config" gorm:"-"`
}

func ValidateChannelCostPricing(cost *ChannelCostPricing) error {
	cost.ModelName = strings.TrimSpace(cost.ModelName)
	cost.Currency = strings.ToUpper(strings.TrimSpace(cost.Currency))
	cost.Mode = strings.TrimSpace(cost.Mode)
	cost.Note = strings.TrimSpace(cost.Note)
	if cost.ChannelId <= 0 {
		return fmt.Errorf("channel is required")
	}
	if cost.ModelName == "" || len(cost.ModelName) > 191 {
		return fmt.Errorf("model name is invalid")
	}
	if cost.Currency != "CNY" && cost.Currency != "USD" {
		return fmt.Errorf("currency must be CNY or USD")
	}
	if len(cost.Note) > 20000 {
		return fmt.Errorf("note is too long")
	}
	if _, err := GetChannelById(cost.ChannelId, false); err != nil {
		return fmt.Errorf("channel does not exist: %w", err)
	}

	validatePrice := func(name string, value *float64) error {
		if value == nil || math.IsNaN(*value) || math.IsInf(*value, 0) || *value < 0 {
			return fmt.Errorf("%s must be a non-negative number", name)
		}
		return nil
	}
	if cost.Config.ReferenceVideoPrice != nil {
		if err := validatePrice("reference video price", cost.Config.ReferenceVideoPrice); err != nil {
			return err
		}
	}

	switch cost.Mode {
	case ChannelCostModePerToken:
		if len(cost.Config.Items) == 0 {
			return fmt.Errorf("per-token pricing requires at least one price item")
		}
		for name, price := range cost.Config.Items {
			if strings.TrimSpace(name) == "" {
				return fmt.Errorf("price item name is required")
			}
			if err := validatePrice("price item", &price); err != nil {
				return err
			}
		}
	case ChannelCostModePerRequest:
		if err := validatePrice("base price", cost.Config.BasePrice); err != nil {
			return err
		}
	case ChannelCostModePerSecond, ChannelCostModeParametric:
		if err := validatePrice("base price", cost.Config.BasePrice); err != nil {
			return err
		}
		if cost.Config.TaskBilling == nil {
			return fmt.Errorf("task billing configuration is required")
		}
		expectedMode := taskbilling.ModePerSecond
		if cost.Mode == ChannelCostModeParametric {
			expectedMode = taskbilling.ModeParametric
		}
		if cost.Config.TaskBilling.Mode != expectedMode {
			return fmt.Errorf("task billing mode must be %s", expectedMode)
		}
		if err := taskbilling.Validate(*cost.Config.TaskBilling); err != nil {
			return err
		}
	case ChannelCostModeTieredExpr:
		cost.Config.BillingExpr = strings.TrimSpace(cost.Config.BillingExpr)
		if cost.Config.BillingExpr == "" {
			return fmt.Errorf("billing expression is required")
		}
		if err := billing_setting.SmokeTestExpr(cost.Config.BillingExpr); err != nil {
			return fmt.Errorf("invalid billing expression: %w", err)
		}
	default:
		return fmt.Errorf("unsupported pricing mode %q", cost.Mode)
	}
	return nil
}

func ListChannelCostPricing() ([]ChannelCostPricing, error) {
	var costs []ChannelCostPricing
	if err := DB.Order("model_name ASC").Order("channel_id ASC").Find(&costs).Error; err != nil {
		return nil, err
	}
	for i := range costs {
		if err := common.UnmarshalJsonStr(costs[i].ConfigJson, &costs[i].Config); err != nil {
			return nil, fmt.Errorf("decode channel cost %d: %w", costs[i].Id, err)
		}
	}
	return costs, nil
}

func UpsertChannelCostPricing(cost *ChannelCostPricing) error {
	if err := ValidateChannelCostPricing(cost); err != nil {
		return err
	}
	config, err := common.Marshal(cost.Config)
	if err != nil {
		return err
	}
	cost.ConfigJson = string(config)
	return DB.Where("channel_id = ? AND model_name = ?", cost.ChannelId, cost.ModelName).
		Assign(map[string]any{
			"currency":    cost.Currency,
			"mode":        cost.Mode,
			"config_json": cost.ConfigJson,
			"note":        cost.Note,
		}).FirstOrCreate(cost).Error
}

func DeleteChannelCostPricing(id int64) (bool, error) {
	if id <= 0 {
		return false, gorm.ErrRecordNotFound
	}
	result := DB.Delete(&ChannelCostPricing{}, id)
	return result.RowsAffected > 0, result.Error
}
