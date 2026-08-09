package model

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupResellerPricingTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	originalDB := DB
	originalMainType := common.MainDatabaseType()
	common.SetDatabaseTypes(common.DatabaseTypeSQLite, common.LogDatabaseType())
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(
		&User{}, &UserSSO{}, &Reseller{}, &ResellerCustomer{},
		&ResellerPriceRule{}, &ResellerRequestSettlement{},
	))
	DB = db
	t.Cleanup(func() {
		DB = originalDB
		common.SetDatabaseTypes(originalMainType, common.LogDatabaseType())
		sqlDB, dbErr := db.DB()
		if dbErr == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func seedResellerPricingIdentity(t *testing.T, db *gorm.DB) (Reseller, ResellerCustomer, User) {
	t.Helper()
	reseller := Reseller{Name: "Pricing Agency", Status: ResellerStatusActive}
	require.NoError(t, db.Create(&reseller).Error)
	user := User{Username: "pricing-customer", Password: "test", Status: common.UserStatusEnabled, Quota: 10_000, AffCode: "pricing-customer-aff"}
	require.NoError(t, db.Create(&user).Error)
	require.NoError(t, db.Create(&UserSSO{SSOSub: "pricing-subject", UserId: user.Id}).Error)
	customer := ResellerCustomer{ResellerId: reseller.Id, Subject: "pricing-subject", Status: ResellerCustomerStatusActive}
	require.NoError(t, db.Create(&customer).Error)
	return reseller, customer, user
}

func createPricingRuleForTest(t *testing.T, params CreateResellerPriceRuleParams) *ResellerPriceRule {
	t.Helper()
	if params.CreatedBy == "" {
		params.CreatedBy = "test"
	}
	if params.EffectiveAt == 0 {
		params.EffectiveAt = common.GetTimestamp()
	}
	params.Enabled = true
	rule, err := CreateResellerPriceRule(params)
	require.NoError(t, err)
	return rule
}

func TestResellerMultiplierPrecisionAndOverflow(t *testing.T) {
	tests := []struct {
		value string
		want  int64
	}{
		{value: "1", want: 1_000_000},
		{value: "1.25", want: 1_250_000},
		{value: "1.2345674", want: 1_234_567},
		{value: "1.2345675", want: 1_234_568},
		{value: "0.0000005", want: 1},
	}
	for _, test := range tests {
		t.Run(test.value, func(t *testing.T) {
			got, err := ParseResellerMultiplier(test.value)
			require.NoError(t, err)
			assert.Equal(t, test.want, got)
			assert.Equal(t, got, mustParseMultiplier(t, FormatResellerMultiplier(got)))
		})
	}
	for _, invalid := range []string{"", "0", "-1", "+1", "1e3", ".5", "1."} {
		_, err := ParseResellerMultiplier(invalid)
		assert.ErrorIs(t, err, ErrInvalidResellerPriceRule, invalid)
	}

	quota, err := ApplyResellerMultiplier(3, 1_500_000)
	require.NoError(t, err)
	assert.Equal(t, int64(5), quota, "4.5 must round half up")
	_, err = ApplyResellerMultiplier(math.MaxInt64, math.MaxInt64)
	assert.ErrorIs(t, err, ErrResellerQuotaOverflow)
}

func mustParseMultiplier(t *testing.T, value string) int64 {
	t.Helper()
	parsed, err := ParseResellerMultiplier(value)
	require.NoError(t, err)
	return parsed
}

func TestResellerPriceRuleResolutionVersionAndMargin(t *testing.T) {
	db := setupResellerPricingTestDB(t)
	reseller, customer, _ := seedResellerPricingIdentity(t, db)
	otherReseller := Reseller{Name: "Other", Status: ResellerStatusActive}
	require.NoError(t, db.Create(&otherReseller).Error)
	otherCustomer := ResellerCustomer{ResellerId: otherReseller.Id, Subject: "other-subject", Status: ResellerCustomerStatusActive}
	require.NoError(t, db.Create(&otherCustomer).Error)

	zero := 0
	wholesale := createPricingRuleForTest(t, CreateResellerPriceRuleParams{
		ResellerId: reseller.Id, Kind: ResellerPriceRuleKindWholesale, ModelName: "model-a",
		MultiplierPPM: 800_000, ExpectedVersion: &zero,
	})
	defaultRetail := createPricingRuleForTest(t, CreateResellerPriceRuleParams{
		ResellerId: reseller.Id, Kind: ResellerPriceRuleKindRetail, ModelName: "model-a",
		MultiplierPPM: 1_200_000, ExpectedVersion: &zero,
	})
	exactRetail := createPricingRuleForTest(t, CreateResellerPriceRuleParams{
		ResellerId: reseller.Id, Kind: ResellerPriceRuleKindRetail, ModelName: "model-a", CustomerId: customer.Id,
		MultiplierPPM: 1_500_000, ExpectedVersion: &zero,
	})
	lowMarginCustomer := ResellerCustomer{ResellerId: reseller.Id, Subject: "low-margin-subject", Status: ResellerCustomerStatusActive}
	require.NoError(t, db.Create(&lowMarginCustomer).Error)
	createPricingRuleForTest(t, CreateResellerPriceRuleParams{
		ResellerId: reseller.Id, Kind: ResellerPriceRuleKindRetail, ModelName: "model-a", CustomerId: lowMarginCustomer.Id,
		MultiplierPPM: 900_000, ExpectedVersion: &zero,
	})

	resolvedWholesale, err := ResolveResellerWholesalePrice(reseller.Id, "model-a", common.GetTimestamp())
	require.NoError(t, err)
	assert.Equal(t, wholesale.Id, resolvedWholesale.RuleId)
	assert.Equal(t, int64(800_000), resolvedWholesale.MultiplierPPM)
	resolvedExact, err := ResolveResellerRetailPrice(reseller.Id, customer.Id, "model-a", common.GetTimestamp())
	require.NoError(t, err)
	assert.Equal(t, exactRetail.Id, resolvedExact.RuleId)
	assert.Equal(t, "customer", resolvedExact.Source)
	resolvedDefault, err := ResolveResellerRetailPrice(reseller.Id, 999, "model-a", common.GetTimestamp())
	require.NoError(t, err)
	assert.Equal(t, defaultRetail.Id, resolvedDefault.RuleId)
	assert.Equal(t, "reseller", resolvedDefault.Source)
	resolvedFallback, err := ResolveResellerRetailPrice(reseller.Id, customer.Id, "model-b", common.GetTimestamp())
	require.NoError(t, err)
	assert.Equal(t, ResellerDefaultMultiplierPPM, resolvedFallback.MultiplierPPM)
	assert.Equal(t, "default", resolvedFallback.Source)

	_, err = CreateResellerPriceRule(CreateResellerPriceRuleParams{
		ResellerId: reseller.Id, Kind: ResellerPriceRuleKindRetail, ModelName: "model-a",
		MultiplierPPM: 1_300_000, ExpectedVersion: &zero, Enabled: true,
		EffectiveAt: common.GetTimestamp(), CreatedBy: "stale-writer",
	})
	assert.ErrorIs(t, err, ErrResellerPriceRuleVersionConflict)
	one := 1
	_, err = CreateResellerPriceRule(CreateResellerPriceRuleParams{
		ResellerId: reseller.Id, Kind: ResellerPriceRuleKindRetail, ModelName: "model-a",
		MultiplierPPM: 700_000, ExpectedVersion: &one, Enabled: true,
		EffectiveAt: common.GetTimestamp(), CreatedBy: "negative-margin",
	})
	assert.ErrorIs(t, err, ErrResellerPriceMarginConflict)
	_, err = CreateResellerPriceRule(CreateResellerPriceRuleParams{
		ResellerId: reseller.Id, Kind: ResellerPriceRuleKindWholesale, ModelName: "model-a",
		MultiplierPPM: 1_000_000, ExpectedVersion: &one, Enabled: true,
		EffectiveAt: common.GetTimestamp(), CreatedBy: "blocked-by-current-customer",
	})
	assert.ErrorIs(t, err, ErrResellerPriceMarginConflict)
	require.NoError(t, db.Model(&lowMarginCustomer).Update("reseller_id", otherReseller.Id).Error)
	newWholesale, err := CreateResellerPriceRule(CreateResellerPriceRuleParams{
		ResellerId: reseller.Id, Kind: ResellerPriceRuleKindWholesale, ModelName: "model-a",
		MultiplierPPM: 1_000_000, ExpectedVersion: &one, Enabled: true,
		EffectiveAt: common.GetTimestamp(), CreatedBy: "orphan-no-longer-blocks",
	})
	require.NoError(t, err)
	assert.Equal(t, 2, newWholesale.Version)
	two := 2
	_, err = CreateResellerPriceRule(CreateResellerPriceRuleParams{
		ResellerId: reseller.Id, Kind: ResellerPriceRuleKindWholesale, ModelName: "model-a",
		MultiplierPPM: 1_300_000, ExpectedVersion: &two, Enabled: true,
		EffectiveAt: common.GetTimestamp(), CreatedBy: "negative-margin",
	})
	assert.ErrorIs(t, err, ErrResellerPriceMarginConflict)
	_, err = CreateResellerPriceRule(CreateResellerPriceRuleParams{
		ResellerId: reseller.Id, Kind: ResellerPriceRuleKindRetail, ModelName: "model-a", CustomerId: otherCustomer.Id,
		MultiplierPPM: 1_500_000, ExpectedVersion: &zero, Enabled: true,
		EffectiveAt: common.GetTimestamp(), CreatedBy: "cross-tenant",
	})
	assert.ErrorIs(t, err, ErrResellerCustomerNotFound)
}

func TestResolveResellerBillingCustomerUsesActiveSSOOwnership(t *testing.T) {
	db := setupResellerPricingTestDB(t)
	reseller, customer, user := seedResellerPricingIdentity(t, db)

	resolved, err := ResolveResellerBillingCustomer(user.Id)
	require.NoError(t, err)
	require.NotNil(t, resolved)
	assert.Equal(t, reseller.Id, resolved.ResellerId)
	assert.Equal(t, customer.Id, resolved.CustomerId)

	require.NoError(t, db.Model(&customer).Update("status", ResellerCustomerStatusSuspend).Error)
	resolved, err = ResolveResellerBillingCustomer(user.Id)
	require.NoError(t, err)
	assert.Nil(t, resolved)
}

func TestResellerSettlementLifecycleIsIdempotentAndFrozen(t *testing.T) {
	db := setupResellerPricingTestDB(t)
	reseller, customer, user := seedResellerPricingIdentity(t, db)
	_, _, err := PrepareResellerRequestSettlement(&ResellerRequestSettlement{
		RequestId: "negative-margin-request", ResellerId: reseller.Id, CustomerId: customer.Id, UserId: user.Id,
		ModelName: "model-a", WholesaleMultiplierPPM: 1_200_000, RetailMultiplierPPM: 800_000,
		EstimatedBaseQuota: 100, EstimatedCustomerQuota: 80, EstimatedWholesaleQuota: 120,
	})
	assert.ErrorIs(t, err, ErrResellerSettlementConflict)
	snapshot := &ResellerRequestSettlement{
		RequestId: "settlement-request_123", ResellerId: reseller.Id, CustomerId: customer.Id, UserId: user.Id,
		ModelName: "model-a", WholesaleMultiplierPPM: 800_000, RetailMultiplierPPM: 1_200_000,
		WholesaleRuleId: 11, WholesaleRuleVersion: 1, RetailRuleId: 12, RetailRuleVersion: 2,
		EstimatedBaseQuota: 100, EstimatedCustomerQuota: 120, EstimatedWholesaleQuota: 80,
	}
	created, wasCreated, err := PrepareResellerRequestSettlement(snapshot)
	require.NoError(t, err)
	assert.True(t, wasCreated)
	assert.Equal(t, ResellerSettlementStatusReserved, created.Status)
	replayed, wasCreated, err := PrepareResellerRequestSettlement(&ResellerRequestSettlement{
		RequestId: "settlement-request_123", ResellerId: reseller.Id, CustomerId: customer.Id, UserId: user.Id,
		ModelName: "model-a", WholesaleMultiplierPPM: 800_000, RetailMultiplierPPM: 1_200_000,
		WholesaleRuleId: 11, WholesaleRuleVersion: 1, RetailRuleId: 12, RetailRuleVersion: 2,
		EstimatedBaseQuota: 100, EstimatedCustomerQuota: 120, EstimatedWholesaleQuota: 80,
	})
	require.NoError(t, err)
	assert.False(t, wasCreated)
	assert.Equal(t, created.Id, replayed.Id)

	assert.ErrorIs(t, BeginResellerSettlement(snapshot.RequestId, 125, 90, 100, ""), ErrResellerSettlementConflict)
	require.NoError(t, BeginResellerSettlement(snapshot.RequestId, 125, 150, 100, `{"total_tokens":125}`))
	require.NoError(t, BeginResellerSettlement(snapshot.RequestId, 125, 150, 100, `{"total_tokens":125}`))
	assert.ErrorIs(t, BeginResellerSettlement(snapshot.RequestId, 126, 151, 101, ""), ErrResellerSettlementConflict)
	require.NoError(t, CompleteResellerSettlement(snapshot.RequestId))
	require.NoError(t, CompleteResellerSettlement(snapshot.RequestId))
	assert.ErrorIs(t, UpdateSettledResellerSettlementActual(snapshot.RequestId, 125, 90, 100, ""), ErrResellerSettlementConflict)
	require.NoError(t, RefundResellerSettlement(snapshot.RequestId))
	require.NoError(t, RefundResellerSettlement(snapshot.RequestId))
	stored, err := GetResellerRequestSettlement(snapshot.RequestId)
	require.NoError(t, err)
	assert.Equal(t, ResellerSettlementStatusRefunded, stored.Status)
	assert.Equal(t, int64(800_000), stored.WholesaleMultiplierPPM)
	assert.Equal(t, int64(1_200_000), stored.RetailMultiplierPPM)
	_, _, err = PrepareResellerRequestSettlement(snapshot)
	assert.ErrorIs(t, err, ErrResellerSettlementConflict)
}

func TestProjectResellerCustomerPricingCopiesAndProjectsTieredExpression(t *testing.T) {
	db := setupResellerPricingTestDB(t)
	reseller, customer, user := seedResellerPricingIdentity(t, db)
	createPricingRuleForTest(t, CreateResellerPriceRuleParams{
		ResellerId: reseller.Id, Kind: ResellerPriceRuleKindRetail, ModelName: "tiered-model", CustomerId: customer.Id,
		MultiplierPPM: 1_250_000,
	})
	original := []Pricing{{
		ModelName: "tiered-model", QuotaType: 0, ModelRatio: 2,
		BillingMode: "tiered_expr", BillingExpr: `v1:tier("base", p * 2 + c * 10)|||when(header("x-fast") has "yes") * 2`,
	}}
	projected, assigned, err := ProjectResellerCustomerPricing(user.Id, original)
	require.NoError(t, err)
	assert.True(t, assigned)
	require.Len(t, projected, 1)
	assert.Equal(t, float64(2.5), projected[0].ModelRatio)
	assert.Equal(t, float64(2), original[0].ModelRatio, "global cache entry must remain unchanged")
	assert.Equal(t, `v1:tier("base", p * 2 + c * 10)|||when(header("x-fast") has "yes") * 2`, original[0].BillingExpr)
	assert.True(t, strings.HasPrefix(projected[0].BillingExpr, "v1:"))
	assert.True(t, strings.HasSuffix(projected[0].BillingExpr, `|||when(header("x-fast") has "yes") * 2`))
	mainExpression := strings.SplitN(projected[0].BillingExpr, "|||", 2)[0]
	_, err = billingexpr.CompileFromCache(mainExpression)
	require.NoError(t, err)

	unassigned := User{Username: "unassigned", Password: "test", Status: common.UserStatusEnabled, AffCode: "unassigned-aff"}
	require.NoError(t, db.Create(&unassigned).Error)
	unchanged, assigned, err := ProjectResellerCustomerPricing(unassigned.Id, original)
	require.NoError(t, err)
	assert.False(t, assigned)
	assert.Equal(t, original, unchanged)
}
