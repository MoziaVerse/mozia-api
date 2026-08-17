package model

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	ResellerIdentityProviderHduCAS = "hdu_cas"

	ResellerIdentityRouteStatusActive   = "active"
	ResellerIdentityRouteStatusInactive = "inactive"

	ResellerVerifiedIdentityClaimAssigned        = "assigned"
	ResellerVerifiedIdentityClaimAlreadyInTarget = "already_in_target"
	ResellerVerifiedIdentityClaimOwnedByOther    = "owned_by_other_reseller"
	ResellerVerifiedIdentityClaimRouteInactive   = "route_inactive"

	ResellerAssignmentConflictPending     = "pending"
	ResellerAssignmentConflictKeptCurrent = "kept_current"
	ResellerAssignmentConflictTransferred = "transferred"

	ResellerAssignmentConflictActionKeepCurrent = "keep_current"
	ResellerAssignmentConflictActionTransfer    = "transfer"

	ResellerCustomerAssignmentSourceHduCAS           = "hdu_cas"
	ResellerCustomerAssignmentSourcePlatformTransfer = "platform_transfer"
)

var (
	ErrInvalidResellerIdentityProvider    = errors.New("invalid reseller identity provider")
	ErrInvalidResellerIdentityRoute       = errors.New("invalid reseller identity route")
	ErrResellerAssignmentConflictNotFound = errors.New("reseller assignment conflict not found")
)

type ResellerIdentityRoute struct {
	Id          int    `json:"id" gorm:"primaryKey;autoIncrement"`
	ProviderKey string `json:"provider_key" gorm:"type:varchar(64);not null;uniqueIndex"`
	ResellerId  int    `json:"reseller_id" gorm:"not null;index"`
	Status      string `json:"status" gorm:"type:varchar(16);not null;index"`
	CreatedAt   int64  `json:"created_at" gorm:"autoCreateTime;column:created_at"`
	UpdatedAt   int64  `json:"updated_at" gorm:"autoUpdateTime;column:updated_at"`
}

type ResellerAssignmentConflict struct {
	Id                int    `json:"id" gorm:"primaryKey;autoIncrement"`
	ProviderKey       string `json:"provider_key" gorm:"type:varchar(64);not null;uniqueIndex:uq_reseller_assignment_conflicts_provider_subject,priority:1"`
	Subject           string `json:"subject" gorm:"type:varchar(255);not null;uniqueIndex:uq_reseller_assignment_conflicts_provider_subject,priority:2;index"`
	TargetResellerId  int    `json:"target_reseller_id" gorm:"not null;index"`
	CurrentResellerId int    `json:"current_reseller_id" gorm:"not null;index"`
	Status            string `json:"status" gorm:"type:varchar(24);not null;index"`
	FirstSeenAt       int64  `json:"first_seen_at" gorm:"not null"`
	LastSeenAt        int64  `json:"last_seen_at" gorm:"not null"`
	ResolvedAt        int64  `json:"resolved_at" gorm:"not null;default:0"`
	ResolvedBySubject string `json:"resolved_by_subject" gorm:"type:varchar(255);not null;default:''"`
}

type ResellerVerifiedIdentityClaimInput struct {
	ProviderKey string `json:"provider_key"`
	Subject     string `json:"subject"`
	VerifiedAt  int64  `json:"verified_at"`
	MatrixName  string `json:"matrix_name"`
	Phone       string `json:"phone"`
}

type ResellerVerifiedIdentityClaimRecord struct {
	Status     string `json:"status"`
	ResellerId *int   `json:"reseller_id,omitempty"`
	CustomerId *int   `json:"customer_id,omitempty"`
	ConflictId *int   `json:"conflict_id,omitempty"`
}

func validResellerIdentityProvider(providerKey string) bool {
	return providerKey == ResellerIdentityProviderHduCAS
}

func UpsertResellerIdentityRoute(providerKey string, resellerId int, status string) (*ResellerIdentityRoute, error) {
	if !validResellerIdentityProvider(providerKey) || resellerId < 1 || (status != ResellerIdentityRouteStatusActive && status != ResellerIdentityRouteStatusInactive) {
		return nil, ErrInvalidResellerIdentityRoute
	}
	var reseller Reseller
	if err := DB.Select("id", "status").Where("id = ?", resellerId).Take(&reseller).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrResellerNotFound
		}
		return nil, err
	}
	if status == ResellerIdentityRouteStatusActive && reseller.Status != ResellerStatusActive {
		return nil, ErrResellerNotFound
	}
	route := ResellerIdentityRoute{ProviderKey: providerKey, ResellerId: resellerId, Status: status}
	if err := DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "provider_key"}},
		DoUpdates: clause.AssignmentColumns([]string{"reseller_id", "status", "updated_at"}),
	}).Create(&route).Error; err != nil {
		return nil, err
	}
	if err := DB.Where("provider_key = ?", providerKey).Take(&route).Error; err != nil {
		return nil, err
	}
	return &route, nil
}

func GetResellerIdentityRoute(providerKey string) (*ResellerIdentityRoute, error) {
	if !validResellerIdentityProvider(providerKey) {
		return nil, ErrInvalidResellerIdentityProvider
	}
	var route ResellerIdentityRoute
	if err := DB.Where("provider_key = ?", providerKey).Take(&route).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &route, nil
}

func ClaimResellerVerifiedIdentity(input ResellerVerifiedIdentityClaimInput) (*ResellerVerifiedIdentityClaimRecord, error) {
	input.MatrixName = strings.TrimSpace(input.MatrixName)
	input.Phone = strings.TrimSpace(input.Phone)
	if !validResellerIdentityProvider(input.ProviderKey) || !ValidResellerSubject(input.Subject) || input.VerifiedAt < 1 || !validResellerCustomerText(input.MatrixName, 255) || !validResellerCustomerText(input.Phone, 50) {
		return nil, ErrInvalidResellerCustomerIdentity
	}

	var record *ResellerVerifiedIdentityClaimRecord
	err := DB.Transaction(func(tx *gorm.DB) error {
		var route ResellerIdentityRoute
		if err := tx.Where("provider_key = ? AND status = ?", input.ProviderKey, ResellerIdentityRouteStatusActive).Take(&route).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				record = &ResellerVerifiedIdentityClaimRecord{Status: ResellerVerifiedIdentityClaimRouteInactive}
				return nil
			}
			return err
		}
		var reseller Reseller
		if err := tx.Select("id", "status").Where("id = ?", route.ResellerId).Take(&reseller).Error; err != nil || reseller.Status != ResellerStatusActive {
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			record = &ResellerVerifiedIdentityClaimRecord{Status: ResellerVerifiedIdentityClaimRouteInactive}
			return nil
		}

		now := common.GetTimestamp()
		customer := ResellerCustomer{
			ResellerId:       route.ResellerId,
			Subject:          input.Subject,
			MatrixName:       input.MatrixName,
			Phone:            input.Phone,
			ProfileSyncedAt:  now,
			AssignmentSource: input.ProviderKey,
			Status:           ResellerCustomerStatusActive,
		}
		created := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&customer)
		if created.Error != nil {
			return created.Error
		}
		if created.RowsAffected == 1 {
			record = &ResellerVerifiedIdentityClaimRecord{Status: ResellerVerifiedIdentityClaimAssigned, ResellerId: &route.ResellerId, CustomerId: &customer.Id}
			return nil
		}

		var existing ResellerCustomer
		if err := tx.Select("id", "reseller_id").Where("subject = ?", input.Subject).Take(&existing).Error; err != nil {
			return err
		}
		if existing.ResellerId == route.ResellerId {
			record = &ResellerVerifiedIdentityClaimRecord{Status: ResellerVerifiedIdentityClaimAlreadyInTarget, ResellerId: &route.ResellerId, CustomerId: &existing.Id}
			return nil
		}

		conflict := ResellerAssignmentConflict{
			ProviderKey: input.ProviderKey, Subject: input.Subject, TargetResellerId: route.ResellerId,
			CurrentResellerId: existing.ResellerId, Status: ResellerAssignmentConflictPending,
			FirstSeenAt: now, LastSeenAt: now,
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "provider_key"}, {Name: "subject"}},
			DoUpdates: clause.Assignments(map[string]any{
				"target_reseller_id":  route.ResellerId,
				"current_reseller_id": existing.ResellerId,
				"last_seen_at":        now,
			}),
		}).Create(&conflict).Error; err != nil {
			return err
		}
		if err := tx.Where("provider_key = ? AND subject = ?", input.ProviderKey, input.Subject).Take(&conflict).Error; err != nil {
			return err
		}
		record = &ResellerVerifiedIdentityClaimRecord{Status: ResellerVerifiedIdentityClaimOwnedByOther, ResellerId: &route.ResellerId, CustomerId: &existing.Id, ConflictId: &conflict.Id}
		return nil
	})
	return record, err
}

func ListResellerAssignmentConflicts(status string) ([]ResellerAssignmentConflict, error) {
	query := DB.Order("last_seen_at DESC, id DESC")
	if status != "" {
		if status != ResellerAssignmentConflictPending && status != ResellerAssignmentConflictKeptCurrent && status != ResellerAssignmentConflictTransferred {
			return nil, ErrInvalidResellerIdentityRoute
		}
		query = query.Where("status = ?", status)
	}
	records := make([]ResellerAssignmentConflict, 0)
	return records, query.Find(&records).Error
}

func ResolveResellerAssignmentConflict(id int, action string, actorSubject string) (*ResellerAssignmentConflict, error) {
	if id < 1 || !ValidResellerSubject(actorSubject) || (action != ResellerAssignmentConflictActionKeepCurrent && action != ResellerAssignmentConflictActionTransfer) {
		return nil, ErrInvalidResellerIdentityRoute
	}
	var result ResellerAssignmentConflict
	err := DB.Transaction(func(tx *gorm.DB) error {
		var conflict ResellerAssignmentConflict
		if err := tx.Where("id = ? AND status = ?", id, ResellerAssignmentConflictPending).Take(&conflict).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrResellerAssignmentConflictNotFound
			}
			return err
		}
		now := common.GetTimestamp()
		status := ResellerAssignmentConflictKeptCurrent
		if action == ResellerAssignmentConflictActionTransfer {
			var target Reseller
			if err := tx.Select("id", "status").Where("id = ?", conflict.TargetResellerId).Take(&target).Error; err != nil || target.Status != ResellerStatusActive {
				if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
					return err
				}
				return ErrResellerNotFound
			}
			update := tx.Model(&ResellerCustomer{}).
				Where("subject = ? AND reseller_id = ?", conflict.Subject, conflict.CurrentResellerId).
				Updates(map[string]any{"reseller_id": conflict.TargetResellerId, "assignment_source": ResellerCustomerAssignmentSourcePlatformTransfer})
			if update.Error != nil {
				return update.Error
			}
			if update.RowsAffected == 0 {
				return ErrResellerCustomerConflict
			}
			status = ResellerAssignmentConflictTransferred
		}
		if err := tx.Model(&conflict).Updates(map[string]any{
			"status": status, "resolved_at": now, "resolved_by_subject": actorSubject,
		}).Error; err != nil {
			return err
		}
		conflict.Status = status
		conflict.ResolvedAt = now
		conflict.ResolvedBySubject = actorSubject
		result = conflict
		return nil
	})
	return &result, err
}
