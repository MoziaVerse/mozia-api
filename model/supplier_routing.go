package model

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"gorm.io/gorm"
)

const SupplierRoutingOptionKey = "SupplierRoutingPublication"

type Supplier struct {
	SupplierResourceMeta
	ID         int64  `json:"id" gorm:"primaryKey"`
	Name       string `json:"name" gorm:"type:varchar(191)"`
	Region     string `json:"region" gorm:"type:varchar(191)"`
	Contact    string `json:"contact" gorm:"type:text"`
	DataPolicy string `json:"data_policy" gorm:"type:text"`
	Terms      string `json:"terms" gorm:"type:text"`
	Enabled    bool   `json:"enabled"`
}

type SupplierLimits struct {
	Concurrency int64 `json:"concurrency"`
	RPM         int64 `json:"rpm"`
	TPM         int64 `json:"tpm"`
}

type SupplierModelSpec struct {
	Name            string         `json:"name"`
	Version         string         `json:"version"`
	ContextTokens   int64          `json:"context_tokens"`
	MaxOutputTokens int64          `json:"max_output_tokens"`
	Tools           bool           `json:"tools"`
	JSON            bool           `json:"json"`
	Limits          SupplierLimits `json:"limits"`
}

type SupplierPool struct {
	SupplierResourceMeta
	ID                  int64               `json:"id" gorm:"primaryKey"`
	SupplierID          int64               `json:"supplier_id" gorm:"index"`
	Name                string              `json:"name" gorm:"type:varchar(191)"`
	FailureDomain       string              `json:"failure_domain" gorm:"type:varchar(191)"`
	Enabled             bool                `json:"enabled"`
	Limits              SupplierLimits      `json:"limits" gorm:"embedded;embeddedPrefix:limit_"`
	MaxExecutionSeconds int64               `json:"max_execution_seconds"`
	InputSafetyPercent  int64               `json:"input_safety_percent"`
	Models              []SupplierModelSpec `json:"models" gorm:"-"`
	ModelsJSON          string              `json:"-" gorm:"type:text"`
	Acceptance          string              `json:"acceptance" gorm:"type:text"`
	// Only populated by the pool editor API; compiled runtime snapshots use the binding table.
	Bindings []SupplierPoolBinding `json:"bindings,omitempty" gorm:"-"`
}

type SupplierPoolBinding struct {
	ChannelID int    `json:"channel_id"`
	Model     string `json:"model"`
}

type SupplierBinding struct {
	SupplierResourceMeta
	ID        int64   `json:"id" gorm:"primaryKey"`
	ActiveKey *string `json:"-" gorm:"type:varchar(64);uniqueIndex"`
	ChannelID int     `json:"channel_id" gorm:"index"`
	Model     string  `json:"model"`
	PoolID    int64   `json:"pool_id" gorm:"index"`
}

type SupplierTarget struct {
	SupplierID int64 `json:"supplier_id"`
	Weight     int64 `json:"weight"`
}

type SupplierHealthPolicy struct {
	WindowSeconds   int64 `json:"window_seconds"`
	MinSamples      int64 `json:"min_samples"`
	FailurePercent  int64 `json:"failure_percent"`
	MaxTTFTMs       int64 `json:"max_ttft_ms"`
	CooldownSeconds int64 `json:"cooldown_seconds"`
	TrialPercent    int64 `json:"trial_percent"`
}

type SupplierRoutingRule struct {
	SupplierResourceMeta
	TargetsJSON        string               `json:"-" gorm:"type:text"`
	HealthJSON         string               `json:"-" gorm:"type:text"`
	MaxSupplierPercent int64                `json:"max_supplier_percent"`
	ID                 string               `json:"id" gorm:"primaryKey;type:varchar(96)"`
	Model              string               `json:"model"`
	Group              string               `json:"group"`
	UserID             int                  `json:"user_id"`
	Mode               string               `json:"mode"`
	Targets            []SupplierTarget     `json:"targets" gorm:"-"`
	MaxAttempts        int                  `json:"max_attempts"`
	TimeoutSeconds     int64                `json:"timeout_seconds"`
	Health             SupplierHealthPolicy `json:"health" gorm:"-"`
}

type SupplierRoutingConfig struct {
	Revision      int64                 `json:"revision"`
	Enabled       bool                  `json:"enabled"`
	Shadow        bool                  `json:"shadow"`
	CanaryPercent int                   `json:"canary_percent"`
	Suppliers     []Supplier            `json:"suppliers"`
	Pools         []SupplierPool        `json:"pools"`
	Bindings      []SupplierBinding     `json:"bindings"`
	Rules         []SupplierRoutingRule `json:"rules"`
}

type RoutingRevision struct {
	ResourceKind string `json:"resource_kind" gorm:"type:varchar(32)"`
	ResourceID   string `json:"resource_id" gorm:"type:varchar(96)"`
	AppliedAt    int64  `json:"applied_at"`
	ID           int64  `json:"id" gorm:"primaryKey"`
	CreatedBy    int    `json:"created_by"`
	CreatedAt    int64  `json:"created_at"`
	ConfigJSON   string `json:"config_json"`
}

type SupplierAttempt struct {
	OutcomeClass      string `json:"outcome_class" gorm:"type:varchar(32)"`
	PriorityFallback  bool   `json:"priority_fallback"`
	CapacityUntil     int64  `json:"capacity_until"`
	ID                int64  `json:"id" gorm:"primaryKey"`
	RequestID         string `json:"request_id" gorm:"type:varchar(96);uniqueIndex:uq_supplier_attempt,priority:1"`
	Attempt           int    `json:"attempt" gorm:"uniqueIndex:uq_supplier_attempt,priority:2"`
	SupplierID        int64  `json:"supplier_id" gorm:"index"`
	PoolID            int64  `json:"pool_id" gorm:"index"`
	ChannelID         int    `json:"channel_id" gorm:"index"`
	UserID            int    `json:"user_id"`
	Model             string `json:"model" gorm:"type:varchar(191)"`
	GroupName         string `json:"group_name" gorm:"type:varchar(191)"`
	Revision          int64  `json:"revision"`
	Reason            string `json:"reason" gorm:"type:varchar(191)"`
	Kind              string `json:"kind" gorm:"type:varchar(32)"`
	Status            string `json:"status" gorm:"type:varchar(32);index"`
	CreatedAt         int64  `json:"created_at" gorm:"index"`
	FinishedAt        int64  `json:"finished_at"`
	StatusCode        int    `json:"status_code"`
	LatencyMs         int64  `json:"latency_ms"`
	TTFTMs            int64  `json:"ttft_ms"`
	InputTokens       int64  `json:"input_tokens"`
	OutputTokens      int64  `json:"output_tokens"`
	UsageJSON         string `json:"usage_json" gorm:"type:text"`
	PriceJSON         string `json:"price_json" gorm:"type:text"`
	Cost              string `json:"cost" gorm:"type:varchar(64)"`
	Currency          string `json:"currency" gorm:"type:varchar(3)"`
	CostStatus        string `json:"cost_status" gorm:"type:varchar(32)"`
	UpstreamRequestID string `json:"upstream_request_id" gorm:"type:varchar(191)"`
}

func ReadSupplierRoutingConfig() (*SupplierRoutingConfig, error) {
	var option Option
	err := DB.Where(&Option{Key: SupplierRoutingOptionKey}).First(&option).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &SupplierRoutingConfig{}, nil
	}
	if err != nil {
		return nil, err
	}
	var cfg SupplierRoutingConfig
	if err := common.UnmarshalJsonStr(option.Value, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func ValidateSupplierRoutingConfig(cfg *SupplierRoutingConfig) error {
	return ValidateSupplierRoutingConfigWithDB(DB, cfg)
}

func ValidateSupplierRoutingConfigWithDB(db *gorm.DB, cfg *SupplierRoutingConfig) error {
	if len(cfg.Suppliers) > 128 || len(cfg.Pools) > 128 || len(cfg.Bindings) > 256 || len(cfg.Rules) > 128 {
		return errors.New("supplier routing configuration is too large")
	}
	if cfg.CanaryPercent < 0 || cfg.CanaryPercent > 100 {
		return errors.New("canary percent must be between 0 and 100")
	}
	suppliers := make(map[int64]bool)
	for _, s := range cfg.Suppliers {
		if s.ID <= 0 || s.ID > 9007199254740991 || suppliers[s.ID] || strings.TrimSpace(s.Name) == "" || len(s.Name) > 191 || len(s.Contact) > 4000 || len(s.DataPolicy) > 16000 || len(s.Terms) > 16000 {
			return errors.New("supplier IDs and names must be valid and unique")
		}
		suppliers[s.ID] = true
	}
	pools := make(map[int64]SupplierPool)
	for _, p := range cfg.Pools {
		if p.ID <= 0 || p.ID > 9007199254740991 || pools[p.ID].ID != 0 || !suppliers[p.SupplierID] || strings.TrimSpace(p.Name) == "" || len(p.Name) > 191 || strings.TrimSpace(p.FailureDomain) == "" || len(p.FailureDomain) > 191 {
			return errors.New("pool must have a unique ID, supplier, name and failure domain")
		}
		if p.Limits.Concurrency <= 0 || p.Limits.RPM <= 0 || p.Limits.TPM <= 0 || p.Limits.Concurrency > 10000 || p.Limits.RPM > 100000 || p.Limits.TPM > 1000000000 {
			return errors.New("pool concurrency, RPM and TPM must be positive and within supported bounds")
		}
		if p.MaxExecutionSeconds < 1 || p.MaxExecutionSeconds > 3600 || p.InputSafetyPercent < 100 || p.InputSafetyPercent > 200 {
			return errors.New("pool execution timeout must be 1..3600 seconds and input safety 100..200 percent")
		}
		if p.Enabled && strings.TrimSpace(p.Acceptance) == "" {
			return errors.New("enabled pools require an acceptance/load-test reference")
		}
		if len(p.Models) == 0 || len(p.Models) > 128 {
			return errors.New("pool requires 1..128 verified model specifications")
		}
		names := make(map[string]bool)
		for _, spec := range p.Models {
			if spec.Name == "" || len(spec.Name) > 191 || names[spec.Name] || spec.Version == "" || len(spec.Version) > 191 || spec.ContextTokens <= 0 || spec.ContextTokens > 10000000 || spec.MaxOutputTokens <= 0 || spec.MaxOutputTokens > spec.ContextTokens {
				return errors.New("model name, version, context and output bounds must be valid")
			}
			if spec.Limits.Concurrency < 0 || spec.Limits.RPM < 0 || spec.Limits.TPM < 0 || spec.Limits.Concurrency > p.Limits.Concurrency || spec.Limits.RPM > p.Limits.RPM || spec.Limits.TPM > p.Limits.TPM {
				return errors.New("model limits must fit the shared pool; zero inherits the pool limit")
			}
			names[spec.Name] = true
		}
		pools[p.ID] = p
	}
	bindings := make(map[string]bool)
	channelSuppliers := make(map[int]int64)
	for _, b := range cfg.Bindings {
		key := fmt.Sprintf("%d:%s", b.ChannelID, b.Model)
		p, ok := pools[b.PoolID]
		if !ok || bindings[key] {
			return errors.New("binding must reference one existing pool per channel/model")
		}
		var ch Channel
		if err := db.First(&ch, b.ChannelID).Error; err != nil {
			return fmt.Errorf("binding channel: %w", err)
		}
		if ch.Type != constant.ChannelTypeOpenAI {
			return errors.New("supplier routing currently requires OpenAI-compatible channels")
		}
		if prior := channelSuppliers[ch.Id]; prior != 0 && prior != p.SupplierID {
			return errors.New("a channel cannot belong to multiple suppliers")
		}
		channelSuppliers[ch.Id] = p.SupplierID
		if !slices.Contains(ch.GetModels(), b.Model) {
			return fmt.Errorf("channel %d does not expose model %s", ch.Id, b.Model)
		}
		if !slices.ContainsFunc(p.Models, func(spec SupplierModelSpec) bool { return spec.Name == b.Model }) {
			return errors.New("binding model has no verified pool specification")
		}
		bindings[key] = true
	}
	ruleIDs := make(map[string]bool)
	matches := make(map[string]bool)
	for _, r := range cfg.Rules {
		match := fmt.Sprintf("%s:%s:%d", r.Model, r.Group, r.UserID)
		if r.ID == "" || len(r.ID) > 96 || ruleIDs[r.ID] || matches[match] || r.Model == "" || len(r.Model) > 191 || len(r.Group) > 191 || r.UserID < 0 {
			return errors.New("rule IDs and customer/group/model matches must be unique")
		}
		if r.Mode != "capacity" && r.Mode != "share" && r.Mode != "failover" {
			return errors.New("supported routing modes: capacity, share, failover")
		}
		if r.MaxAttempts < 1 || r.MaxAttempts > 10 || r.TimeoutSeconds < 1 || r.TimeoutSeconds > 3600 {
			return errors.New("rule attempts must be 1..10 and timeout 1..3600 seconds")
		}
		if r.MaxSupplierPercent < 0 || r.MaxSupplierPercent > 100 {
			return errors.New("supplier concentration percent must be 0..100; zero means no additional cap")
		}
		h := r.Health
		if h.WindowSeconds < 10 || h.WindowSeconds > 3600 || h.MinSamples < 1 || h.MinSamples > 10000 || h.FailurePercent < 1 || h.FailurePercent > 100 || h.MaxTTFTMs < 1 || h.CooldownSeconds < 1 || h.CooldownSeconds > 3600 || h.TrialPercent < 1 || h.TrialPercent > 100 {
			return errors.New("invalid health window, samples, failure threshold, TTFT, cooldown or trial percent")
		}
		if len(r.Targets) == 0 || len(r.Targets) > 32 {
			return errors.New("rule requires 1..32 supplier targets")
		}
		targets := make(map[int64]bool)
		version := ""
		for _, target := range r.Targets {
			if !suppliers[target.SupplierID] || targets[target.SupplierID] || target.Weight < 1 || target.Weight > 10000 {
				return errors.New("targets must reference unique suppliers with positive weights")
			}
			found := false
			for _, b := range cfg.Bindings {
				p := pools[b.PoolID]
				if p.SupplierID != target.SupplierID || b.Model != r.Model {
					continue
				}
				for _, spec := range p.Models {
					if spec.Name == r.Model {
						if version != "" && version != spec.Version {
							return errors.New("all targets of a rule must serve the same verified model version")
						}
						version = spec.Version
						found = true
					}
				}
			}
			if !found {
				return errors.New("target supplier has no binding for the rule model")
			}
			targets[target.SupplierID] = true
		}
		ruleIDs[r.ID] = true
		matches[match] = true
	}
	return nil
}

// PublishSupplierRouting saves an immutable revision and updates its reference with
// compare-and-swap. A stale editor cannot overwrite a concurrent publication.
func PublishSupplierRouting(ctx context.Context, cfg *SupplierRoutingConfig, expected int64, userID int) error {
	return DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var migrated int64
		if err := tx.Model(&Option{}).Where(commonKeyCol+" = ?", SupplierResourceStateKey).Count(&migrated).Error; err != nil {
			return err
		}
		if migrated > 0 {
			return errors.New("whole configuration writes are retired; use supplier resource endpoints")
		}
		var current Option
		err := tx.Where(commonKeyCol+" = ?", SupplierRoutingOptionKey).First(&current).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var old SupplierRoutingConfig
		if current.Value != "" {
			if err := common.UnmarshalJsonStr(current.Value, &old); err != nil {
				return err
			}
		}
		if old.Revision != expected {
			return errors.New("routing configuration changed; reload before publishing")
		}
		for _, prior := range old.Pools {
			for _, next := range cfg.Pools {
				if prior.ID == next.ID && prior.SupplierID != next.SupplierID {
					return errors.New("an existing resource pool cannot be reassigned to another supplier")
				}
			}
		}
		// Bindings stay stable while any attempt might still be executing.
		if !reflect.DeepEqual(old.Bindings, cfg.Bindings) {
			var count int64
			if err := tx.Model(&SupplierAttempt{}).Where("status IN ?", []string{"pending", "unknown"}).Count(&count).Error; err != nil {
				return err
			}
			if count > 0 {
				return errors.New("reconcile pending/unknown attempts before changing resource bindings")
			}
		}
		revision := RoutingRevision{CreatedBy: userID, CreatedAt: time.Now().Unix()}
		if err := tx.Create(&revision).Error; err != nil {
			return err
		}
		cfg.Revision = revision.ID
		data, err := common.Marshal(cfg)
		if err != nil {
			return err
		}
		if err := tx.Model(&revision).Update("config_json", string(data)).Error; err != nil {
			return err
		}
		if current.Value == "" {
			if err := tx.Create(&Option{Key: SupplierRoutingOptionKey, Value: string(data)}).Error; err != nil {
				return err
			}
		} else {
			result := tx.Model(&Option{}).Where(commonKeyCol+" = ? AND value = ?", SupplierRoutingOptionKey, current.Value).Update("value", string(data))
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return errors.New("concurrent routing publication")
			}
		}
		for _, s := range cfg.Suppliers {
			if err := tx.Save(&s).Error; err != nil {
				return err
			}
		}
		for _, p := range cfg.Pools {
			data, err := common.Marshal(p.Models)
			if err != nil {
				return err
			}
			p.ModelsJSON = string(data)
			if err := tx.Save(&p).Error; err != nil {
				return err
			}
		}
		channelIDs := make(map[int]bool)
		for _, b := range old.Bindings {
			channelIDs[b.ChannelID] = true
		}
		for _, b := range cfg.Bindings {
			channelIDs[b.ChannelID] = true
		}
		for id := range channelIDs {
			var ch Channel
			if err := tx.First(&ch, id).Error; err != nil {
				return err
			}
			settings := ch.GetOtherSettings()
			settings.SupplierPools = make(map[string]int64)
			var supplierID int64
			for _, b := range cfg.Bindings {
				if b.ChannelID == id {
					settings.SupplierPools[b.Model] = b.PoolID
					for _, p := range cfg.Pools {
						if p.ID == b.PoolID {
							supplierID = p.SupplierID
						}
					}
				}
			}
			data, err := common.Marshal(settings)
			if err != nil {
				return err
			}
			if err := tx.Model(&ch).Updates(map[string]any{"supplier_id": supplierID, "settings": string(data)}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
