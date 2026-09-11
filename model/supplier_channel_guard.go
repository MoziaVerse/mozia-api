package model

import (
	"context"
	"maps"
	"reflect"
	"slices"

	"github.com/QuantumNous/new-api/constant"
	"gorm.io/gorm"
)

// Install on this DB only after the resource migration, including on slave nodes.
func RegisterSupplierChannelGuards(db *gorm.DB) error {
	if db.Callback().Update().Get("supplier:channel_update") != nil {
		return nil
	}
	if err := db.Callback().Create().Before("gorm:create").Register("supplier:channel_create", func(tx *gorm.DB) {
		if tx.Statement.Schema == nil || tx.Statement.Schema.Name != "Channel" || tx.Statement.SkipHooks {
			return
		}
		value := tx.Statement.ReflectValue
		if value.Kind() != reflect.Slice && value.Kind() != reflect.Array {
			guardSupplierChannelCreate(tx, value)
			return
		}
		for i := 0; i < value.Len(); i++ {
			guardSupplierChannelCreate(tx, value.Index(i))
		}
	}); err != nil {
		return err
	}
	if err := db.Callback().Delete().Before("gorm:delete").Register("supplier:channel_delete", func(tx *gorm.DB) {
		if tx.Statement.Schema == nil || tx.Statement.Schema.Name != "Channel" || tx.Statement.SkipHooks {
			return
		}
		if err := LockSupplierResources(tx.Session(&gorm.Session{NewDB: true})); err != nil {
			tx.AddError(err)
			return
		}
		channels, err := supplierChannelMutationTargets(tx)
		if err != nil {
			tx.AddError(err)
			return
		}
		for _, ch := range channels {
			if ch.SupplierID != 0 {
				tx.AddError(SupplierResourceConflict("resource_in_use", "remove this channel's supplier bindings before deleting it"))
				return
			}
		}
	}); err != nil {
		return err
	}
	return db.Callback().Update().Before("gorm:update").Register("supplier:channel_update", func(tx *gorm.DB) {
		if tx.Statement.Schema == nil || tx.Statement.Schema.Name != "Channel" || tx.Statement.SkipHooks {
			return
		}
		columns := map[string]any{}
		selected, restricted := tx.Statement.SelectAndOmitColumns(false, true)
		if values, ok := tx.Statement.Dest.(map[string]interface{}); ok {
			for key, value := range values {
				field := tx.Statement.Schema.LookUpField(key)
				if field == nil {
					continue
				}
				if allowed, ok := selected[field.DBName]; (ok && !allowed) || (!ok && restricted) {
					continue
				}
				columns[field.DBName] = value
			}
		} else {
			value := reflect.Indirect(reflect.ValueOf(tx.Statement.Dest))
			if value.Kind() != reflect.Struct {
				return
			}
			for _, name := range []string{"models", "type", "model_mapping", "supplier_id", "settings"} {
				field := tx.Statement.Schema.LookUpField(name)
				v, zero := field.ValueOf(context.Background(), value)
				allowed, exists := selected[name]
				if exists && !allowed {
					continue
				}
				if !exists && (restricted || zero) {
					continue
				}
				columns[name] = v
			}
		}
		relevant := false
		for _, key := range []string{"models", "type", "model_mapping", "supplier_id", "settings"} {
			if _, ok := columns[key]; ok {
				relevant = true
			}
		}
		if !relevant {
			return
		}
		if err := LockSupplierResources(tx.Session(&gorm.Session{NewDB: true})); err != nil {
			tx.AddError(err)
			return
		}
		channels, err := supplierChannelMutationTargets(tx)
		if err != nil {
			tx.AddError(err)
			return
		}
		for _, prior := range channels {
			if id, ok := columns["supplier_id"]; ok && !reflect.DeepEqual(id, prior.SupplierID) {
				tx.AddError(SupplierResourceConflict("derived_supplier_field", "supplier ownership is managed through supplier bindings"))
				return
			}
			if settings, ok := columns["settings"]; ok {
				raw, ok := settings.(string)
				if !ok {
					tx.AddError(SupplierFieldError("settings", "invalid channel settings"))
					return
				}
				next := Channel{OtherSettings: raw}
				if !maps.Equal(next.GetOtherSettings().SupplierPools, prior.GetOtherSettings().SupplierPools) {
					tx.AddError(SupplierResourceConflict("derived_supplier_field", "resource pool mappings are managed through supplier bindings"))
					return
				}
			}
			if prior.SupplierID == 0 {
				continue
			}
			if channelType, ok := columns["type"]; ok && channelType != constant.ChannelTypeOpenAI {
				tx.AddError(SupplierResourceConflict("resource_in_use", "unbind the supplier before changing the channel protocol"))
				return
			}
			if mapping, ok := columns["model_mapping"]; ok {
				next := Channel{}
				switch v := mapping.(type) {
				case *string:
					next.ModelMapping = v
				case string:
					next.ModelMapping = &v
				}
				if next.GetModelMapping() != prior.GetModelMapping() {
					tx.AddError(SupplierResourceConflict("resource_in_use", "unbind the supplier before changing the accepted model mapping"))
					return
				}
			}
			if models, ok := columns["models"].(string); ok {
				next := Channel{Models: models}
				for name := range prior.GetOtherSettings().SupplierPools {
					if !slices.Contains(next.GetModels(), name) {
						tx.AddError(SupplierResourceConflict("resource_in_use", "remove the supplier binding before removing its channel model"))
						return
					}
				}
			}
		}
	})
}

func guardSupplierChannelCreate(tx *gorm.DB, value reflect.Value) {
	value = reflect.Indirect(value)
	ch, ok := value.Interface().(Channel)
	if !ok {
		return
	}
	if ch.SupplierID != 0 || len(ch.GetOtherSettings().SupplierPools) > 0 {
		tx.AddError(SupplierResourceConflict("derived_supplier_field", "create the channel first, then add its supplier bindings"))
	}
}
func supplierChannelMutationTargets(tx *gorm.DB) ([]Channel, error) {
	query := tx.Session(&gorm.Session{NewDB: true}).Model(&Channel{})
	if where, ok := tx.Statement.Clauses["WHERE"]; ok {
		query = query.Clauses(where.Expression)
	}
	value := reflect.Indirect(reflect.ValueOf(tx.Statement.Model))
	if value.IsValid() && value.Kind() == reflect.Struct {
		if field := tx.Statement.Schema.PrioritizedPrimaryField; field != nil {
			if id, zero := field.ValueOf(context.Background(), value); !zero {
				query = query.Where("id = ?", id)
			}
		}
	}
	var channels []Channel
	err := query.Select("id", "supplier_id", "models", "type", "model_mapping", "settings").Find(&channels).Error
	return channels, err
}
