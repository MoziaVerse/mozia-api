package model

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const SupplierResourceStateKey = "SupplierRoutingResourceState"
const SupplierRoutingSettingsKey = "SupplierRoutingSettings"

type SupplierResourceMeta struct {
	Version         int64          `json:"version"`
	RuntimeRevision int64          `json:"runtime_revision"`
	CreationKey     *string        `json:"-" gorm:"type:varchar(64);uniqueIndex"`
	RequestHash     string         `json:"-" gorm:"type:varchar(64)"`
	DeletedAt       gorm.DeletedAt `json:"-" gorm:"index"`
}

type SupplierRoutingSettings struct {
	Version         int64 `json:"version"`
	Enabled         bool  `json:"enabled"`
	Shadow          bool  `json:"shadow"`
	CanaryPercent   int   `json:"canary_percent"`
	RuntimeRevision int64 `json:"runtime_revision"`
}

type SupplierResourceState struct {
	TargetRevision  int64 `json:"target_revision"`
	AppliedRevision int64 `json:"applied_revision"`
}

type SupplierResourceError struct {
	Status      int
	Code        string
	Message     string
	FieldErrors map[string]string
}

func (e *SupplierResourceError) Error() string { return e.Message }
func SupplierResourceConflict(code, message string) error {
	return &SupplierResourceError{Status: 409, Code: code, Message: message}
}
func SupplierFieldError(field, message string) error {
	return &SupplierResourceError{Status: 422, Code: "validation_failed", Message: message, FieldErrors: map[string]string{field: message}}
}

func (p *SupplierPool) BeforeSave(tx *gorm.DB) error {
	data, err := common.Marshal(p.Models)
	p.ModelsJSON = string(data)
	return err
}
func (p *SupplierPool) AfterFind(tx *gorm.DB) error {
	if p.ModelsJSON == "" {
		return nil
	}
	return common.UnmarshalJsonStr(p.ModelsJSON, &p.Models)
}
func (r *SupplierRoutingRule) BeforeSave(tx *gorm.DB) error {
	data, err := common.Marshal(r.Targets)
	if err != nil {
		return err
	}
	r.TargetsJSON = string(data)
	data, err = common.Marshal(r.Health)
	r.HealthJSON = string(data)
	return err
}
func (r *SupplierRoutingRule) AfterFind(tx *gorm.DB) error {
	if r.TargetsJSON != "" {
		if err := common.UnmarshalJsonStr(r.TargetsJSON, &r.Targets); err != nil {
			return err
		}
	}
	if r.HealthJSON != "" {
		return common.UnmarshalJsonStr(r.HealthJSON, &r.Health)
	}
	return nil
}

func ReadSupplierOption(db *gorm.DB, key string, dest any) error {
	var option Option
	if err := db.Where(&Option{Key: key}).First(&option).Error; err != nil {
		return err
	}
	return common.UnmarshalJsonStr(option.Value, dest)
}
func WriteSupplierOption(db *gorm.DB, key string, value any) error {
	data, err := common.Marshal(value)
	if err != nil {
		return err
	}
	return db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "key"}}, DoUpdates: clause.AssignmentColumns([]string{"value"})}).Create(&Option{Key: key, Value: string(data)}).Error
}

// The row also serializes channel mutations with resource validation on every SQL dialect.
func LockSupplierResources(db *gorm.DB) error {
	return db.Model(&Option{}).Where(&Option{Key: SupplierResourceStateKey}).Update("value", gorm.Expr("value")).Error
}

func ReadSupplierResources(db *gorm.DB) (*SupplierRoutingConfig, error) {
	var settings SupplierRoutingSettings
	if err := ReadSupplierOption(db, SupplierRoutingSettingsKey, &settings); err != nil {
		return nil, err
	}
	var state SupplierResourceState
	if err := ReadSupplierOption(db, SupplierResourceStateKey, &state); err != nil {
		return nil, err
	}
	cfg := &SupplierRoutingConfig{Revision: state.TargetRevision, Enabled: settings.Enabled, Shadow: settings.Shadow, CanaryPercent: settings.CanaryPercent,
		Suppliers: []Supplier{}, Pools: []SupplierPool{}, Bindings: []SupplierBinding{}, Rules: []SupplierRoutingRule{}}
	for _, query := range []struct {
		dest  any
		order string
	}{{&cfg.Suppliers, "id"}, {&cfg.Pools, "id"}, {&cfg.Bindings, "id"}, {&cfg.Rules, "id"}} {
		if err := db.Order(query.order).Find(query.dest).Error; err != nil {
			return nil, err
		}
	}
	return cfg, nil
}

// Migrate only the active legacy document. Old projection rows are not active resources.
// The runtime document and Redis capacity ledger are deliberately left untouched.
func MigrateSupplierResources() error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&Option{}).Where(&Option{Key: SupplierResourceStateKey}).Count(&count).Error; err != nil {
			return err
		}
		if count != 0 {
			return nil
		}
		var cfg SupplierRoutingConfig
		err := ReadSupplierOption(tx, SupplierRoutingOptionKey, &cfg)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := ValidateSupplierRoutingConfigWithDB(tx, &cfg); err != nil {
			return fmt.Errorf("supplier resource migration: %w", err)
		}
		for _, dest := range []any{&Supplier{}, &SupplierPool{}} {
			if err := tx.Where("1 = 1").Delete(dest).Error; err != nil {
				return err
			}
		}
		for i := range cfg.Suppliers {
			cfg.Suppliers[i].SupplierResourceMeta = SupplierResourceMeta{Version: 1, RuntimeRevision: cfg.Revision}
			if err := tx.Unscoped().Save(&cfg.Suppliers[i]).Error; err != nil {
				return err
			}
		}
		for i := range cfg.Pools {
			cfg.Pools[i].SupplierResourceMeta = SupplierResourceMeta{Version: 1, RuntimeRevision: cfg.Revision}
			if err := tx.Unscoped().Save(&cfg.Pools[i]).Error; err != nil {
				return err
			}
		}
		for i := range cfg.Bindings {
			b := &cfg.Bindings[i]
			b.ID = 0
			b.SupplierResourceMeta = SupplierResourceMeta{Version: 1, RuntimeRevision: cfg.Revision}
			key := SupplierBindingKey(b)
			b.ActiveKey = &key
			if err := tx.Create(b).Error; err != nil {
				return err
			}
		}
		for i := range cfg.Rules {
			cfg.Rules[i].SupplierResourceMeta = SupplierResourceMeta{Version: 1, RuntimeRevision: cfg.Revision}
			if err := tx.Create(&cfg.Rules[i]).Error; err != nil {
				return err
			}
		}
		if err := ProjectSupplierBindings(tx, &cfg); err != nil {
			return err
		}
		if err := WriteSupplierOption(tx, SupplierRoutingSettingsKey, SupplierRoutingSettings{Version: 1, Enabled: cfg.Enabled, Shadow: cfg.Shadow, CanaryPercent: cfg.CanaryPercent, RuntimeRevision: cfg.Revision}); err != nil {
			return err
		}
		if err := WriteSupplierOption(tx, SupplierResourceStateKey, SupplierResourceState{TargetRevision: cfg.Revision, AppliedRevision: cfg.Revision}); err != nil {
			return err
		}
		// Legacy manually assigned numeric IDs did not advance PostgreSQL sequences.
		if tx.Dialector.Name() == "postgres" {
			for _, table := range []string{"suppliers", "supplier_pools"} {
				if err := tx.Exec("SELECT setval(pg_get_serial_sequence('" + table + "','id'), COALESCE((SELECT MAX(id) FROM " + table + "), 0) + 1, false)").Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func SupplierBindingKey(b *SupplierBinding) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%d:%s", b.ChannelID, b.Model))))
}

// Resource paths are an internal enum, never table names supplied by a client.
func NewSupplierResource(kind string) any {
	switch kind {
	case "supplier":
		return &Supplier{}
	case "pool":
		return &SupplierPool{}
	case "binding":
		return &SupplierBinding{}
	case "rule":
		return &SupplierRoutingRule{}
	case "settings":
		return &SupplierRoutingSettings{}
	default:
		return nil
	}
}
func SupplierResourceIdentity(value any) (string, int64) {
	switch v := value.(type) {
	case *Supplier:
		return strconv.FormatInt(v.ID, 10), v.Version
	case *SupplierPool:
		return strconv.FormatInt(v.ID, 10), v.Version
	case *SupplierBinding:
		return strconv.FormatInt(v.ID, 10), v.Version
	case *SupplierRoutingRule:
		return v.ID, v.Version
	case *SupplierRoutingSettings:
		return "settings", v.Version
	}
	return "", 0
}
func SupplierResourceMetadata(value any) *SupplierResourceMeta {
	switch v := value.(type) {
	case *Supplier:
		return &v.SupplierResourceMeta
	case *SupplierPool:
		return &v.SupplierResourceMeta
	case *SupplierBinding:
		return &v.SupplierResourceMeta
	case *SupplierRoutingRule:
		return &v.SupplierResourceMeta
	}
	return nil
}
func ReadSupplierResource(db *gorm.DB, kind, id string) (any, error) {
	value := NewSupplierResource(kind)
	if value == nil {
		return nil, gorm.ErrRecordNotFound
	}
	if kind == "settings" {
		return value, ReadSupplierOption(db, SupplierRoutingSettingsKey, value)
	}
	if err := db.Where("id = ?", id).First(value).Error; err != nil {
		return nil, err
	}
	if pool, ok := value.(*SupplierPool); ok {
		return pool, LoadSupplierPoolBindings(db, pool)
	}
	return value, nil
}

func LoadSupplierPoolBindings(db *gorm.DB, pool *SupplierPool) error {
	pool.Bindings = []SupplierPoolBinding{}
	return db.Model(&SupplierBinding{}).Select("channel_id", "model").Where("pool_id = ?", pool.ID).Order("id").Find(&pool.Bindings).Error
}

// Replace the selected pool's associations in the caller's publication transaction.
// Unchanged associations retain their identity, version and capacity accounting.
func ReplaceSupplierPoolBindings(tx *gorm.DB, pool *SupplierPool) (bool, error) {
	if len(pool.Bindings) > 256 {
		return false, SupplierFieldError("bindings", "at most 256 channel/model associations are supported")
	}
	selected := make(map[string]SupplierBinding, len(pool.Bindings))
	for i, b := range pool.Bindings {
		value := SupplierBinding{ChannelID: b.ChannelID, Model: b.Model, PoolID: pool.ID}
		if err := ValidateSupplierResourceFields(&value); err != nil {
			return false, SupplierFieldError(fmt.Sprintf("bindings.%d", i), err.Error())
		}
		key := SupplierBindingKey(&value)
		if _, exists := selected[key]; exists {
			return false, SupplierFieldError(fmt.Sprintf("bindings.%d", i), "duplicate channel/model association")
		}
		value.ActiveKey = &key
		selected[key] = value
	}
	var prior []SupplierBinding
	if err := tx.Where("pool_id = ?", pool.ID).Find(&prior).Error; err != nil {
		return false, err
	}
	changed := false
	for _, b := range prior {
		key := SupplierBindingKey(&b)
		if _, keep := selected[key]; keep {
			delete(selected, key)
			continue
		}
		changed = true
		if err := tx.Model(&b).Updates(map[string]any{"active_key": nil, "version": b.Version + 1}).Error; err != nil {
			return false, err
		}
		if err := tx.Delete(&b).Error; err != nil {
			return false, err
		}
	}
	for key, b := range selected {
		var count int64
		if err := tx.Model(&SupplierBinding{}).Where("active_key = ?", key).Count(&count).Error; err != nil {
			return false, err
		}
		if count > 0 {
			return false, SupplierResourceConflict("binding_exists", "this channel/model already belongs to another resource pool")
		}
		changed = true
		b.Version = 1
		if err := tx.Create(&b).Error; err != nil {
			return false, err
		}
	}
	return changed, nil
}

// PATCH merges objects, replaces arrays, and rejects unknown/read-only/null fields.
// Reflect over the existing domain types so nested model/health schemas cannot drift.
func PatchSupplierResource(value any, patch json.RawMessage) error {
	return patchSupplierValue(reflect.ValueOf(value).Elem(), patch, "")
}
func patchSupplierValue(dest reflect.Value, data json.RawMessage, path string) error {
	if strings.TrimSpace(string(data)) == "null" {
		return SupplierFieldError(path, "null is not allowed")
	}
	switch dest.Kind() {
	case reflect.Struct:
		var fields map[string]json.RawMessage
		if err := common.Unmarshal(data, &fields); err != nil {
			return SupplierFieldError(path, "expected an object")
		}
		for name, raw := range fields {
			fieldPath := name
			if path != "" {
				fieldPath = path + "." + name
			}
			var field reflect.Value
			for i := 0; i < dest.NumField(); i++ {
				info := dest.Type().Field(i)
				if strings.Split(info.Tag.Get("json"), ",")[0] == name && name != "-" {
					field = dest.Field(i)
					break
				}
			}
			if !field.IsValid() || (path == "" && (name == "id" || name == "supplier_id" || name == "version" || name == "runtime_revision")) {
				return SupplierFieldError(fieldPath, "unknown or read-only field")
			}
			if err := patchSupplierValue(field, raw, fieldPath); err != nil {
				return err
			}
		}
	case reflect.Slice:
		var items []json.RawMessage
		if err := common.Unmarshal(data, &items); err != nil {
			return SupplierFieldError(path, "expected an array")
		}
		next := reflect.MakeSlice(dest.Type(), len(items), len(items))
		for i, raw := range items {
			if err := patchSupplierValue(next.Index(i), raw, fmt.Sprintf("%s.%d", path, i)); err != nil {
				return err
			}
		}
		dest.Set(next)
	default:
		if err := common.Unmarshal(data, dest.Addr().Interface()); err != nil {
			return SupplierFieldError(path, "invalid field type or number")
		}
	}
	return nil
}

// Validation messages are relative to this editor, not to an unrelated full-document draft.
func ValidateSupplierResourceFields(value any) error {
	switch v := value.(type) {
	case *Supplier:
		if strings.TrimSpace(v.Name) == "" || len(v.Name) > 191 {
			return SupplierFieldError("name", "name is required and must be at most 191 bytes")
		}
		for key, val := range map[string]string{"region": v.Region, "contact": v.Contact, "data_policy": v.DataPolicy, "terms": v.Terms} {
			max := 16000
			if key == "region" {
				max = 191
			}
			if key == "contact" {
				max = 4000
			}
			if len(val) > max {
				return SupplierFieldError(key, "value is too long")
			}
		}
	case *SupplierPool:
		if strings.TrimSpace(v.Name) == "" {
			return SupplierFieldError("name", "name is required")
		}
		if strings.TrimSpace(v.FailureDomain) == "" {
			return SupplierFieldError("failure_domain", "failure domain is required")
		}
		for key, n := range map[string]int64{"concurrency": v.Limits.Concurrency, "rpm": v.Limits.RPM, "tpm": v.Limits.TPM} {
			max := int64(10000)
			if key == "rpm" {
				max = 100000
			}
			if key == "tpm" {
				max = 1000000000
			}
			if n < 1 || n > max {
				return SupplierFieldError("limits."+key, "shared limit is outside the supported range")
			}
		}
		if v.MaxExecutionSeconds < 1 || v.MaxExecutionSeconds > 3600 {
			return SupplierFieldError("max_execution_seconds", "execution timeout must be 1..3600 seconds")
		}
		if v.InputSafetyPercent < 100 || v.InputSafetyPercent > 200 {
			return SupplierFieldError("input_safety_percent", "input safety must be 100..200 percent")
		}
		if v.Enabled && strings.TrimSpace(v.Acceptance) == "" {
			return SupplierFieldError("acceptance", "enabled pools require an acceptance/load-test reference")
		}
		if len(v.Models) == 0 || len(v.Models) > 128 {
			return SupplierFieldError("models", "pool requires 1..128 verified models")
		}
		for i, m := range v.Models {
			path := fmt.Sprintf("models.%d.", i)
			if strings.TrimSpace(m.Name) == "" {
				return SupplierFieldError(path+"name", "model name is required")
			}
			if strings.TrimSpace(m.Version) == "" {
				return SupplierFieldError(path+"version", "verified version is required")
			}
			if m.ContextTokens < 1 || m.ContextTokens > 10000000 {
				return SupplierFieldError(path+"context_tokens", "invalid context limit")
			}
			if m.MaxOutputTokens < 1 || m.MaxOutputTokens > m.ContextTokens {
				return SupplierFieldError(path+"max_output_tokens", "output limit must fit the context")
			}
			for key, pair := range map[string][2]int64{"concurrency": {m.Limits.Concurrency, v.Limits.Concurrency}, "rpm": {m.Limits.RPM, v.Limits.RPM}, "tpm": {m.Limits.TPM, v.Limits.TPM}} {
				if pair[0] < 0 || pair[0] > pair[1] {
					return SupplierFieldError(path+"limits."+key, "model limit must fit the shared pool")
				}
			}
		}
	case *SupplierBinding:
		if v.ChannelID < 1 {
			return SupplierFieldError("channel_id", "channel is required")
		}
		if v.Model == "" {
			return SupplierFieldError("model", "model is required")
		}
		if v.PoolID < 1 {
			return SupplierFieldError("pool_id", "pool is required")
		}
	case *SupplierRoutingRule:
		if strings.TrimSpace(v.Model) == "" || len(v.Model) > 191 {
			return SupplierFieldError("model", "model is required and must fit the supported length")
		}
		if len(v.Group) > 191 {
			return SupplierFieldError("group", "group is too long")
		}
		if v.UserID < 0 {
			return SupplierFieldError("user_id", "customer ID cannot be negative")
		}
		if v.Mode != "capacity" && v.Mode != "share" && v.Mode != "failover" {
			return SupplierFieldError("mode", "unsupported routing mode")
		}
		if v.MaxAttempts < 1 || v.MaxAttempts > 10 {
			return SupplierFieldError("max_attempts", "attempts must be 1..10")
		}
		if v.TimeoutSeconds < 1 || v.TimeoutSeconds > 3600 {
			return SupplierFieldError("timeout_seconds", "timeout must be 1..3600 seconds")
		}
		if v.MaxSupplierPercent < 0 || v.MaxSupplierPercent > 100 {
			return SupplierFieldError("max_supplier_percent", "supplier concentration cap must be 0..100")
		}
		if len(v.Targets) < 1 || len(v.Targets) > 32 {
			return SupplierFieldError("targets", "rule requires 1..32 suppliers")
		}
		for i, target := range v.Targets {
			if target.SupplierID <= 0 {
				return SupplierFieldError(fmt.Sprintf("targets.%d.supplier_id", i), "supplier is required")
			}
			if target.Weight < 1 || target.Weight > 10000 {
				return SupplierFieldError(fmt.Sprintf("targets.%d.weight", i), "weight must be 1..10000")
			}
		}
		for _, bound := range []struct {
			name            string
			value, min, max int64
		}{
			{"window_seconds", v.Health.WindowSeconds, 10, 3600}, {"min_samples", v.Health.MinSamples, 1, 10000},
			{"failure_percent", v.Health.FailurePercent, 1, 100}, {"max_ttft_ms", v.Health.MaxTTFTMs, 1, 9007199254740991},
			{"cooldown_seconds", v.Health.CooldownSeconds, 1, 3600}, {"trial_percent", v.Health.TrialPercent, 1, 100},
		} {
			if bound.value < bound.min || bound.value > bound.max {
				return SupplierFieldError("health."+bound.name, "health threshold is outside the supported range")
			}
		}
	case *SupplierRoutingSettings:
		if v.CanaryPercent < 0 || v.CanaryPercent > 100 {
			return SupplierFieldError("canary_percent", "canary percent must be 0..100")
		}
	}
	return nil
}

func CreateSupplierRuntimeRevision(tx *gorm.DB, cfg *SupplierRoutingConfig, kind, id string, userID int) error {
	revision := RoutingRevision{CreatedBy: userID, CreatedAt: time.Now().Unix(), ResourceKind: kind, ResourceID: id}
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
	if err := WriteSupplierOption(tx, SupplierRoutingOptionKey, cfg); err != nil {
		return err
	}
	var state SupplierResourceState
	if err := ReadSupplierOption(tx, SupplierResourceStateKey, &state); err != nil {
		return err
	}
	state.TargetRevision = revision.ID
	return WriteSupplierOption(tx, SupplierResourceStateKey, state)
}

// Supplier fields in channels are a projection, not an alternative write surface.
func ProjectSupplierBindings(tx *gorm.DB, cfg *SupplierRoutingConfig) error {
	var channels []Channel
	if err := tx.Where("supplier_id <> 0").Find(&channels).Error; err != nil {
		return err
	}
	ids := map[int]bool{}
	for _, ch := range channels {
		ids[ch.Id] = true
	}
	for _, b := range cfg.Bindings {
		ids[b.ChannelID] = true
	}
	owners := map[int64]int64{}
	for _, p := range cfg.Pools {
		owners[p.ID] = p.SupplierID
	}
	for id := range ids {
		var ch Channel
		if err := tx.First(&ch, id).Error; err != nil {
			return err
		}
		settings := ch.GetOtherSettings()
		settings.SupplierPools = map[string]int64{}
		owner := int64(0)
		for _, b := range cfg.Bindings {
			if b.ChannelID == id {
				settings.SupplierPools[b.Model] = b.PoolID
				owner = owners[b.PoolID]
			}
		}
		data, err := common.Marshal(settings)
		if err != nil {
			return err
		}
		// Skip hooks: this is the authoritative, already-validated projection writer.
		if err := tx.Session(&gorm.Session{SkipHooks: true}).Model(&ch).Updates(map[string]any{"supplier_id": owner, "settings": string(data)}).Error; err != nil {
			return err
		}
	}
	return nil
}
