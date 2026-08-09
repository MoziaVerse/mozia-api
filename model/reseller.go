package model

import (
	crand "crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
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
)

var (
	ErrInvalidResellerHost           = errors.New("invalid reseller host")
	ErrInvalidResellerName           = errors.New("invalid reseller name")
	ErrInvalidResellerOwnerSubject   = errors.New("invalid reseller owner subject")
	ErrInvalidResellerStatus         = errors.New("invalid reseller status")
	ErrInvalidResellerCustomerStatus = errors.New("invalid reseller customer status")
	ErrInvalidResellerInvitation     = errors.New("invalid reseller invitation")
	ErrInvalidResellerSubject        = errors.New("invalid reseller subject")
	ErrResellerConflict              = errors.New("reseller conflict")
	ErrResellerNotFound              = errors.New("reseller not found")
	ErrResellerForbidden             = errors.New("reseller forbidden")
	ErrResellerInvitationNotFound    = errors.New("reseller invitation not found")
	ErrResellerInvitationExpired     = errors.New("reseller invitation expired")
	ErrResellerInvitationRevoked     = errors.New("reseller invitation revoked")
	ErrResellerInvitationConsumed    = errors.New("reseller invitation consumed")
	ErrResellerCustomerNotFound      = errors.New("reseller customer not found")
	ErrResellerCustomerConflict      = errors.New("reseller customer conflict")
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
	Id         int    `json:"id" gorm:"primaryKey;autoIncrement"`
	ResellerId int    `json:"reseller_id" gorm:"not null;index"`
	Subject    string `json:"subject" gorm:"type:varchar(255);not null;uniqueIndex"`
	Status     string `json:"status" gorm:"type:varchar(16);not null;index"`
	CreatedAt  int64  `json:"created_at" gorm:"autoCreateTime;column:created_at"`
	UpdatedAt  int64  `json:"updated_at" gorm:"autoUpdateTime;column:updated_at"`
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
	Id               int    `json:"id"`
	Name             string `json:"name"`
	Status           string `json:"status"`
	Host             string `json:"host"`
	OwnerSubject     string `json:"owner_subject"`
	OwnerUsername    string `json:"owner_username"`
	OwnerDisplayName string `json:"owner_display_name"`
	MemberCount      int    `json:"member_count"`
}

type ResellerMemberRecord struct {
	Id      int    `json:"id"`
	Subject string `json:"subject"`
	Role    string `json:"role"`
	Status  string `json:"status"`
}

type ResellerCustomerRecord struct {
	Id       int    `json:"id"`
	Subject  string `json:"subject"`
	Status   string `json:"status"`
	JoinedAt int64  `json:"joined_at"`
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

type ResellerCustomerTransferRecord struct {
	PreviousResellerId int                    `json:"previous_reseller_id"`
	TargetResellerId   int                    `json:"target_reseller_id"`
	Customer           ResellerCustomerRecord `json:"customer"`
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
	var context ResellerContext
	err := DB.Table("reseller_members AS rm").
		Select("r.id AS reseller_id, r.name AS reseller_name, rm.subject, rd.host, rm.role").
		Joins("JOIN resellers AS r ON r.id = rm.reseller_id AND r.status = ?", ResellerStatusActive).
		Joins("JOIN reseller_domains AS rd ON rd.reseller_id = r.id AND rd.host = ? AND rd.verified = ? AND rd.status = ?", host, true, ResellerDomainStatusActive).
		Where("rm.subject = ? AND rm.status = ?", subject, ResellerMemberStatusActive).
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

func ListResellerCustomerRecords(resellerId int) ([]ResellerCustomerRecord, error) {
	records := make([]ResellerCustomerRecord, 0)
	err := DB.Table("reseller_customers").
		Select("id, subject, status, created_at AS joined_at").
		Where("reseller_id = ?", resellerId).
		Order("id ASC").
		Scan(&records).Error
	if err != nil {
		return nil, err
	}
	return records, nil
}

func GetResellerCustomerRecord(resellerId int, customerId int) (*ResellerCustomerRecord, error) {
	var record ResellerCustomerRecord
	result := DB.Table("reseller_customers").
		Select("id, subject, status, created_at AS joined_at").
		Where("id = ? AND reseller_id = ?", customerId, resellerId).
		Limit(1).
		Scan(&record)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, ErrResellerCustomerNotFound
	}
	return &record, nil
}

func UpdateResellerCustomerRecordStatus(resellerId int, customerId int, status string) (*ResellerCustomerRecord, error) {
	if status != ResellerCustomerStatusActive && status != ResellerCustomerStatusSuspend {
		return nil, ErrInvalidResellerCustomerStatus
	}
	update := DB.Model(&ResellerCustomer{}).
		Where("id = ? AND reseller_id = ?", customerId, resellerId).
		Update("status", status)
	if update.Error != nil {
		return nil, update.Error
	}
	record, err := GetResellerCustomerRecord(resellerId, customerId)
	if err != nil {
		return nil, err
	}
	return record, nil
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

func ConsumeResellerInvitationRecord(token string, subject string) (*ResellerInvitationConsumeRecord, error) {
	if token == "" || len(token) > 255 || strings.TrimSpace(token) != token || !ValidResellerSubject(subject) {
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
			ResellerId: invitation.ResellerId,
			Subject:    subject,
			Status:     ResellerCustomerStatusActive,
		}
		if err := tx.Create(&customer).Error; err != nil {
			if isResellerUniqueConstraintError(err) {
				return ErrResellerCustomerConflict
			}
			return err
		}

		response = &ResellerInvitationConsumeRecord{
			Customer: ResellerCustomerRecord{
				Id:       customer.Id,
				Subject:  customer.Subject,
				Status:   customer.Status,
				JoinedAt: customer.CreatedAt,
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

func TransferResellerCustomerRecord(customerId int, targetResellerId int) (*ResellerCustomerTransferRecord, error) {
	if targetResellerId < 1 {
		return nil, ErrResellerNotFound
	}

	var response *ResellerCustomerTransferRecord
	err := DB.Transaction(func(tx *gorm.DB) error {
		var customer ResellerCustomer
		if err := tx.Where("id = ?", customerId).Take(&customer).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrResellerCustomerNotFound
			}
			return err
		}

		var target Reseller
		if err := tx.Select("id", "status").Where("id = ?", targetResellerId).Take(&target).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrResellerNotFound
			}
			return err
		}
		if target.Status != ResellerStatusActive {
			return ErrResellerNotFound
		}

		previousResellerId := customer.ResellerId
		if previousResellerId == targetResellerId {
			return ErrResellerCustomerConflict
		}
		if err := tx.Model(&ResellerCustomer{}).
			Where("id = ?", customerId).
			Update("reseller_id", targetResellerId).Error; err != nil {
			return err
		}
		customer.ResellerId = targetResellerId

		response = &ResellerCustomerTransferRecord{
			PreviousResellerId: previousResellerId,
			TargetResellerId:   targetResellerId,
			Customer: ResellerCustomerRecord{
				Id:       customer.Id,
				Subject:  customer.Subject,
				Status:   customer.Status,
				JoinedAt: customer.CreatedAt,
			},
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return response, nil
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
		if err := tx.Create(&ResellerDomain{
			ResellerId: reseller.Id,
			Host:       normalizedHost,
			Verified:   true,
			Status:     ResellerDomainStatusActive,
		}).Error; err != nil {
			return err
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

func GetResellerAdminRecord(id int) (*ResellerAdminRecord, error) {
	var record ResellerAdminRecord
	result := resellerAdminRecordsQuery(DB).Where("r.id = ?", id).Limit(1).Scan(&record)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, ErrResellerNotFound
	}
	return &record, nil
}

func resellerAdminRecordsQuery(db *gorm.DB) *gorm.DB {
	return db.Table("resellers AS r").
		Select("r.id, r.name, r.status, rd.host, owner.subject AS owner_subject, owner_user.username AS owner_username, owner_user.display_name AS owner_display_name, COUNT(DISTINCT members.id) AS member_count").
		Joins("LEFT JOIN reseller_domains AS rd ON rd.reseller_id = r.id AND rd.verified = ? AND rd.status = ?", true, ResellerDomainStatusActive).
		Joins("LEFT JOIN reseller_members AS owner ON owner.reseller_id = r.id AND owner.role = ? AND owner.status = ?", ResellerRoleOwner, ResellerMemberStatusActive).
		Joins("LEFT JOIN users AS owner_user ON owner_user.id = (SELECT MIN(owner_candidate.id) FROM users AS owner_candidate WHERE owner_candidate.oidc_id = owner.subject AND owner_candidate.deleted_at IS NULL)").
		Joins("LEFT JOIN reseller_members AS members ON members.reseller_id = r.id AND members.status = ?", ResellerMemberStatusActive).
		Group("r.id, r.name, r.status, rd.host, owner.subject, owner_user.username, owner_user.display_name")
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
