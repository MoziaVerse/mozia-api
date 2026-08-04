package model

import (
	"errors"
	"strconv"
	"strings"
)

const (
	ResellerStatusActive       = "active"
	ResellerDomainStatusActive = "active"
	ResellerMemberStatusActive = "active"

	ResellerRoleOwner  = "owner"
	ResellerRoleAdmin  = "admin"
	ResellerRoleViewer = "viewer"
)

var errInvalidResellerHost = errors.New("invalid reseller host")

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

func NormalizeResellerHost(raw string) (string, error) {
	if raw == "" || len(raw) > 260 || strings.TrimSpace(raw) != raw {
		return "", errInvalidResellerHost
	}

	host := raw
	port := 0
	if strings.Contains(host, ":") {
		if strings.Count(host, ":") != 1 {
			return "", errInvalidResellerHost
		}
		separator := strings.LastIndexByte(host, ':')
		parsedPort, err := strconv.Atoi(host[separator+1:])
		if err != nil || parsedPort < 1 || parsedPort > 65535 {
			return "", errInvalidResellerHost
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
			return "", errInvalidResellerHost
		}
	}
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if host == "" || len(host) > 253 {
		return "", errInvalidResellerHost
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", errInvalidResellerHost
		}
		for i := 0; i < len(label); i++ {
			character := label[i]
			if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' {
				return "", errInvalidResellerHost
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
