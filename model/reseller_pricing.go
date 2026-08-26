package model

import (
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	ResellerPriceRuleKindWholesale = "wholesale"
	ResellerPriceRuleKindRetail    = "retail"

	ResellerDefaultMultiplierPPM int64 = 1_000_000

	ResellerSettlementStatusReserved = "reserved"
	ResellerSettlementStatusSettling = "settling"
	ResellerSettlementStatusSettled  = "settled"
	ResellerSettlementStatusRefunded = "refunded"
	ResellerSettlementStatusFailed   = "failed"
)

var (
	ErrInvalidResellerPriceRule         = errors.New("invalid reseller price rule")
	ErrResellerPriceRuleVersionConflict = errors.New("reseller price rule version conflict")
	ErrResellerPriceMarginConflict      = errors.New("reseller price margin conflict")
	ErrResellerBillingIdentityConflict  = errors.New("reseller billing identity conflict")
	ErrResellerSettlementConflict       = errors.New("reseller settlement conflict")
	ErrResellerQuotaOverflow            = errors.New("reseller quota overflow")
)

// ResellerPriceRule is an immutable rule version. A new price always creates a
// new row; existing versions are never updated in place.
type ResellerPriceRule struct {
	Id            int64  `json:"id" gorm:"primaryKey;autoIncrement"`
	ResellerId    int    `json:"reseller_id" gorm:"not null;index;uniqueIndex:uq_reseller_price_rule_version,priority:1"`
	Kind          string `json:"kind" gorm:"type:varchar(16);not null;index;uniqueIndex:uq_reseller_price_rule_version,priority:2"`
	ModelName     string `json:"model_name" gorm:"type:varchar(191);not null;index;uniqueIndex:uq_reseller_price_rule_version,priority:3"`
	CustomerId    int    `json:"customer_id" gorm:"not null;index;uniqueIndex:uq_reseller_price_rule_version,priority:4"`
	Version       int    `json:"version" gorm:"not null;uniqueIndex:uq_reseller_price_rule_version,priority:5"`
	MultiplierPPM int64  `json:"-" gorm:"type:bigint;not null"`
	Enabled       bool   `json:"enabled" gorm:"not null"`
	EffectiveAt   int64  `json:"effective_at" gorm:"not null;index"`
	CreatedBy     string `json:"created_by" gorm:"type:varchar(255);not null"`
	CreatedAt     int64  `json:"created_at" gorm:"autoCreateTime;column:created_at"`
}

type ResellerPriceRuleRecord struct {
	Id          int64  `json:"id"`
	Kind        string `json:"kind"`
	Model       string `json:"model"`
	Multiplier  string `json:"multiplier"`
	CustomerId  *int   `json:"customer_id"`
	Version     int    `json:"version"`
	Enabled     bool   `json:"enabled"`
	EffectiveAt int64  `json:"effective_at"`
	CreatedBy   string `json:"created_by"`
	CreatedAt   int64  `json:"created_at"`
}

type CreateResellerPriceRuleParams struct {
	ResellerId               int
	Kind                     string
	ModelName                string
	CustomerId               int
	MultiplierPPM            int64
	ExpectedVersion          *int
	Enabled                  bool
	EffectiveAt              int64
	CreatedBy                string
	RequiredSubagentMemberId *int
}

type ResellerEffectivePrice struct {
	RuleId        int64
	RuleVersion   int
	MultiplierPPM int64
	Source        string
}

type ResellerBillingCustomer struct {
	ResellerId int
	CustomerId int
	UserId     int
}

// ResellerRequestSettlement is the main-database financial snapshot. All rule
// identifiers and multipliers are frozen before customer funds are reserved.
type ResellerRequestSettlement struct {
	Id                      int64  `json:"id" gorm:"primaryKey;autoIncrement"`
	RequestId               string `json:"request_id" gorm:"type:varchar(64);not null;uniqueIndex"`
	ResellerId              int    `json:"reseller_id" gorm:"not null;index"`
	CustomerId              int    `json:"customer_id" gorm:"not null;index"`
	UserId                  int    `json:"user_id" gorm:"not null;index"`
	ModelName               string `json:"model_name" gorm:"type:varchar(191);not null;index"`
	WholesaleRuleId         int64  `json:"wholesale_rule_id" gorm:"type:bigint;not null"`
	WholesaleRuleVersion    int    `json:"wholesale_rule_version" gorm:"not null"`
	WholesaleMultiplierPPM  int64  `json:"-" gorm:"type:bigint;not null"`
	RetailRuleId            int64  `json:"retail_rule_id" gorm:"type:bigint;not null"`
	RetailRuleVersion       int    `json:"retail_rule_version" gorm:"not null"`
	RetailMultiplierPPM     int64  `json:"-" gorm:"type:bigint;not null"`
	EstimatedBaseQuota      int64  `json:"estimated_base_quota" gorm:"type:bigint;not null"`
	EstimatedCustomerQuota  int64  `json:"estimated_customer_quota" gorm:"type:bigint;not null"`
	EstimatedWholesaleQuota int64  `json:"estimated_wholesale_quota" gorm:"type:bigint;not null"`
	ActualBaseQuota         int64  `json:"actual_base_quota" gorm:"type:bigint;not null"`
	ActualCustomerQuota     int64  `json:"actual_customer_quota" gorm:"type:bigint;not null"`
	ActualWholesaleQuota    int64  `json:"actual_wholesale_quota" gorm:"type:bigint;not null"`
	UsageJSON               string `json:"usage_json,omitempty" gorm:"type:text;not null"`
	Status                  string `json:"status" gorm:"type:varchar(16);not null;index"`
	CreatedAt               int64  `json:"created_at" gorm:"autoCreateTime;column:created_at"`
	UpdatedAt               int64  `json:"updated_at" gorm:"autoUpdateTime;column:updated_at"`
	SettledAt               int64  `json:"settled_at" gorm:"type:bigint;not null"`
	RefundedAt              int64  `json:"refunded_at" gorm:"type:bigint;not null"`
}

func ParseResellerMultiplier(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.HasPrefix(value, "+") || strings.HasPrefix(value, "-") || strings.Count(value, ".") > 1 {
		return 0, ErrInvalidResellerPriceRule
	}
	parts := strings.SplitN(value, ".", 2)
	if parts[0] == "" {
		return 0, ErrInvalidResellerPriceRule
	}
	for _, part := range parts {
		for _, character := range part {
			if character < '0' || character > '9' {
				return 0, ErrInvalidResellerPriceRule
			}
		}
	}
	if len(parts) == 2 && parts[1] == "" {
		return 0, ErrInvalidResellerPriceRule
	}

	whole := new(big.Int)
	if _, ok := whole.SetString(parts[0], 10); !ok {
		return 0, ErrInvalidResellerPriceRule
	}
	ppm := new(big.Int).Mul(whole, big.NewInt(ResellerDefaultMultiplierPPM))
	if len(parts) == 2 {
		fraction := parts[1]
		kept := fraction
		if len(kept) > 6 {
			kept = kept[:6]
		}
		kept += strings.Repeat("0", 6-len(kept))
		fractionPPM, err := strconv.ParseInt(kept, 10, 64)
		if err != nil {
			return 0, ErrInvalidResellerPriceRule
		}
		ppm.Add(ppm, big.NewInt(fractionPPM))
		if len(fraction) > 6 && fraction[6] >= '5' {
			ppm.Add(ppm, big.NewInt(1))
		}
	}
	if !ppm.IsInt64() || ppm.Sign() <= 0 {
		return 0, ErrInvalidResellerPriceRule
	}
	return ppm.Int64(), nil
}

func FormatResellerMultiplier(multiplierPPM int64) string {
	if multiplierPPM <= 0 {
		return ""
	}
	whole := multiplierPPM / ResellerDefaultMultiplierPPM
	fraction := multiplierPPM % ResellerDefaultMultiplierPPM
	if fraction == 0 {
		return strconv.FormatInt(whole, 10)
	}
	fractionString := strings.TrimRight(fmt.Sprintf("%06d", fraction), "0")
	return strconv.FormatInt(whole, 10) + "." + fractionString
}

func ApplyResellerMultiplier(baseQuota int64, multiplierPPM int64) (int64, error) {
	if baseQuota < 0 || multiplierPPM <= 0 {
		return 0, ErrInvalidResellerPriceRule
	}
	if baseQuota == 0 {
		return 0, nil
	}
	product := new(big.Int).Mul(big.NewInt(baseQuota), big.NewInt(multiplierPPM))
	product.Add(product, big.NewInt(ResellerDefaultMultiplierPPM/2))
	product.Quo(product, big.NewInt(ResellerDefaultMultiplierPPM))
	if !product.IsInt64() || product.Sign() < 0 {
		return 0, ErrResellerQuotaOverflow
	}
	return product.Int64(), nil
}

func (rule ResellerPriceRule) Record() ResellerPriceRuleRecord {
	var customerId *int
	if rule.CustomerId > 0 {
		value := rule.CustomerId
		customerId = &value
	}
	return ResellerPriceRuleRecord{
		Id: rule.Id, Kind: rule.Kind, Model: rule.ModelName,
		Multiplier: FormatResellerMultiplier(rule.MultiplierPPM), CustomerId: customerId,
		Version: rule.Version, Enabled: rule.Enabled, EffectiveAt: rule.EffectiveAt,
		CreatedBy: rule.CreatedBy, CreatedAt: rule.CreatedAt,
	}
}

func CreateResellerPriceRule(params CreateResellerPriceRuleParams) (*ResellerPriceRule, error) {
	params.ModelName = strings.TrimSpace(params.ModelName)
	params.CreatedBy = strings.TrimSpace(params.CreatedBy)
	if params.ResellerId < 1 || len(params.ModelName) == 0 || len(params.ModelName) > 191 ||
		params.MultiplierPPM <= 0 || len(params.CreatedBy) == 0 || len(params.CreatedBy) > 255 ||
		(params.Kind != ResellerPriceRuleKindWholesale && params.Kind != ResellerPriceRuleKindRetail) ||
		params.CustomerId < 0 || params.Kind == ResellerPriceRuleKindWholesale && params.CustomerId != 0 {
		return nil, ErrInvalidResellerPriceRule
	}
	if params.ExpectedVersion != nil && *params.ExpectedVersion < 0 {
		return nil, ErrInvalidResellerPriceRule
	}
	if params.RequiredSubagentMemberId != nil && (params.Kind != ResellerPriceRuleKindRetail || params.CustomerId < 1 || *params.RequiredSubagentMemberId < 1) {
		return nil, ErrInvalidResellerPriceRule
	}
	if params.EffectiveAt <= 0 {
		params.EffectiveAt = common.GetTimestamp()
	}

	var created ResellerPriceRule
	err := DB.Transaction(func(tx *gorm.DB) error {
		var reseller Reseller
		// Serialize every pricing write for a reseller. Without this lock, a
		// concurrent retail decrease and wholesale increase can both validate
		// against stale counterparts and commit a negative-margin configuration.
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").Where("id = ?", params.ResellerId).Take(&reseller).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrResellerNotFound
			}
			return err
		}
		if params.CustomerId > 0 {
			var count int64
			customerQuery := tx.Model(&ResellerCustomer{}).
				Where("id = ? AND reseller_id = ?", params.CustomerId, params.ResellerId)
			if params.RequiredSubagentMemberId != nil {
				customerQuery = customerQuery.Where("subagent_member_id = ?", *params.RequiredSubagentMemberId)
			}
			if err := customerQuery.
				Count(&count).Error; err != nil {
				return err
			}
			if count == 0 {
				return ErrResellerCustomerNotFound
			}
		}

		var latest ResellerPriceRule
		err := tx.Where("reseller_id = ? AND kind = ? AND model_name = ? AND customer_id = ?",
			params.ResellerId, params.Kind, params.ModelName, params.CustomerId).
			Order("version DESC").Order("id DESC").Take(&latest).Error
		currentVersion := 0
		if err == nil {
			currentVersion = latest.Version
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if params.ExpectedVersion != nil && *params.ExpectedVersion != currentVersion {
			return ErrResellerPriceRuleVersionConflict
		}
		if currentVersion == int(^uint(0)>>1) {
			return ErrResellerPriceRuleVersionConflict
		}
		if err := validateResellerPriceMargin(tx, params); err != nil {
			return err
		}
		created = ResellerPriceRule{
			ResellerId: params.ResellerId, Kind: params.Kind, ModelName: params.ModelName,
			CustomerId: params.CustomerId, Version: currentVersion + 1,
			MultiplierPPM: params.MultiplierPPM, Enabled: params.Enabled,
			EffectiveAt: params.EffectiveAt, CreatedBy: params.CreatedBy,
		}
		if err := tx.Create(&created).Error; err != nil {
			// PostgreSQL aborts the transaction after a unique violation, so do not
			// issue a diagnostic query here. Classify the driver error directly.
			if isResellerUniqueConstraintError(err) {
				return ErrResellerPriceRuleVersionConflict
			}
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &created, nil
}

func ListResellerPriceRules(resellerId int, customerId *int) ([]ResellerPriceRuleRecord, error) {
	if resellerId < 1 || customerId != nil && *customerId < 1 {
		return nil, ErrInvalidResellerPriceRule
	}
	if customerId != nil {
		var count int64
		if err := DB.Model(&ResellerCustomer{}).
			Where("id = ? AND reseller_id = ?", *customerId, resellerId).Count(&count).Error; err != nil {
			return nil, err
		}
		if count == 0 {
			return nil, ErrResellerCustomerNotFound
		}
	}

	var rules []ResellerPriceRule
	query := DB.Where("reseller_id = ?", resellerId)
	if customerId != nil {
		query = query.Where("kind = ? OR (kind = ? AND customer_id IN ?)",
			ResellerPriceRuleKindWholesale, ResellerPriceRuleKindRetail, []int{0, *customerId})
	}
	if err := query.Order("kind ASC").Order("model_name ASC").Order("customer_id ASC").
		Order("version DESC").Order("id DESC").Find(&rules).Error; err != nil {
		return nil, err
	}

	return latestResellerPriceRuleRecords(rules), nil
}

func ListResellerSubagentPriceRules(resellerId int, memberId int, customerId *int) ([]ResellerPriceRuleRecord, error) {
	if resellerId < 1 || memberId < 1 || customerId != nil && *customerId < 1 {
		return nil, ErrInvalidResellerPriceRule
	}
	assignedCustomerIds := make([]int, 0)
	query := DB.Model(&ResellerCustomer{}).
		Where("reseller_id = ? AND subagent_member_id = ?", resellerId, memberId)
	if customerId != nil {
		query = query.Where("id = ?", *customerId)
	}
	if err := query.Pluck("id", &assignedCustomerIds).Error; err != nil {
		return nil, err
	}
	if customerId != nil && len(assignedCustomerIds) == 0 {
		return nil, ErrResellerCustomerNotFound
	}
	retailCustomerIds := append([]int{0}, assignedCustomerIds...)
	var rules []ResellerPriceRule
	if err := DB.Where("reseller_id = ? AND (kind = ? OR (kind = ? AND customer_id IN ?))",
		resellerId, ResellerPriceRuleKindWholesale, ResellerPriceRuleKindRetail, retailCustomerIds).
		Order("kind ASC").Order("model_name ASC").Order("customer_id ASC").
		Order("version DESC").Order("id DESC").Find(&rules).Error; err != nil {
		return nil, err
	}
	return latestResellerPriceRuleRecords(rules), nil
}

func latestResellerPriceRuleRecords(rules []ResellerPriceRule) []ResellerPriceRuleRecord {
	records := make([]ResellerPriceRuleRecord, 0, len(rules))
	seen := make(map[string]struct{}, len(rules))
	for _, rule := range rules {
		key := fmt.Sprintf("%s\x00%s\x00%d", rule.Kind, rule.ModelName, rule.CustomerId)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		records = append(records, rule.Record())
	}
	return records
}

func ResolveResellerBillingCustomer(userId int) (*ResellerBillingCustomer, error) {
	if userId < 1 {
		return nil, nil
	}
	var matches []ResellerBillingCustomer
	err := DB.Table("reseller_customers AS customers").
		Select("customers.reseller_id, customers.id AS customer_id, user_ssos.user_id").
		Joins("JOIN user_ssos ON user_ssos.sso_sub = customers.subject AND user_ssos.user_id = ?", userId).
		Joins("JOIN resellers ON resellers.id = customers.reseller_id AND resellers.status = ?", ResellerStatusActive).
		Where("customers.status = ?", ResellerCustomerStatusActive).
		Order("customers.id ASC").Limit(2).Scan(&matches).Error
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return nil, nil
	}
	if len(matches) > 1 {
		return nil, ErrResellerBillingIdentityConflict
	}
	return &matches[0], nil
}

func effectiveResellerPriceRuleOn(db *gorm.DB, resellerId int, kind string, modelName string, customerId int, now int64) (*ResellerPriceRule, error) {
	var rule ResellerPriceRule
	err := db.Where("reseller_id = ? AND kind = ? AND model_name = ? AND customer_id = ? AND effective_at <= ?",
		resellerId, kind, modelName, customerId, now).
		Order("version DESC").Order("id DESC").Take(&rule).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !rule.Enabled {
		return nil, nil
	}
	return &rule, nil
}

func effectiveResellerPriceRule(resellerId int, kind string, modelName string, customerId int, now int64) (*ResellerPriceRule, error) {
	return effectiveResellerPriceRuleOn(DB, resellerId, kind, modelName, customerId, now)
}

func effectiveResellerMultiplierOn(db *gorm.DB, resellerId int, kind string, modelName string, customerId int, now int64) (int64, error) {
	rule, err := effectiveResellerPriceRuleOn(db, resellerId, kind, modelName, customerId, now)
	if err != nil {
		return 0, err
	}
	if rule == nil {
		return ResellerDefaultMultiplierPPM, nil
	}
	return rule.MultiplierPPM, nil
}

func validateResellerPriceMargin(tx *gorm.DB, params CreateResellerPriceRuleParams) error {
	if params.Kind == ResellerPriceRuleKindRetail {
		wholesale, err := effectiveResellerMultiplierOn(tx, params.ResellerId, ResellerPriceRuleKindWholesale, params.ModelName, 0, params.EffectiveAt)
		if err != nil {
			return err
		}
		if params.MultiplierPPM < wholesale {
			return ErrResellerPriceMarginConflict
		}
		return nil
	}

	defaultRetail, err := effectiveResellerMultiplierOn(tx, params.ResellerId, ResellerPriceRuleKindRetail, params.ModelName, 0, params.EffectiveAt)
	if err != nil {
		return err
	}
	if params.MultiplierPPM > defaultRetail {
		return ErrResellerPriceMarginConflict
	}
	var customerIds []int
	if err := tx.Model(&ResellerPriceRule{}).
		Select("reseller_price_rules.customer_id").
		Joins("JOIN reseller_customers ON reseller_customers.id = reseller_price_rules.customer_id AND reseller_customers.reseller_id = ?", params.ResellerId).
		Where("reseller_price_rules.reseller_id = ? AND reseller_price_rules.kind = ? AND reseller_price_rules.model_name = ? AND reseller_price_rules.customer_id > 0 AND reseller_price_rules.effective_at <= ?",
			params.ResellerId, ResellerPriceRuleKindRetail, params.ModelName, params.EffectiveAt).
		Distinct().Pluck("reseller_price_rules.customer_id", &customerIds).Error; err != nil {
		return err
	}
	for _, customerId := range customerIds {
		retail, err := effectiveResellerMultiplierOn(tx, params.ResellerId, ResellerPriceRuleKindRetail, params.ModelName, customerId, params.EffectiveAt)
		if err != nil {
			return err
		}
		if params.MultiplierPPM > retail {
			return ErrResellerPriceMarginConflict
		}
	}
	return nil
}

func ResolveResellerWholesalePrice(resellerId int, modelName string, now int64) (ResellerEffectivePrice, error) {
	rule, err := effectiveResellerPriceRule(resellerId, ResellerPriceRuleKindWholesale, modelName, 0, now)
	if err != nil {
		return ResellerEffectivePrice{}, err
	}
	if rule == nil {
		return ResellerEffectivePrice{MultiplierPPM: ResellerDefaultMultiplierPPM, Source: "default"}, nil
	}
	return ResellerEffectivePrice{RuleId: rule.Id, RuleVersion: rule.Version, MultiplierPPM: rule.MultiplierPPM, Source: "rule"}, nil
}

func ResolveResellerRetailPrice(resellerId int, customerId int, modelName string, now int64) (ResellerEffectivePrice, error) {
	if customerId > 0 {
		rule, err := effectiveResellerPriceRule(resellerId, ResellerPriceRuleKindRetail, modelName, customerId, now)
		if err != nil {
			return ResellerEffectivePrice{}, err
		}
		if rule != nil {
			return ResellerEffectivePrice{RuleId: rule.Id, RuleVersion: rule.Version, MultiplierPPM: rule.MultiplierPPM, Source: "customer"}, nil
		}
	}
	rule, err := effectiveResellerPriceRule(resellerId, ResellerPriceRuleKindRetail, modelName, 0, now)
	if err != nil {
		return ResellerEffectivePrice{}, err
	}
	if rule == nil {
		return ResellerEffectivePrice{MultiplierPPM: ResellerDefaultMultiplierPPM, Source: "default"}, nil
	}
	return ResellerEffectivePrice{RuleId: rule.Id, RuleVersion: rule.Version, MultiplierPPM: rule.MultiplierPPM, Source: "reseller"}, nil
}

// ProjectResellerCustomerPricing returns a customer-price projection without
// mutating the globally cached pricing slice. No reseller identifiers, rule
// metadata, or multipliers are added to the customer-facing model.
func ProjectResellerCustomerPricing(userId int, pricing []Pricing) ([]Pricing, bool, error) {
	customer, err := ResolveResellerBillingCustomer(userId)
	if err != nil || customer == nil {
		return pricing, false, err
	}
	if len(pricing) == 0 {
		return []Pricing{}, true, nil
	}
	modelNames := make([]string, 0, len(pricing))
	for _, item := range pricing {
		modelNames = append(modelNames, item.ModelName)
	}
	var rules []ResellerPriceRule
	err = DB.Where("reseller_id = ? AND kind = ? AND model_name IN ? AND customer_id IN ? AND effective_at <= ?",
		customer.ResellerId, ResellerPriceRuleKindRetail, modelNames, []int{0, customer.CustomerId}, common.GetTimestamp()).
		Order("model_name ASC").Order("customer_id DESC").Order("version DESC").Order("id DESC").Find(&rules).Error
	if err != nil {
		return nil, false, err
	}
	type scopeKey struct {
		modelName  string
		customerId int
	}
	latest := make(map[scopeKey]*ResellerPriceRule, len(rules))
	for i := range rules {
		key := scopeKey{modelName: rules[i].ModelName, customerId: rules[i].CustomerId}
		if _, seen := latest[key]; seen {
			continue
		}
		if rules[i].Enabled {
			latest[key] = &rules[i]
		} else {
			latest[key] = nil
		}
	}

	projected := make([]Pricing, len(pricing))
	copy(projected, pricing)
	for i := range projected {
		multiplierPPM := ResellerDefaultMultiplierPPM
		if rule := latest[scopeKey{modelName: projected[i].ModelName, customerId: customer.CustomerId}]; rule != nil {
			multiplierPPM = rule.MultiplierPPM
		} else if rule := latest[scopeKey{modelName: projected[i].ModelName, customerId: 0}]; rule != nil {
			multiplierPPM = rule.MultiplierPPM
		}
		if multiplierPPM == ResellerDefaultMultiplierPPM {
			continue
		}
		multiplier := float64(multiplierPPM) / float64(ResellerDefaultMultiplierPPM)
		if projected[i].QuotaType == 1 {
			projected[i].ModelPrice *= multiplier
		} else {
			projected[i].ModelRatio *= multiplier
		}
		if projected[i].BillingMode == "tiered_expr" && projected[i].BillingExpr != "" {
			projected[i].BillingExpr = projectResellerBillingExpression(projected[i].BillingExpr, multiplierPPM)
		}
	}
	return projected, true, nil
}

func projectResellerBillingExpression(expression string, multiplierPPM int64) string {
	if expression == "" || multiplierPPM == ResellerDefaultMultiplierPPM {
		return expression
	}
	versionPrefix := ""
	bodyAndRules := expression
	if strings.HasPrefix(bodyAndRules, "v1:") {
		versionPrefix = "v1:"
		bodyAndRules = strings.TrimPrefix(bodyAndRules, versionPrefix)
	}
	parts := strings.SplitN(bodyAndRules, "|||", 2)
	projected := versionPrefix + "(" + parts[0] + ") * " + FormatResellerMultiplier(multiplierPPM)
	if len(parts) == 2 {
		projected += "|||" + parts[1]
	}
	// Rules are stored only after validation, so this is defensive. Returning an
	// empty expression is safer than leaking the platform base expression.
	mainExpression := strings.SplitN(projected, "|||", 2)[0]
	if _, err := billingexpr.CompileFromCache(mainExpression); err != nil {
		return ""
	}
	return projected
}

func PrepareResellerRequestSettlement(settlement *ResellerRequestSettlement) (*ResellerRequestSettlement, bool, error) {
	if settlement == nil || settlement.RequestId == "" || len(settlement.RequestId) > 64 ||
		settlement.ResellerId < 1 || settlement.CustomerId < 1 || settlement.UserId < 1 ||
		strings.TrimSpace(settlement.ModelName) == "" || len(settlement.ModelName) > 191 ||
		settlement.WholesaleMultiplierPPM <= 0 || settlement.RetailMultiplierPPM <= 0 ||
		settlement.RetailMultiplierPPM < settlement.WholesaleMultiplierPPM ||
		settlement.EstimatedBaseQuota < 0 || settlement.EstimatedCustomerQuota < 0 || settlement.EstimatedWholesaleQuota < 0 ||
		settlement.EstimatedCustomerQuota < settlement.EstimatedWholesaleQuota {
		return nil, false, ErrResellerSettlementConflict
	}
	settlement.Status = ResellerSettlementStatusReserved
	if err := DB.Create(settlement).Error; err == nil {
		return settlement, true, nil
	}
	existing, getErr := GetResellerRequestSettlement(settlement.RequestId)
	if getErr != nil {
		return nil, false, getErr
	}
	if existing.ResellerId != settlement.ResellerId || existing.CustomerId != settlement.CustomerId ||
		existing.UserId != settlement.UserId || existing.ModelName != settlement.ModelName ||
		existing.WholesaleRuleId != settlement.WholesaleRuleId || existing.WholesaleRuleVersion != settlement.WholesaleRuleVersion ||
		existing.WholesaleMultiplierPPM != settlement.WholesaleMultiplierPPM ||
		existing.RetailRuleId != settlement.RetailRuleId || existing.RetailRuleVersion != settlement.RetailRuleVersion ||
		existing.RetailMultiplierPPM != settlement.RetailMultiplierPPM ||
		existing.EstimatedBaseQuota != settlement.EstimatedBaseQuota ||
		existing.EstimatedCustomerQuota != settlement.EstimatedCustomerQuota ||
		existing.EstimatedWholesaleQuota != settlement.EstimatedWholesaleQuota ||
		existing.Status == ResellerSettlementStatusFailed || existing.Status == ResellerSettlementStatusRefunded {
		return nil, false, ErrResellerSettlementConflict
	}
	return existing, false, nil
}

func GetResellerRequestSettlement(requestId string) (*ResellerRequestSettlement, error) {
	var settlement ResellerRequestSettlement
	if err := DB.Where("request_id = ?", requestId).Take(&settlement).Error; err != nil {
		return nil, err
	}
	return &settlement, nil
}

func MarkResellerSettlementFailed(requestId string) error {
	result := DB.Model(&ResellerRequestSettlement{}).
		Where("request_id = ? AND status = ?", requestId, ResellerSettlementStatusReserved).
		Update("status", ResellerSettlementStatusFailed)
	return result.Error
}

func BeginResellerSettlement(requestId string, actualBaseQuota int64, actualCustomerQuota int64, actualWholesaleQuota int64, usageJSON string) error {
	if actualBaseQuota < 0 || actualCustomerQuota < 0 || actualWholesaleQuota < 0 || actualCustomerQuota < actualWholesaleQuota {
		return ErrResellerSettlementConflict
	}
	updates := map[string]any{
		"actual_base_quota": actualBaseQuota, "actual_customer_quota": actualCustomerQuota,
		"actual_wholesale_quota": actualWholesaleQuota, "usage_json": usageJSON,
		"status": ResellerSettlementStatusSettling,
	}
	result := DB.Model(&ResellerRequestSettlement{}).
		Where("request_id = ? AND status = ?", requestId, ResellerSettlementStatusReserved).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 {
		return nil
	}
	existing, err := GetResellerRequestSettlement(requestId)
	if err != nil {
		return err
	}
	if (existing.Status == ResellerSettlementStatusSettling || existing.Status == ResellerSettlementStatusSettled) &&
		existing.ActualBaseQuota == actualBaseQuota && existing.ActualCustomerQuota == actualCustomerQuota &&
		existing.ActualWholesaleQuota == actualWholesaleQuota {
		return nil
	}
	return ErrResellerSettlementConflict
}

func CompleteResellerSettlement(requestId string) error {
	now := common.GetTimestamp()
	result := DB.Model(&ResellerRequestSettlement{}).
		Where("request_id = ? AND status = ?", requestId, ResellerSettlementStatusSettling).
		Updates(map[string]any{"status": ResellerSettlementStatusSettled, "settled_at": now})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 {
		return nil
	}
	existing, err := GetResellerRequestSettlement(requestId)
	if err != nil {
		return err
	}
	if existing.Status == ResellerSettlementStatusSettled {
		return nil
	}
	return ErrResellerSettlementConflict
}

// UpdateSettledResellerSettlementActual applies an asynchronous task's later
// actual usage using the multipliers frozen on the original request snapshot.
func UpdateSettledResellerSettlementActual(requestId string, actualBaseQuota int64, actualCustomerQuota int64, actualWholesaleQuota int64, usageJSON string) error {
	if actualBaseQuota < 0 || actualCustomerQuota < 0 || actualWholesaleQuota < 0 || actualCustomerQuota < actualWholesaleQuota {
		return ErrResellerSettlementConflict
	}
	updates := map[string]any{
		"actual_base_quota": actualBaseQuota, "actual_customer_quota": actualCustomerQuota,
		"actual_wholesale_quota": actualWholesaleQuota,
	}
	if usageJSON != "" {
		updates["usage_json"] = usageJSON
	}
	result := DB.Model(&ResellerRequestSettlement{}).
		Where("request_id = ? AND status = ?", requestId, ResellerSettlementStatusSettled).
		Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 {
		return nil
	}
	existing, err := GetResellerRequestSettlement(requestId)
	if err != nil {
		return err
	}
	if existing.Status == ResellerSettlementStatusSettled && existing.ActualBaseQuota == actualBaseQuota &&
		existing.ActualCustomerQuota == actualCustomerQuota && existing.ActualWholesaleQuota == actualWholesaleQuota &&
		(usageJSON == "" || existing.UsageJSON == usageJSON) {
		return nil
	}
	return ErrResellerSettlementConflict
}

func RefundResellerSettlement(requestId string) error {
	now := common.GetTimestamp()
	result := DB.Model(&ResellerRequestSettlement{}).
		Where("request_id = ? AND status IN ?", requestId, []string{ResellerSettlementStatusReserved, ResellerSettlementStatusSettling, ResellerSettlementStatusSettled}).
		Updates(map[string]any{"status": ResellerSettlementStatusRefunded, "refunded_at": now})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 {
		return nil
	}
	existing, err := GetResellerRequestSettlement(requestId)
	if err != nil {
		return err
	}
	if existing.Status == ResellerSettlementStatusRefunded {
		return nil
	}
	return ErrResellerSettlementConflict
}

func RestoreResellerSettlementAfterRefundFailure(requestId string) error {
	result := DB.Model(&ResellerRequestSettlement{}).
		Where("request_id = ? AND status = ?", requestId, ResellerSettlementStatusRefunded).
		Updates(map[string]any{"status": ResellerSettlementStatusSettled, "refunded_at": int64(0)})
	return result.Error
}
