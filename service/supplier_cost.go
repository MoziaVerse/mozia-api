package service

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/go-redis/redis/v8"
	"gorm.io/gorm"
)

func SupplierExpectedOutput(ctx context.Context, scope string, fallback int64) (int64, error) {
	now, err := common.RDB.Time(ctx).Result()
	if err != nil {
		return 0, err
	}
	pipe := common.RDB.Pipeline()
	minute := now.Unix() / 60
	for i := int64(0); i < 5; i++ {
		pipe.HGetAll(ctx, fmt.Sprintf("%s:%d", scope, minute-i))
	}
	commands, err := pipe.Exec(ctx)
	if err != nil {
		return 0, err
	}
	var tokens, samples int64
	for _, command := range commands {
		values := command.(*redis.StringStringMapCmd).Val()
		n, _ := strconv.ParseInt(values["output"], 10, 64)
		tokens += n
		n, _ = strconv.ParseInt(values["count"], 10, 64)
		samples += n
	}
	if samples == 0 {
		return fallback, nil
	}
	return max(1, tokens/samples), nil
}

// Price edits use the same publication fence as resource edits. The unchanged
// pricing API remains usable without Redis when there are no adaptive rules.
func MutateSupplierCost(parent context.Context, cost *model.ChannelCostPricing, deleteID int64, userID int) (*SupplierResourceResult, error) {
	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	defer cancel()
	managed := model.DB.Migrator().HasTable(&model.SupplierRoutingRule{})
	var adaptive int64
	if managed {
		if err := model.DB.Model(&model.SupplierRoutingRule{}).Where("mode = ?", "adaptive").Count(&adaptive).Error; err != nil {
			return nil, err
		}
	}
	token := ""
	if adaptive > 0 {
		var err error
		token, err = acquireSupplierPublisher(ctx)
		if err != nil {
			return nil, err
		}
		defer releaseSupplierPublisher(token)
	}
	result := &SupplierResourceResult{Application: "not_required"}
	err := model.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if managed {
			if err := model.LockSupplierResources(tx); err != nil {
				return err
			}
			if err := tx.Model(&model.SupplierRoutingRule{}).Where("mode = ?", "adaptive").Count(&adaptive).Error; err != nil {
				return err
			}
			if adaptive > 0 && token == "" {
				return model.SupplierResourceConflict("configuration_changed", "adaptive routing was enabled; retry this price edit")
			}
		}
		if adaptive > 0 {
			var state model.SupplierResourceState
			if err := model.ReadSupplierOption(tx, model.SupplierResourceStateKey, &state); err != nil {
				return err
			}
			if state.TargetRevision != state.AppliedRevision {
				return model.SupplierResourceConflict("configuration_apply_pending", "wait for the current publication before changing prices")
			}
		}
		id := fmt.Sprint(deleteID)
		if cost != nil {
			if err := model.UpsertChannelCostPricing(tx, cost); err != nil {
				return err
			}
			result.Resource = cost
			id = fmt.Sprint(cost.Id)
		} else {
			deleted := tx.Delete(&model.ChannelCostPricing{}, deleteID)
			if deleted.Error != nil {
				return deleted.Error
			}
			result.Resource = deleted.RowsAffected > 0
		}
		if adaptive == 0 {
			return nil
		}
		cfg, err := model.ReadSupplierResources(tx)
		if err != nil {
			return err
		}
		if err := model.ValidateSupplierRoutingConfigWithDB(tx, cfg); err != nil {
			return err
		}
		exists, err := checkSupplierLedger(ctx, tx)
		if err != nil {
			return err
		}
		if err := supplierLeaseGate.Run(ctx, common.RDB, []string{supplierPublishLock, supplierRuntimeKey}, token, exists).Err(); err != nil {
			return err
		}
		if err := model.CreateSupplierRuntimeRevision(tx, cfg, "price", id, userID); err != nil {
			return err
		}
		result.Revision, result.Application = cfg.Revision, "pending"
		return nil
	})
	if err != nil {
		return nil, err
	}
	if result.Application == "pending" {
		if err := applySupplierRevision(ctx, token); err == nil {
			result.Application = "applied"
		} else {
			common.SysError("supplier price saved; application pending: " + err.Error())
			_, _, _ = EnqueueSystemTask("supplier_configuration_apply", struct{}{})
		}
	}
	return result, nil
}
