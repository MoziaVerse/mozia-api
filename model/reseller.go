package model

import (
	crand "crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"gorm.io/gorm"
)

const (
	ResellerStatusActive          = "active"
	ResellerStatusSuspended       = "suspended"
	ResellerDomainStatusActive    = "active"
	ResellerMemberStatusActive    = "active"
	ResellerCustomerStatusActive  = "active"
	ResellerCustomerStatusSuspend = "suspended"

	ResellerRoleOwner  = "owner"
	ResellerRoleAdmin  = "admin"
	ResellerRoleViewer = "viewer"

	ResellerInvitationStatusPending  = "pending"
	ResellerInvitationStatusExpired  = "expired"
	ResellerInvitationStatusRevoked  = "revoked"
	ResellerInvitationStatusConsumed = "consumed"

	ResellerCustomerBatchAssignStatusAssigned             = "assigned"
	ResellerCustomerBatchAssignStatusAlreadyInTarget      = "already_in_target"
	ResellerCustomerBatchAssignStatusOwnedByOtherReseller = "owned_by_other_reseller"
)

var (
	ErrInvalidResellerHost             = errors.New("invalid reseller host")
	ErrInvalidResellerName             = errors.New("invalid reseller name")
	ErrInvalidResellerOwnerSubject     = errors.New("invalid reseller owner subject")
	ErrInvalidResellerStatus           = errors.New("invalid reseller status")
	ErrInvalidResellerCustomerStatus   = errors.New("invalid reseller customer status")
	ErrInvalidResellerCustomerIdentity = errors.New("invalid reseller customer identity")
	ErrInvalidResellerCustomerRemark   = errors.New("invalid reseller customer remark")
	ErrInvalidResellerInvitation       = errors.New("invalid reseller invitation")
	ErrInvalidResellerSubject          = errors.New("invalid reseller subject")
	ErrResellerConflict                = errors.New("reseller conflict")
	ErrResellerNotFound                = errors.New("reseller not found")
	ErrResellerForbidden               = errors.New("reseller forbidden")
	ErrResellerInvitationNotFound      = errors.New("reseller invitation not found")
	ErrResellerInvitationExpired       = errors.New("reseller invitation expired")
	ErrResellerInvitationRevoked       = errors.New("reseller invitation revoked")
	ErrResellerInvitationConsumed      = errors.New("reseller invitation consumed")
	ErrResellerCustomerNotFound        = errors.New("reseller customer not found")
	ErrResellerCustomerConflict        = errors.New("reseller customer conflict")
)

type Reseller struct {
	Id     int    `json:"id" gorm:"primaryKey;autoIncrement"`
	Name   string `json:"name" gorm:"type:varchar(128);not null"`
	Status string `json:"status" gorm:"type:varchar(16);not null;index"`
}

type ResellerDomain struct {
	Id         int    `json:"id" gorm:"primaryKey;autoIncrement"`
	ResellerId int    `json:"reseller_id" gorm:"not null;index"`
	Host       string `json:"host" gorm:"type:varchar(260);not null;uniqueIndex"`
	Verified   bool   `json:"verified" gorm:"not null"`
	Status     string `json:"status" gorm:"type:varchar(16);not null"`
}

type ResellerMember struct {
	Id         int    `json:"id" gorm:"primaryKey;autoIncrement"`
	ResellerId int    `json:"reseller_id" gorm:"not null;uniqueIndex:uq_reseller_members_reseller_subject,priority:1"`
	Subject    string `json:"subject" gorm:"type:varchar(255);not null;index;uniqueIndex:uq_reseller_members_reseller_subject,priority:2"`
	Role       string `json:"role" gorm:"type:varchar(16);not null"`
	Status     string `json:"status" gorm:"type:varchar(16);not null"`
}

type ResellerCustomer struct {
	Id              int    `json:"id" gorm:"primaryKey;autoIncrement"`
	ResellerId      int    `json:"reseller_id" gorm:"not null;index"`
	Subject         string `json:"subject" gorm:"type:varchar(255);not null;uniqueIndex"`
	MatrixName      string `json:"matrix_name" gorm:"type:varchar(255);not null;default:''"`
	Phone           string `json:"phone" gorm:"type:varchar(50);not null;default:''"`
	ProfileSyncedAt int64  `json:"profile_synced_at" gorm:"not null;default:0;index"`
	Remark          string `json:"-" gorm:"type:varchar(255);not null;default:''"`
	Status          string `json:"status" gorm:"type:varchar(16);not null;index"`
	CreatedAt       int64  `json:"created_at" gorm:"autoCreateTime;column:created_at"`
	UpdatedAt       int64  `json:"updated_at" gorm:"autoUpdateTime;column:updated_at"`
}

type ResellerInvitation struct {
	Id                int     `json:"id" gorm:"primaryKey;autoIncrement"`
	ResellerId        int     `json:"reseller_id" gorm:"not null;index"`
	TokenHash         string  `json:"token_hash" gorm:"type:varchar(64);not null;uniqueIndex"`
	CreatedBySubject  string  `json:"created_by_subject" gorm:"type:varchar(255);not null"`
	ExpiresAt         int64   `json:"expires_at" gorm:"not null;index"`
	RevokedAt         *int64  `json:"revoked_at" gorm:"default:null"`
	ConsumedAt        *int64  `json:"consumed_at" gorm:"default:null"`
	ConsumedBySubject *string `json:"consumed_by_subject" gorm:"type:varchar(255);default:null"`
	CreatedAt         int64   `json:"created_at" gorm:"autoCreateTime;column:created_at"`
}

type ResellerContext struct {
	ResellerId   int    `json:"reseller_id"`
	ResellerName string `json:"reseller_name"`
	Subject      string `json:"subject"`
	Host         string `json:"host"`
	Role         string `json:"role"`
}

type ResellerAdminRecord struct {
	Id                    int     `json:"id"`
	Name                  string  `json:"name"`
	Status                string  `json:"status"`
	Host                  string  `json:"host"`
	OwnerSubject          string  `json:"owner_subject"`
	OwnerUserId           int     `json:"owner_user_id"`
	OwnerUsername         string  `json:"owner_username"`
	OwnerDisplayName      string  `json:"owner_display_name"`
	OwnerBalanceQuota     int     `json:"-"`
	OwnerGiftBalanceQuota int     `json:"-"`
	OwnerPaidBalanceQuota int     `json:"-"`
	OwnerBalance          float64 `json:"owner_balance" gorm:"-"`
	OwnerGiftBalance      float64 `json:"owner_gift_balance" gorm:"-"`
	OwnerPaidBalance      float64 `json:"owner_paid_balance" gorm:"-"`
	OwnerRequestCount     int     `json:"owner_request_count"`
	BalanceDisplayType    string  `json:"balance_display_type" gorm:"-"`
	BalanceCurrencySymbol string  `json:"balance_currency_symbol" gorm:"-"`
	MemberCount           int     `json:"member_count"`
}

type ResellerMemberRecord struct {
	Id      int    `json:"id"`
	Subject string `json:"subject"`
	Role    string `json:"role"`
	Status  string `json:"status"`
}

type ResellerCustomerRecord struct {
	Id                    int     `json:"id"`
	Subject               string  `json:"subject"`
	Status                string  `json:"status"`
	JoinedAt              int64   `json:"joined_at"`
	UserId                int     `json:"user_id"`
	Username              string  `json:"username"`
	DisplayName           string  `json:"display_name"`
	MatrixName            string  `json:"matrix_name"`
	Phone                 string  `json:"phone"`
	ProfileSyncedAt       int64   `json:"profile_synced_at"`
	Remark                *string `json:"remark,omitempty"`
	BalanceQuota          int     `json:"-"`
	GiftBalanceQuota      int     `json:"-"`
	PaidBalanceQuota      int     `json:"-"`
	Balance               float64 `json:"balance" gorm:"-"`
	GiftBalance           float64 `json:"gift_balance" gorm:"-"`
	PaidBalance           float64 `json:"paid_balance" gorm:"-"`
	RequestCount          int     `json:"request_count"`
	BalanceDisplayType    string  `json:"balance_display_type" gorm:"-"`
	BalanceCurrencySymbol string  `json:"balance_currency_symbol" gorm:"-"`
}

type ResellerCustomerProfileBackfillCandidate struct {
	Id      int    `json:"id"`
	Subject string `json:"subject"`
}

type ResellerCustomerProfileBackfillPage struct {
	Items       []ResellerCustomerProfileBackfillCandidate `json:"items"`
	NextAfterId int                                        `json:"next_after_id"`
}

type ResellerInvitationRecord struct {
	Id                int     `json:"id"`
	CreatedBySubject  string  `json:"created_by_subject"`
	ExpiresAt         int64   `json:"expires_at"`
	RevokedAt         *int64  `json:"revoked_at"`
	ConsumedAt        *int64  `json:"consumed_at"`
	ConsumedBySubject *string `json:"consumed_by_subject"`
	CreatedAt         int64   `json:"created_at"`
	Status            string  `json:"status"`
}

type ResellerInvitationCreateRecord struct {
	Invitation ResellerInvitationRecord `json:"invitation"`
	Token      string                   `json:"token"`
}

type ResellerInvitationConsumeRecord struct {
	Customer     ResellerCustomerRecord `json:"customer"`
	ResellerId   int                    `json:"reseller_id"`
	ResellerName string                 `json:"reseller_name"`
}

type ResellerCustomerBatchAssignInput struct {
	Subject    string `json:"subject"`
	MatrixName string `json:"matrix_name"`
	Phone      string `json:"phone"`
}

type ResellerCustomerBatchAssignResult struct {
	Subject           string `json:"subject"`
	Status            string `json:"status"`
	CustomerId        *int   `json:"customer_id,omitempty"`
	CurrentResellerId *int   `json:"current_reseller_id,omitempty"`
}

type ResellerCustomerBatchAssignRecord struct {
	ResellerId int                                 `json:"reseller_id"`
	Results    []ResellerCustomerBatchAssignResult `json:"results"`
}

func NormalizeResellerHost(raw string) (string, error) {
	if raw == "" || len(raw) > 260 || strings.TrimSpace(raw) != raw {
		return "", ErrInvalidResellerHost
	}

	host := raw
	port := 0
	if strings.Contains(host, ":") {
		if strings.Count(host, ":") != 1 {
			return "", ErrInvalidResellerHost
		}
		separator := strings.LastIndexByte(host, ':')
		parsedPort, err := strconv.Atoi(host[separator+1:])
		if err != nil || parsedPort < 1 || parsedPort > 65535 {
			return "", ErrInvalidResellerHost
		}
		port = parsedPort
		host = host[:separator]
	}

	for i := 0; i < len(host); i++ {
		character := host[i]
		if (character < 'a' || character > 'z') &&
			(character < 'A' || character > 'Z') &&
			(character < '0' || character > '9') &&
			character != '-' && character != '.' {
			return "", ErrInvalidResellerHost
		}
	}
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if host == "" || len(host) > 253 {
		return "", ErrInvalidResellerHost
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", ErrInvalidResellerHost
		}
		for i := 0; i < len(label); i++ {
			character := label[i]
			if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' {
				return "", ErrInvalidResellerHost
			}
		}
	}
	if port != 0 && port != 80 && port != 443 {
		return host + ":" + strconv.Itoa(port), nil
	}
	return host, nil
}

func configuredResellerSharedHost() string {
	host, err := NormalizeResellerHost(strings.TrimSpace(os.Getenv("MATRIX_RESELLER_SHARED_HOST")))
	if err != nil {
		return ""
	}
	return host
}

func ValidResellerSubject(subject string) bool {
	if subject == "" || len(subject) > 255 || strings.TrimSpace(subject) != subject {
		return false
	}
	for _, character := range subject {
		if character < 0x20 || character == 0x7f {
			return false
		}
	}
	return true
}

func ResolveResellerContext(subject string, host string) (*ResellerContext, error) {
	query := DB.Table("reseller_members AS rm").
		Joins("JOIN resellers AS r ON r.id = rm.reseller_id AND r.status = ?", ResellerStatusActive).
		Where("rm.subject = ? AND rm.status = ?", subject, ResellerMemberStatusActive)
	if host == configuredResellerSharedHost() {
		var contexts []ResellerContext
		err := query.
			Select("r.id AS reseller_id, r.name AS reseller_name, rm.subject, rm.role").
			Limit(2).
			Scan(&contexts).Error
		if err != nil {
			return nil, err
		}
		if len(contexts) != 1 {
			return nil, gorm.ErrRecordNotFound
		}
		contexts[0].Host = host
		return &contexts[0], nil
	}

	var context ResellerContext
	err := query.
		Select("r.id AS reseller_id, r.name AS reseller_name, rm.subject, rd.host, rm.role").
		Joins("JOIN reseller_domains AS rd ON rd.reseller_id = r.id AND rd.host = ? AND rd.verified = ? AND rd.status = ?", host, true, ResellerDomainStatusActive).
		Take(&context).Error
	if err != nil {
		return nil, err
	}
	return &context, nil
}

func ListResellerAdminRecords() ([]ResellerAdminRecord, error) {
	var records []ResellerAdminRecord
	err := resellerAdminRecordsQuery(DB).Order("r.id ASC").Scan(&records).Error
	if err != nil {
		return nil, err
	}
	for i := range records {
		setResellerAdminBalanceDisplay(&records[i])
	}
	return records, nil
}

func ListResellerMemberRecords(resellerId int) ([]ResellerMemberRecord, error) {
	var records []ResellerMemberRecord
	err := DB.Table("reseller_members").
		Select("id, subject, role, status").
		Where("reseller_id = ?", resellerId).
		Order("id ASC").
		Scan(&records).Error
	if err != nil {
		return nil, err
	}
	return records, nil
}

func ListResellerCustomerRecords(resellerId int, includeRemark bool) ([]ResellerCustomerRecord, error) {
	records := make([]ResellerCustomerRecord, 0)
	err := resellerCustomerRecordsQuery(DB, includeRemark).
		Where("customers.reseller_id = ?", resellerId).
		Order("customers.id ASC").
		Scan(&records).Error
	if err != nil {
		return nil, err
	}
	for i := range records {
		setResellerCustomerBalanceDisplay(&records[i])
	}
	return records, nil
}

func GetResellerCustomerRecord(resellerId int, customerId int, includeRemark bool) (*ResellerCustomerRecord, error) {
	var record ResellerCustomerRecord
	result := resellerCustomerRecordsQuery(DB, includeRemark).
		Where("customers.id = ? AND customers.reseller_id = ?", customerId, resellerId).
		Limit(1).
		Scan(&record)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, ErrResellerCustomerNotFound
	}
	setResellerCustomerBalanceDisplay(&record)
	return &record, nil
}

func UpdateResellerCustomerRecordStatus(resellerId int, customerId int, status string, includeRemark bool) (*ResellerCustomerRecord, error) {
	if status != ResellerCustomerStatusActive && status != ResellerCustomerStatusSuspend {
		return nil, ErrInvalidResellerCustomerStatus
	}
	update := DB.Model(&ResellerCustomer{}).
		Where("id = ? AND reseller_id = ?", customerId, resellerId).
		Update("status", status)
	if update.Error != nil {
		return nil, update.Error
	}
	record, err := GetResellerCustomerRecord(resellerId, customerId, includeRemark)
	if err != nil {
		return nil, err
	}
	return record, nil
}

func UpdateResellerCustomerRecordRemark(resellerId int, customerId int, remark string) (*ResellerCustomerRecord, error) {
	remark = strings.TrimSpace(remark)
	if !validResellerCustomerText(remark, 255) {
		return nil, ErrInvalidResellerCustomerRemark
	}
	update := DB.Model(&ResellerCustomer{}).
		Where("id = ? AND reseller_id = ?", customerId, resellerId).
		Update("remark", remark)
	if update.Error != nil {
		return nil, update.Error
	}
	return GetResellerCustomerRecord(resellerId, customerId, true)
}

func SyncResellerCustomerIdentity(subject string, matrixName string, phone string) (bool, error) {
	matrixName = strings.TrimSpace(matrixName)
	phone = strings.TrimSpace(phone)
	if !ValidResellerSubject(subject) || !validResellerCustomerText(matrixName, 255) || !validResellerCustomerText(phone, 50) {
		return false, ErrInvalidResellerCustomerIdentity
	}
	update := DB.Model(&ResellerCustomer{}).
		Where("subject = ?", subject).
		Updates(map[string]any{
			"matrix_name":       matrixName,
			"phone":             phone,
			"profile_synced_at": common.GetTimestamp(),
		})
	return update.RowsAffected > 0, update.Error
}

func ListPendingResellerCustomerProfiles(afterId int, limit int) (*ResellerCustomerProfileBackfillPage, error) {
	items := make([]ResellerCustomerProfileBackfillCandidate, 0, limit+1)
	err := DB.Model(&ResellerCustomer{}).
		Select("id, subject").
		Where("id > ? AND profile_synced_at = 0", afterId).
		Order("id ASC").
		Limit(limit + 1).
		Scan(&items).Error
	if err != nil {
		return nil, err
	}

	nextAfterId := 0
	if len(items) > limit {
		items = items[:limit]
		nextAfterId = items[len(items)-1].Id
	}
	return &ResellerCustomerProfileBackfillPage{Items: items, NextAfterId: nextAfterId}, nil
}

func ListResellerInvitationRecords(resellerId int) ([]ResellerInvitationRecord, error) {
	var invitations []ResellerInvitation
	err := DB.Where("reseller_id = ?", resellerId).Order("id DESC").Find(&invitations).Error
	if err != nil {
		return nil, err
	}
	now := common.GetTimestamp()
	records := make([]ResellerInvitationRecord, 0, len(invitations))
	for _, invitation := range invitations {
		records = append(records, resellerInvitationRecordFromModel(invitation, now))
	}
	return records, nil
}

func CreateResellerInvitationRecord(resellerId int, createdBySubject string, expiresInHours int) (*ResellerInvitationCreateRecord, error) {
	if !ValidResellerSubject(createdBySubject) {
		return nil, ErrInvalidResellerSubject
	}
	if expiresInHours < 1 || expiresInHours > 168 {
		return nil, ErrInvalidResellerInvitation
	}
	tokenBytes := make([]byte, 24)
	if _, err := crand.Read(tokenBytes); err != nil {
		return nil, err
	}
	token := base64.RawURLEncoding.EncodeToString(tokenBytes)
	invitation := ResellerInvitation{
		ResellerId:       resellerId,
		TokenHash:        resellerInvitationTokenHash(token),
		CreatedBySubject: createdBySubject,
		ExpiresAt:        common.GetTimestamp() + int64(expiresInHours*3600),
	}
	if err := DB.Create(&invitation).Error; err != nil {
		if isResellerUniqueConstraintError(err) {
			return nil, ErrResellerConflict
		}
		return nil, err
	}
	return &ResellerInvitationCreateRecord{
		Invitation: resellerInvitationRecordFromModel(invitation, common.GetTimestamp()),
		Token:      token,
	}, nil
}

func RevokeResellerInvitationRecord(resellerId int, invitationId int) (*ResellerInvitationRecord, error) {
	now := common.GetTimestamp()
	var record *ResellerInvitationRecord
	err := DB.Transaction(func(tx *gorm.DB) error {
		invitation, err := getResellerInvitationByID(tx, resellerId, invitationId)
		if err != nil {
			return err
		}
		if invitation.ConsumedAt != nil {
			return ErrResellerInvitationConsumed
		}
		if invitation.RevokedAt != nil {
			return ErrResellerInvitationRevoked
		}
		if invitation.ExpiresAt < now {
			return ErrResellerInvitationExpired
		}
		update := tx.Model(&ResellerInvitation{}).
			Where("id = ? AND reseller_id = ? AND consumed_at IS NULL AND revoked_at IS NULL AND expires_at >= ?", invitation.Id, resellerId, now).
			Update("revoked_at", now)
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected == 0 {
			current, err := getResellerInvitationByID(tx, resellerId, invitationId)
			if err != nil {
				return err
			}
			return resellerInvitationStateError(current, now)
		}
		invitation.RevokedAt = &now
		record = resellerInvitationRecordPointer(invitation, now)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return record, nil
}

func ConsumeResellerInvitationRecord(token string, subject string, matrixName string, phone string) (*ResellerInvitationConsumeRecord, error) {
	matrixName = strings.TrimSpace(matrixName)
	phone = strings.TrimSpace(phone)
	if token == "" || len(token) > 255 || strings.TrimSpace(token) != token || !ValidResellerSubject(subject) || !validResellerCustomerText(matrixName, 255) || !validResellerCustomerText(phone, 50) {
		return nil, ErrInvalidResellerInvitation
	}

	now := common.GetTimestamp()
	var response *ResellerInvitationConsumeRecord
	err := DB.Transaction(func(tx *gorm.DB) error {
		var invitation ResellerInvitation
		if err := tx.Where("token_hash = ?", resellerInvitationTokenHash(token)).Take(&invitation).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrResellerInvitationNotFound
			}
			return err
		}
		if err := resellerInvitationStateError(&invitation, now); err != nil {
			return err
		}

		var reseller Reseller
		if err := tx.Select("id", "name", "status").Where("id = ?", invitation.ResellerId).Take(&reseller).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrResellerNotFound
			}
			return err
		}
		if reseller.Status != ResellerStatusActive {
			return ErrResellerNotFound
		}

		update := tx.Model(&ResellerInvitation{}).
			Where("id = ? AND consumed_at IS NULL AND revoked_at IS NULL AND expires_at >= ?", invitation.Id, now).
			Updates(map[string]any{
				"consumed_at":         now,
				"consumed_by_subject": subject,
			})
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected == 0 {
			current, err := getResellerInvitationByTokenHash(tx, invitation.TokenHash)
			if err != nil {
				return err
			}
			return resellerInvitationStateError(current, now)
		}

		customer := ResellerCustomer{
			ResellerId:      invitation.ResellerId,
			Subject:         subject,
			MatrixName:      matrixName,
			Phone:           phone,
			ProfileSyncedAt: now,
			Status:          ResellerCustomerStatusActive,
		}
		if err := tx.Create(&customer).Error; err != nil {
			if isResellerUniqueConstraintError(err) {
				return ErrResellerCustomerConflict
			}
			return err
		}

		response = &ResellerInvitationConsumeRecord{
			Customer: ResellerCustomerRecord{
				Id:              customer.Id,
				Subject:         customer.Subject,
				MatrixName:      customer.MatrixName,
				Phone:           customer.Phone,
				ProfileSyncedAt: customer.ProfileSyncedAt,
				Status:          customer.Status,
				JoinedAt:        customer.CreatedAt,
			},
			ResellerId:   invitation.ResellerId,
			ResellerName: reseller.Name,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return response, nil
}

func UnbindResellerCustomerRecord(resellerId int, customerId int) error {
	result := DB.Where("id = ? AND reseller_id = ?", customerId, resellerId).Delete(&ResellerCustomer{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrResellerCustomerNotFound
	}
	return nil
}

func BatchAssignResellerCustomerRecords(resellerId int, inputs []ResellerCustomerBatchAssignInput) (*ResellerCustomerBatchAssignRecord, error) {
	if resellerId < 1 {
		return nil, ErrResellerNotFound
	}

	var reseller Reseller
	if err := DB.Select("id", "status").Where("id = ?", resellerId).Take(&reseller).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrResellerNotFound
		}
		return nil, err
	}
	if reseller.Status != ResellerStatusActive {
		return nil, ErrResellerNotFound
	}

	normalized := make([]ResellerCustomerBatchAssignInput, 0, len(inputs))
	for _, input := range inputs {
		item := ResellerCustomerBatchAssignInput{
			Subject:    input.Subject,
			MatrixName: strings.TrimSpace(input.MatrixName),
			Phone:      strings.TrimSpace(input.Phone),
		}
		if !ValidResellerSubject(item.Subject) || !validResellerCustomerText(item.MatrixName, 255) || !validResellerCustomerText(item.Phone, 50) {
			return nil, ErrInvalidResellerCustomerIdentity
		}
		normalized = append(normalized, item)
	}

	results := make([]ResellerCustomerBatchAssignResult, 0, len(inputs))
	for _, input := range normalized {
		result, err := batchAssignResellerCustomerRecord(resellerId, input.Subject, input.MatrixName, input.Phone)
		if err != nil {
			return nil, err
		}
		results = append(results, *result)
	}

	return &ResellerCustomerBatchAssignRecord{
		ResellerId: resellerId,
		Results:    results,
	}, nil
}

func batchAssignResellerCustomerRecord(resellerId int, subject string, matrixName string, phone string) (*ResellerCustomerBatchAssignResult, error) {
	now := common.GetTimestamp()
	result := ResellerCustomerBatchAssignResult{Subject: subject}
	customer := ResellerCustomer{
		ResellerId:      resellerId,
		Subject:         subject,
		MatrixName:      matrixName,
		Phone:           phone,
		ProfileSyncedAt: now,
		Status:          ResellerCustomerStatusActive,
	}
	if err := DB.Create(&customer).Error; err == nil {
		customerId := customer.Id
		currentResellerId := resellerId
		result.Status = ResellerCustomerBatchAssignStatusAssigned
		result.CustomerId = &customerId
		result.CurrentResellerId = &currentResellerId
		return &result, nil
	} else if !isResellerUniqueConstraintError(err) {
		return nil, err
	}

	var existing ResellerCustomer
	if err := DB.Select("id", "reseller_id").Where("subject = ?", subject).Take(&existing).Error; err != nil {
		return nil, err
	}

	customerId := existing.Id
	currentResellerId := existing.ResellerId
	result.CustomerId = &customerId
	result.CurrentResellerId = &currentResellerId
	if existing.ResellerId == resellerId {
		result.Status = ResellerCustomerBatchAssignStatusAlreadyInTarget
		return &result, nil
	}
	result.Status = ResellerCustomerBatchAssignStatusOwnedByOtherReseller
	return &result, nil
}

func CreateResellerAdminRecord(name string, host string, ownerSubject string) (*ResellerAdminRecord, error) {
	if !validResellerName(name) {
		return nil, ErrInvalidResellerName
	}
	if !validResellerSubject(ownerSubject) {
		return nil, ErrInvalidResellerOwnerSubject
	}
	normalizedHost, err := NormalizeResellerHost(host)
	if err != nil {
		return nil, err
	}

	var record ResellerAdminRecord
	err = DB.Transaction(func(tx *gorm.DB) error {
		reseller := Reseller{Name: name, Status: ResellerStatusActive}
		if err := tx.Create(&reseller).Error; err != nil {
			return err
		}
		if normalizedHost != configuredResellerSharedHost() {
			if err := tx.Create(&ResellerDomain{
				ResellerId: reseller.Id,
				Host:       normalizedHost,
				Verified:   true,
				Status:     ResellerDomainStatusActive,
			}).Error; err != nil {
				return err
			}
		}
		if err := tx.Create(&ResellerMember{
			ResellerId: reseller.Id,
			Subject:    ownerSubject,
			Role:       ResellerRoleOwner,
			Status:     ResellerMemberStatusActive,
		}).Error; err != nil {
			return err
		}
		result := resellerAdminRecordsQuery(tx).Where("r.id = ?", reseller.Id).Limit(1).Scan(&record)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrResellerNotFound
		}
		return nil
	})
	if err != nil {
		if isResellerUniqueConstraintError(err) {
			return nil, ErrResellerConflict
		}
		return nil, err
	}
	setResellerAdminBalanceDisplay(&record)
	return &record, nil
}

func UpdateResellerAdminStatus(id int, status string) (*ResellerAdminRecord, error) {
	if status != ResellerStatusActive && status != ResellerStatusSuspended {
		return nil, ErrInvalidResellerStatus
	}
	update := DB.Model(&Reseller{}).Where("id = ?", id).Update("status", status)
	if update.Error != nil {
		return nil, update.Error
	}
	return GetResellerAdminRecord(id)
}

func UpdateResellerAdminRecord(id int, name string, host string) (*ResellerAdminRecord, error) {
	if !validResellerName(name) {
		return nil, ErrInvalidResellerName
	}
	normalizedHost, err := NormalizeResellerHost(host)
	if err != nil {
		return nil, err
	}

	var record ResellerAdminRecord
	err = DB.Transaction(func(tx *gorm.DB) error {
		var reseller Reseller
		if err := tx.Select("id").Where("id = ?", id).Take(&reseller).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrResellerNotFound
			}
			return err
		}
		if err := tx.Model(&reseller).Update("name", name).Error; err != nil {
			return err
		}
		if err := tx.Where("reseller_id = ?", id).Delete(&ResellerDomain{}).Error; err != nil {
			return err
		}
		if normalizedHost != configuredResellerSharedHost() {
			if err := tx.Create(&ResellerDomain{
				ResellerId: id,
				Host:       normalizedHost,
				Verified:   true,
				Status:     ResellerDomainStatusActive,
			}).Error; err != nil {
				return err
			}
		}
		result := resellerAdminRecordsQuery(tx).Where("r.id = ?", id).Limit(1).Scan(&record)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrResellerNotFound
		}
		return nil
	})
	if err != nil {
		if isResellerUniqueConstraintError(err) {
			return nil, ErrResellerConflict
		}
		return nil, err
	}
	setResellerAdminBalanceDisplay(&record)
	return &record, nil
}

func GetResellerAdminRecord(id int) (*ResellerAdminRecord, error) {
	var record ResellerAdminRecord
	result := resellerAdminRecordsQuery(DB).Where("r.id = ?", id).Limit(1).Scan(&record)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, ErrResellerNotFound
	}
	setResellerAdminBalanceDisplay(&record)
	return &record, nil
}

func resellerAdminRecordsQuery(db *gorm.DB) *gorm.DB {
	return db.Table("resellers AS r").
		Select("r.id, r.name, r.status, rd.host, owner.subject AS owner_subject, COALESCE(owner_sso_user.id, owner_oidc_user.id, 0) AS owner_user_id, COALESCE(owner_sso_user.username, owner_oidc_user.username, '') AS owner_username, COALESCE(owner_sso_user.display_name, owner_oidc_user.display_name, '') AS owner_display_name, COALESCE(owner_sso_user.quota, owner_oidc_user.quota, 0) AS owner_balance_quota, COALESCE((SELECT SUM(owner_gift.balance) FROM mozia_wallet_balances AS owner_gift WHERE owner_gift.user_id = COALESCE(owner_sso_user.id, owner_oidc_user.id, 0) AND owner_gift.source = 'gift'), 0) AS owner_gift_balance_quota, COALESCE((SELECT SUM(owner_paid.balance) FROM mozia_wallet_balances AS owner_paid WHERE owner_paid.user_id = COALESCE(owner_sso_user.id, owner_oidc_user.id, 0) AND owner_paid.source = 'paid'), 0) AS owner_paid_balance_quota, COALESCE(owner_sso_user.request_count, owner_oidc_user.request_count, 0) AS owner_request_count, COUNT(DISTINCT members.id) AS member_count").
		Joins("LEFT JOIN reseller_domains AS rd ON rd.reseller_id = r.id AND rd.verified = ? AND rd.status = ?", true, ResellerDomainStatusActive).
		Joins("LEFT JOIN reseller_members AS owner ON owner.reseller_id = r.id AND owner.role = ? AND owner.status = ?", ResellerRoleOwner, ResellerMemberStatusActive).
		Joins("LEFT JOIN user_ssos AS owner_sso ON owner_sso.sso_sub = owner.subject").
		Joins("LEFT JOIN users AS owner_sso_user ON owner_sso_user.id = owner_sso.user_id AND owner_sso_user.deleted_at IS NULL").
		Joins("LEFT JOIN users AS owner_oidc_user ON owner_oidc_user.id = (SELECT MIN(owner_candidate.id) FROM users AS owner_candidate WHERE owner_candidate.oidc_id = owner.subject AND owner_candidate.deleted_at IS NULL)").
		Joins("LEFT JOIN reseller_members AS members ON members.reseller_id = r.id AND members.status = ?", ResellerMemberStatusActive).
		Group("r.id, r.name, r.status, rd.host, owner.subject, owner_sso_user.id, owner_sso_user.username, owner_sso_user.display_name, owner_sso_user.quota, owner_sso_user.request_count, owner_oidc_user.id, owner_oidc_user.username, owner_oidc_user.display_name, owner_oidc_user.quota, owner_oidc_user.request_count")
}

func resellerCustomerRecordsQuery(db *gorm.DB, includeRemark bool) *gorm.DB {
	fields := "customers.id, customers.subject, customers.status, customers.created_at AS joined_at, customers.matrix_name, customers.phone, customers.profile_synced_at, COALESCE(customer_sso_user.id, customer_oidc_user.id, 0) AS user_id, COALESCE(customer_sso_user.username, customer_oidc_user.username, '') AS username, COALESCE(customer_sso_user.display_name, customer_oidc_user.display_name, '') AS display_name, COALESCE(customer_sso_user.quota, customer_oidc_user.quota, 0) AS balance_quota, COALESCE((SELECT SUM(customer_gift.balance) FROM mozia_wallet_balances AS customer_gift WHERE customer_gift.user_id = COALESCE(customer_sso_user.id, customer_oidc_user.id, 0) AND customer_gift.source = 'gift'), 0) AS gift_balance_quota, COALESCE((SELECT SUM(customer_paid.balance) FROM mozia_wallet_balances AS customer_paid WHERE customer_paid.user_id = COALESCE(customer_sso_user.id, customer_oidc_user.id, 0) AND customer_paid.source = 'paid'), 0) AS paid_balance_quota, COALESCE(customer_sso_user.request_count, customer_oidc_user.request_count, 0) AS request_count"
	if includeRemark {
		fields += ", customers.remark"
	}
	return db.Table("reseller_customers AS customers").
		Select(fields).
		Joins("LEFT JOIN user_ssos AS customer_sso ON customer_sso.sso_sub = customers.subject").
		Joins("LEFT JOIN users AS customer_sso_user ON customer_sso_user.id = customer_sso.user_id AND customer_sso_user.deleted_at IS NULL").
		Joins("LEFT JOIN users AS customer_oidc_user ON customer_oidc_user.id = (SELECT MIN(customer_candidate.id) FROM users AS customer_candidate WHERE customer_candidate.oidc_id = customers.subject AND customer_candidate.deleted_at IS NULL)")
}

func validResellerCustomerText(value string, maxRunes int) bool {
	if utf8.RuneCountInString(value) > maxRunes || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return false
		}
	}
	return true
}

func resellerBalanceAmount(quota int) float64 {
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		return float64(quota)
	}
	if common.QuotaPerUnit <= 0 {
		return 0
	}
	return float64(quota) / common.QuotaPerUnit * operation_setting.GetUsdToCurrencyRate(operation_setting.USDExchangeRate)
}

func setResellerAdminBalanceDisplay(record *ResellerAdminRecord) {
	if record.Host == "" {
		record.Host = configuredResellerSharedHost()
	}
	record.OwnerBalance = resellerBalanceAmount(record.OwnerBalanceQuota)
	record.OwnerGiftBalance = resellerBalanceAmount(record.OwnerGiftBalanceQuota)
	record.OwnerPaidBalance = resellerBalanceAmount(record.OwnerPaidBalanceQuota)
	record.BalanceDisplayType = operation_setting.GetQuotaDisplayType()
	record.BalanceCurrencySymbol = operation_setting.GetCurrencySymbol()
}

func setResellerCustomerBalanceDisplay(record *ResellerCustomerRecord) {
	record.Balance = resellerBalanceAmount(record.BalanceQuota)
	record.GiftBalance = resellerBalanceAmount(record.GiftBalanceQuota)
	record.PaidBalance = resellerBalanceAmount(record.PaidBalanceQuota)
	record.BalanceDisplayType = operation_setting.GetQuotaDisplayType()
	record.BalanceCurrencySymbol = operation_setting.GetCurrencySymbol()
}

func validResellerName(name string) bool {
	if name == "" || len(name) > 128 || strings.TrimSpace(name) != name {
		return false
	}
	for _, character := range name {
		if character < 0x20 || character == 0x7f {
			return false
		}
	}
	return true
}

func validResellerSubject(subject string) bool {
	return ValidResellerSubject(subject)
}

func isResellerUniqueConstraintError(err error) bool {
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "duplicate key") ||
		strings.Contains(message, "duplicated key") ||
		strings.Contains(message, "duplicate entry") ||
		strings.Contains(message, "unique constraint") ||
		strings.Contains(message, "unique failed")
}

func resellerInvitationTokenHash(token string) string {
	return hex.EncodeToString(common.Sha256Raw([]byte(token)))
}

func resellerInvitationStatus(invitation ResellerInvitation, now int64) string {
	if invitation.ConsumedAt != nil {
		return ResellerInvitationStatusConsumed
	}
	if invitation.RevokedAt != nil {
		return ResellerInvitationStatusRevoked
	}
	if invitation.ExpiresAt < now {
		return ResellerInvitationStatusExpired
	}
	return ResellerInvitationStatusPending
}

func resellerInvitationRecordFromModel(invitation ResellerInvitation, now int64) ResellerInvitationRecord {
	return ResellerInvitationRecord{
		Id:                invitation.Id,
		CreatedBySubject:  invitation.CreatedBySubject,
		ExpiresAt:         invitation.ExpiresAt,
		RevokedAt:         invitation.RevokedAt,
		ConsumedAt:        invitation.ConsumedAt,
		ConsumedBySubject: invitation.ConsumedBySubject,
		CreatedAt:         invitation.CreatedAt,
		Status:            resellerInvitationStatus(invitation, now),
	}
}

func resellerInvitationRecordPointer(invitation *ResellerInvitation, now int64) *ResellerInvitationRecord {
	record := resellerInvitationRecordFromModel(*invitation, now)
	return &record
}

func resellerInvitationStateError(invitation *ResellerInvitation, now int64) error {
	switch resellerInvitationStatus(*invitation, now) {
	case ResellerInvitationStatusConsumed:
		return ErrResellerInvitationConsumed
	case ResellerInvitationStatusRevoked:
		return ErrResellerInvitationRevoked
	case ResellerInvitationStatusExpired:
		return ErrResellerInvitationExpired
	default:
		return nil
	}
}

func getResellerInvitationByID(tx *gorm.DB, resellerId int, invitationId int) (*ResellerInvitation, error) {
	var invitation ResellerInvitation
	if err := tx.Where("id = ? AND reseller_id = ?", invitationId, resellerId).Take(&invitation).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrResellerInvitationNotFound
		}
		return nil, err
	}
	return &invitation, nil
}

func getResellerInvitationByTokenHash(tx *gorm.DB, tokenHash string) (*ResellerInvitation, error) {
	var invitation ResellerInvitation
	if err := tx.Where("token_hash = ?", tokenHash).Take(&invitation).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrResellerInvitationNotFound
		}
		return nil, err
	}
	return &invitation, nil
}
