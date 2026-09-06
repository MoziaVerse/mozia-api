package model

import (
	"database/sql/driver"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

var ErrInvalidResellerModelAccess = errors.New("invalid reseller model access")

// An unrestricted zero value preserves existing portals and API keys.
// Stored as TEXT, not a database-specific JSON type.
type ResellerModelAccess struct {
	Restricted bool     `json:"restricted"`
	Models     []string `json:"models"`
}

func (p ResellerModelAccess) Value() (driver.Value, error) {
	if p.Models == nil {
		p.Models = []string{}
	}
	b, err := common.Marshal(p)
	return string(b), err
}

func (p *ResellerModelAccess) Scan(value any) error {
	*p = ResellerModelAccess{Models: []string{}}
	switch v := value.(type) {
	case nil:
		return nil
	case string:
		return common.UnmarshalJsonStr(v, p)
	case []byte:
		return common.Unmarshal(v, p)
	default:
		return fmt.Errorf("invalid reseller model access storage: %T", value)
	}
}

func (p ResellerModelAccess) Allows(modelName string) bool {
	return !p.Restricted || slices.Contains(p.Models, modelName)
}

func (p ResellerModelAccess) Filter(models []string) []string {
	if !p.Restricted {
		return models
	}
	result := make([]string, 0, len(models))
	for _, name := range models {
		if p.Allows(name) {
			result = append(result, name)
		}
	}
	return result
}

func UpdateResellerModelAccess(id int, policy ResellerModelAccess) (*ResellerModelAccess, error) {
	var reseller Reseller
	if err := DB.Select("id", "model_access").First(&reseller, id).Error; err != nil {
		return nil, err
	}
	if len(policy.Models) > 1000 {
		return nil, ErrInvalidResellerModelAccess
	}
	var enabledModels []string
	if err := DB.Model(&Ability{}).Where("enabled = ?", true).Distinct("model").Pluck("model", &enabledModels).Error; err != nil {
		return nil, err
	}
	selected := make([]string, 0, len(policy.Models))
	for _, name := range policy.Models {
		name = strings.TrimSpace(name)
		// Previously selected, retired models can be retained, but arbitrary IDs cannot be granted.
		if name == "" || len(name) > 255 || (!slices.Contains(enabledModels, name) && !slices.Contains(reseller.ModelAccess.Models, name)) {
			return nil, ErrInvalidResellerModelAccess
		}
		selected = append(selected, name)
	}
	if !policy.Restricted {
		selected = []string{}
	}
	slices.Sort(selected)
	policy.Models = slices.Compact(selected)
	encoded, err := common.Marshal(policy)
	if err != nil {
		return nil, err
	}
	if len(encoded) > 60<<10 { // Keep below MySQL TEXT's 64 KiB byte limit.
		return nil, ErrInvalidResellerModelAccess
	}
	if err := DB.Model(&reseller).Update("model_access", policy).Error; err != nil {
		return nil, err
	}
	return &policy, nil
}

// Resolve from trusted user identity, never a browser-supplied Host or reseller ID.
func GetUserResellerModelAccess(userId int) (ResellerModelAccess, error) {
	policy := ResellerModelAccess{Models: []string{}}
	if userId <= 0 {
		return policy, nil
	}
	var records []struct {
		ModelAccess    ResellerModelAccess `gorm:"type:text"`
		CustomerStatus string
		ResellerStatus string
	}
	// ponytail: read the authority on each request; add shared cache invalidation only if measured load warrants it.
	err := DB.Table("reseller_customers AS rc").
		Select("r.model_access, rc.status AS customer_status, r.status AS reseller_status").
		Joins("JOIN resellers AS r ON r.id = rc.reseller_id").
		Where("rc.subject IN (SELECT sso_sub FROM user_ssos WHERE user_id = ?) OR rc.subject IN (SELECT oidc_id FROM users WHERE id = ? AND oidc_id <> '')", userId, userId).
		Limit(2).Scan(&records).Error
	if err != nil {
		return policy, err
	}
	if len(records) > 1 {
		return policy, ErrResellerBillingIdentityConflict
	}
	if len(records) == 1 {
		if records[0].ModelAccess.Restricted && (records[0].CustomerStatus != ResellerCustomerStatusActive || records[0].ResellerStatus != ResellerStatusActive) {
			return ResellerModelAccess{Restricted: true, Models: []string{}}, nil
		}
		return records[0].ModelAccess, nil
	}
	return policy, nil
}
