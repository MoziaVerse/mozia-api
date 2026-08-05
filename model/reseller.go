package model

import (
	"errors"
	"strconv"
	"strings"

	"gorm.io/gorm"
)

const (
	ResellerStatusActive       = "active"
	ResellerStatusSuspended    = "suspended"
	ResellerDomainStatusActive = "active"
	ResellerMemberStatusActive = "active"

	ResellerRoleOwner  = "owner"
	ResellerRoleAdmin  = "admin"
	ResellerRoleViewer = "viewer"
)

var (
	ErrInvalidResellerHost         = errors.New("invalid reseller host")
	ErrInvalidResellerName         = errors.New("invalid reseller name")
	ErrInvalidResellerOwnerSubject = errors.New("invalid reseller owner subject")
	ErrInvalidResellerStatus       = errors.New("invalid reseller status")
	ErrResellerConflict            = errors.New("reseller conflict")
	ErrResellerNotFound            = errors.New("reseller not found")
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

type ResellerContext struct {
	ResellerId   int    `json:"reseller_id"`
	ResellerName string `json:"reseller_name"`
	Host         string `json:"host"`
	Role         string `json:"role"`
}

type ResellerAdminRecord struct {
	Id           int    `json:"id"`
	Name         string `json:"name"`
	Status       string `json:"status"`
	Host         string `json:"host"`
	OwnerSubject string `json:"owner_subject"`
	MemberCount  int    `json:"member_count"`
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

func ResolveResellerContext(subject string, host string) (*ResellerContext, error) {
	var context ResellerContext
	err := DB.Table("reseller_members AS rm").
		Select("r.id AS reseller_id, r.name AS reseller_name, rd.host, rm.role").
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

	var record *ResellerAdminRecord
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
		record = &ResellerAdminRecord{
			Id:           reseller.Id,
			Name:         reseller.Name,
			Status:       reseller.Status,
			Host:         normalizedHost,
			OwnerSubject: ownerSubject,
			MemberCount:  1,
		}
		return nil
	})
	if err != nil {
		if isResellerUniqueConstraintError(err) {
			return nil, ErrResellerConflict
		}
		return nil, err
	}
	return record, nil
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
		Select("r.id, r.name, r.status, rd.host, owner.subject AS owner_subject, COUNT(DISTINCT members.id) AS member_count").
		Joins("LEFT JOIN reseller_domains AS rd ON rd.reseller_id = r.id AND rd.verified = ? AND rd.status = ?", true, ResellerDomainStatusActive).
		Joins("LEFT JOIN reseller_members AS owner ON owner.reseller_id = r.id AND owner.role = ? AND owner.status = ?", ResellerRoleOwner, ResellerMemberStatusActive).
		Joins("LEFT JOIN reseller_members AS members ON members.reseller_id = r.id AND members.status = ?", ResellerMemberStatusActive).
		Group("r.id, r.name, r.status, rd.host, owner.subject")
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
