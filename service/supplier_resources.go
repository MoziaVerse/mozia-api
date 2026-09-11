package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/go-redis/redis/v8"
	"gorm.io/gorm"
)

type SupplierResourceMutation struct {
	Kind            string
	ID              string
	SupplierID      int64
	Action          string
	IfMatch         string
	IdempotencyKey  string
	Scope           string
	UserID          int
	Patch           json.RawMessage
	RestoreRevision int64
}

type SupplierResourceResult struct {
	Resource    any    `json:"resource"`
	Application string `json:"application"`
	Revision    int64  `json:"revision"`
	Replayed    bool   `json:"replayed"`
}

func SupplierResourceETag(kind string, resource any) string {
	id, version := model.SupplierResourceIdentity(resource)
	return fmt.Sprintf(`"%s-%s-v%d"`, kind, id, version)
}

const supplierPublishLock = "supplier-routing:publish-lock"

var supplierLeaseUnlock = redis.NewScript(`if redis.call('GET',KEYS[1])==ARGV[1] then return redis.call('DEL',KEYS[1]) end return 0`)
var supplierLeaseGate = redis.NewScript(`
if redis.call('GET',KEYS[1])~=ARGV[1] then return redis.error_reply('publication lease lost') end
local prior=redis.call('GET',KEYS[2]); local value={config={revision=0}}
if ARGV[2]=='1' and not prior then return redis.error_reply('capacity ledger disappeared during publication') end
if prior then value=cjson.decode(prior) end
value.blocked=true
redis.call('SET',KEYS[2],cjson.encode(value)); return 1`)
var supplierLeaseApply = redis.NewScript(`
if redis.call('GET',KEYS[1])~=ARGV[1] then return redis.error_reply('publication lease lost') end
local prior=redis.call('GET',KEYS[2]); local next=cjson.decode(ARGV[2])
if ARGV[3]=='1' and not prior then return redis.error_reply('capacity ledger disappeared during application') end
if prior then local old=cjson.decode(prior); if old.config and old.config.revision>next.config.revision then return redis.error_reply('newer revision already applied') end end
redis.call('SET',KEYS[2],ARGV[2]); return 1`)

func acquireSupplierPublisher(ctx context.Context) (string, error) {
	if !common.RedisEnabled || common.RDB == nil {
		return "", model.SupplierResourceConflict("runtime_unavailable", "shared Redis is required for runtime changes")
	}
	token := common.NewRequestId()
	ok, err := common.RDB.SetNX(ctx, supplierPublishLock, token, 30*time.Second).Result()
	if err != nil {
		return "", err
	}
	if !ok {
		return "", model.SupplierResourceConflict("publication_busy", "another publication is active; retry shortly")
	}
	return token, nil
}
func releaseSupplierPublisher(token string) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = supplierLeaseUnlock.Run(ctx, common.RDB, []string{supplierPublishLock}, token).Err()
}

// Never reconstruct missing capacity accounting while earlier calls might still run.
func checkSupplierLedger(ctx context.Context, db *gorm.DB) (int, error) {
	exists, err := common.RDB.Exists(ctx, supplierRuntimeKey).Result()
	if err != nil {
		return 0, err
	}
	if exists != 0 {
		return 1, nil
	}
	var pending int64
	if err := db.Model(&model.SupplierAttempt{}).Where("status IN ? OR (created_at >= ? AND kind <> ? AND status <> ?)", []string{"pending", "unknown"}, time.Now().Unix()-61, "shadow", "cancelled").Count(&pending).Error; err != nil {
		return 0, err
	}
	if pending > 0 {
		return 0, model.SupplierResourceConflict("capacity_reconciliation_required", "reconcile uncertain supplier calls and wait 61 seconds without supplier traffic before restoring Redis state")
	}
	return 0, nil
}

func MutateSupplierResource(parent context.Context, request SupplierResourceMutation) (*SupplierResourceResult, error) {
	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	defer cancel()
	var fields map[string]json.RawMessage
	if request.Action != "delete" && request.RestoreRevision == 0 {
		if err := common.Unmarshal(request.Patch, &fields); err != nil || fields == nil {
			return nil, model.SupplierFieldError("", "expected a JSON object")
		}
	}
	metadataOnly := request.Kind == "supplier" && request.Action == "patch"
	for key := range fields {
		if key != "name" && key != "contact" && key != "terms" {
			metadataOnly = false
		}
	}
	var creationKey, requestHash string
	if request.Action == "create" {
		if strings.TrimSpace(request.IdempotencyKey) == "" || len(request.IdempotencyKey) > 128 {
			return nil, &model.SupplierResourceError{Status: 400, Code: "idempotency_key_required", Message: "Idempotency-Key of 1..128 bytes is required"}
		}
		creationKey = fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%d:%s:%s", request.UserID, request.Scope, request.IdempotencyKey))))
		canonical, err := common.Marshal(fields)
		if err != nil {
			return nil, err
		}
		requestHash = fmt.Sprintf("%x", sha256.Sum256(canonical))
		replay, err := findSupplierCreation(model.DB.WithContext(ctx), request.Kind, creationKey, requestHash)
		if err != nil || replay != nil {
			return replay, err
		}
	} else if request.IfMatch == "" {
		return nil, &model.SupplierResourceError{Status: 428, Code: "precondition_required", Message: "If-Match is required; reload this record before saving"}
	}
	var token string
	if !metadataOnly {
		var err error
		token, err = acquireSupplierPublisher(ctx)
		if err != nil {
			return nil, err
		}
		defer releaseSupplierPublisher(token)
	}
	result := &SupplierResourceResult{Application: "not_required"}
	err := model.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := model.LockSupplierResources(tx); err != nil {
			return err
		}
		if request.Action == "create" {
			replay, err := findSupplierCreation(tx, request.Kind, creationKey, requestHash)
			if err != nil {
				return err
			}
			if replay != nil {
				*result = *replay
				return nil
			}
		}
		var state model.SupplierResourceState
		if err := model.ReadSupplierOption(tx, model.SupplierResourceStateKey, &state); err != nil {
			return err
		}
		if !metadataOnly && state.TargetRevision != state.AppliedRevision {
			return model.SupplierResourceConflict("configuration_apply_pending", "the saved configuration is awaiting application; retry after it is applied")
		}
		value := model.NewSupplierResource(request.Kind)
		if value == nil {
			return gorm.ErrRecordNotFound
		}
		if request.Action != "create" {
			var err error
			value, err = model.ReadSupplierResource(tx, request.Kind, request.ID)
			if err != nil {
				return err
			}
			if request.IfMatch != SupplierResourceETag(request.Kind, value) {
				return &model.SupplierResourceError{Status: 412, Code: "resource_version_conflict", Message: "this record changed; reload it before saving"}
			}
		}
		if request.RestoreRevision > 0 {
			var revision model.RoutingRevision
			if err := tx.First(&revision, request.RestoreRevision).Error; err != nil {
				return err
			}
			var cfg model.SupplierRoutingConfig
			if err := common.UnmarshalJsonStr(revision.ConfigJSON, &cfg); err != nil {
				return err
			}
			switch v := value.(type) {
			case *model.SupplierRoutingSettings:
				v.Enabled = cfg.Enabled
				v.Shadow = cfg.Shadow
				v.CanaryPercent = cfg.CanaryPercent
			case *model.SupplierRoutingRule:
				found := false
				for _, old := range cfg.Rules {
					if old.ID == v.ID {
						meta := v.SupplierResourceMeta
						*v = old
						v.SupplierResourceMeta = meta
						found = true
						break
					}
				}
				if !found {
					return model.SupplierResourceConflict("restore_target_missing", "the selected revision does not contain this rule")
				}
			default:
				return model.SupplierFieldError("revision", "only one rule or the global settings can be restored")
			}
		} else if request.Action != "delete" {
			if err := model.PatchSupplierResource(value, request.Patch); err != nil {
				return err
			}
		}
		if request.Action == "create" {
			meta := model.SupplierResourceMetadata(value)
			meta.Version = 1
			meta.CreationKey = &creationKey
			meta.RequestHash = requestHash
			switch v := value.(type) {
			case *model.SupplierPool:
				v.SupplierID = request.SupplierID
			case *model.SupplierRoutingRule:
				v.ID = "rule-" + common.NewRequestId()
			}
		} else if meta := model.SupplierResourceMetadata(value); meta != nil {
			meta.Version++
		} else {
			value.(*model.SupplierRoutingSettings).Version++
		}
		if request.Action == "delete" {
			// The compiled graph below rejects every remaining dependent pool/binding/rule.
			if b, ok := value.(*model.SupplierBinding); ok {
				if err := tx.Model(b).Update("active_key", nil).Error; err != nil {
					return err
				}
			}
			if err := tx.Delete(value).Error; err != nil {
				return err
			}
		} else {
			if err := model.ValidateSupplierResourceFields(value); err != nil {
				return err
			}
			if b, ok := value.(*model.SupplierBinding); ok {
				key := model.SupplierBindingKey(b)
				b.ActiveKey = &key
				var count int64
				if err := tx.Model(&model.SupplierBinding{}).Where("active_key = ? AND id <> ?", key, b.ID).Count(&count).Error; err != nil {
					return err
				}
				if count > 0 {
					return model.SupplierResourceConflict("binding_exists", "this channel/model already has a resource pool")
				}
			}
			if request.Kind == "settings" {
				if err := model.WriteSupplierOption(tx, model.SupplierRoutingSettingsKey, value); err != nil {
					return err
				}
			} else if request.Action == "create" {
				if err := tx.Create(value).Error; err != nil {
					return err
				}
			} else if err := tx.Save(value).Error; err != nil {
				return err
			}
		}
		result.Resource = value
		if metadataOnly {
			return nil
		}
		cfg, err := model.ReadSupplierResources(tx)
		if err != nil {
			return err
		}
		if err := model.ValidateSupplierRoutingConfigWithDB(tx, cfg); err != nil {
			if request.Action == "delete" {
				return model.SupplierResourceConflict("resource_in_use", err.Error())
			}
			return model.SupplierFieldError("", err.Error())
		}
		if request.Kind == "binding" {
			var pending int64
			if err := tx.Model(&model.SupplierAttempt{}).Where("status IN ?", []string{"pending", "unknown"}).Count(&pending).Error; err != nil {
				return err
			}
			if pending > 0 {
				return model.SupplierResourceConflict("binding_in_use", "reconcile pending/unknown attempts before changing resource bindings")
			}
			if err := model.ProjectSupplierBindings(tx, cfg); err != nil {
				return err
			}
		}
		ledgerExists, err := checkSupplierLedger(ctx, tx)
		if err != nil {
			return err
		}
		// Gate and final SET are both fenced. On uncertain commit, leave admission paused;
		// the durable application task recovers the SQL snapshot under a fresh lease.
		if err := supplierLeaseGate.Run(ctx, common.RDB, []string{supplierPublishLock, supplierRuntimeKey}, token, ledgerExists).Err(); err != nil {
			return err
		}
		id, _ := model.SupplierResourceIdentity(value)
		if err := model.CreateSupplierRuntimeRevision(tx, cfg, request.Kind, id, request.UserID); err != nil {
			return err
		}
		result.Revision = cfg.Revision
		result.Application = "pending"
		if request.Action != "delete" {
			if meta := model.SupplierResourceMetadata(value); meta != nil {
				meta.RuntimeRevision = cfg.Revision
				return tx.Model(value).UpdateColumn("runtime_revision", cfg.Revision).Error
			}
			settings := value.(*model.SupplierRoutingSettings)
			settings.RuntimeRevision = cfg.Revision
			return model.WriteSupplierOption(tx, model.SupplierRoutingSettingsKey, settings)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if metadataOnly || result.Replayed {
		return result, nil
	}
	if err := applySupplierRevision(ctx, token); err == nil {
		result.Application = "applied"
	} else {
		// Committed data remains a successful save even if Redis or the response fails.
		common.SysError("supplier configuration saved; application pending: " + err.Error())
		_, _, _ = EnqueueSystemTask("supplier_configuration_apply", struct{}{})
	}
	return result, nil
}

func findSupplierCreation(db *gorm.DB, kind, key, hash string) (*SupplierResourceResult, error) {
	value := model.NewSupplierResource(kind)
	if value == nil || kind == "settings" {
		return nil, model.SupplierFieldError("", "invalid resource kind")
	}
	err := db.Unscoped().Where("creation_key = ?", key).First(value).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	meta := model.SupplierResourceMetadata(value)
	if meta.RequestHash != hash {
		return nil, model.SupplierResourceConflict("idempotency_key_conflict", "this Idempotency-Key was used with different input")
	}
	if meta.DeletedAt.Valid {
		return nil, model.SupplierResourceConflict("resource_deleted", "the resource created by this request has been deleted")
	}
	application := "not_required"
	if meta.RuntimeRevision > 0 {
		var state model.SupplierResourceState
		if err := model.ReadSupplierOption(db, model.SupplierResourceStateKey, &state); err != nil {
			return nil, err
		}
		application = "pending"
		if state.AppliedRevision >= meta.RuntimeRevision {
			application = "applied"
		}
	}
	return &SupplierResourceResult{Resource: value, Replayed: true, Revision: meta.RuntimeRevision, Application: application}, nil
}

func applySupplierRevision(ctx context.Context, token string) error {
	err := model.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := model.LockSupplierResources(tx); err != nil {
			return err
		}
		var state model.SupplierResourceState
		if err := model.ReadSupplierOption(tx, model.SupplierResourceStateKey, &state); err != nil {
			return err
		}
		var cfg model.SupplierRoutingConfig
		err := model.ReadSupplierOption(tx, model.SupplierRoutingOptionKey, &cfg)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if cfg.Revision != state.TargetRevision {
			return errors.New("supplier snapshot does not match the target revision")
		}
		ledgerExists, err := checkSupplierLedger(ctx, tx)
		if err != nil {
			return err
		}
		data, err := common.Marshal(BuildSupplierRuntime(&cfg))
		if err != nil {
			return err
		}
		if err := supplierLeaseApply.Run(ctx, common.RDB, []string{supplierPublishLock, supplierRuntimeKey}, token, string(data), ledgerExists).Err(); err != nil {
			return err
		}
		if state.TargetRevision > 0 {
			if err := tx.Model(&model.RoutingRevision{}).Where("id = ? AND applied_at = 0", state.TargetRevision).Update("applied_at", time.Now().Unix()).Error; err != nil {
				return err
			}
		}
		state.AppliedRevision = state.TargetRevision
		return model.WriteSupplierOption(tx, model.SupplierResourceStateKey, state)
	})
	if err != nil {
		return err
	}
	cfg, err := model.ReadSupplierRoutingConfig()
	if err != nil {
		return err
	}
	data, err := common.Marshal(cfg)
	if err != nil {
		return err
	}
	common.OptionMapRWMutex.Lock()
	common.OptionMap[model.SupplierRoutingOptionKey] = string(data)
	common.OptionMapRWMutex.Unlock()
	model.InitChannelCache()
	return nil
}

func SupplierMutationHTTPStatus(result *SupplierResourceResult, action string) int {
	if result.Application == "pending" {
		return http.StatusAccepted
	}
	if action == "create" && !result.Replayed {
		return http.StatusCreated
	}
	return http.StatusOK
}

type supplierConfigurationApplyTask struct{}

func (supplierConfigurationApplyTask) Type() string            { return "supplier_configuration_apply" }
func (supplierConfigurationApplyTask) Interval() time.Duration { return 30 * time.Second }
func (supplierConfigurationApplyTask) NewPayload() any         { return struct{}{} }
func (supplierConfigurationApplyTask) Enabled() bool           { return common.RedisEnabled && common.RDB != nil }
func (supplierConfigurationApplyTask) Run(parent context.Context, task *model.SystemTask, runnerID string) {
	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	defer cancel()
	err := ReconcileSupplierConfiguration(ctx)
	status, message := model.SystemTaskStatusSucceeded, ""
	if err != nil {
		status = model.SystemTaskStatusFailed
		message = err.Error()
	}
	if err := model.FinishSystemTask(task.TaskID, runnerID, status, nil, message); err != nil {
		common.SysError("supplier application task: " + err.Error())
	}
}
func ReconcileSupplierConfiguration(ctx context.Context) error {
	var state model.SupplierResourceState
	if err := model.ReadSupplierOption(model.DB.WithContext(ctx), model.SupplierResourceStateKey, &state); err != nil {
		return err
	}
	data, err := common.RDB.Get(ctx, supplierRuntimeKey).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return err
	}
	if err == nil {
		var runtime SupplierRuntime
		if err := common.UnmarshalJsonStr(data, &runtime); err != nil {
			return err
		}
		if !runtime.Blocked && runtime.Config.Revision == state.TargetRevision && state.AppliedRevision == state.TargetRevision {
			return nil
		}
	} else if state.TargetRevision == 0 {
		return nil
	}
	token, err := acquireSupplierPublisher(ctx)
	if err != nil {
		return err
	}
	defer releaseSupplierPublisher(token)
	return applySupplierRevision(ctx, token)
}
func init() { RegisterSystemTaskHandler(supplierConfigurationApplyTask{}) }
