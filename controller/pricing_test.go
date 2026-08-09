package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type pricingResponse struct {
	Success bool            `json:"success"`
	Data    []model.Pricing `json:"data"`
}

func decodePricingResponse(t *testing.T, recorder *httptest.ResponseRecorder) []model.Pricing {
	t.Helper()

	require.Equal(t, http.StatusOK, recorder.Code)
	var payload pricingResponse
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &payload))
	require.True(t, payload.Success)
	return payload.Data
}

func pricingModelNames(pricing []model.Pricing) []string {
	names := make([]string, 0, len(pricing))
	for _, item := range pricing {
		names = append(names, item.ModelName)
	}
	return names
}

func TestGetPricingCanIncludeInaccessibleMoziaWalletModels(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&model.MoziaWalletBalance{},
		&model.MoziaWalletTransaction{},
		&model.MoziaModelQuotaPolicy{},
		&model.UserSubscription{},
	))
	t.Cleanup(model.InvalidatePricingCache)

	require.NoError(t, db.Create(&model.User{
		Id:       1004,
		Username: "pricing-wallet-user",
		Password: "password",
		Group:    "default",
		Quota:    20,
		Status:   common.UserStatusEnabled,
	}).Error)
	require.NoError(t, model.RecordMoziaInitialGiftQuota(1004, 20, "test", "gift-only"))
	require.NoError(t, db.Create(&model.Ability{
		Group:     "default",
		Model:     "paid-only-catalog-model",
		ChannelId: 1,
		Enabled:   true,
	}).Error)
	require.NoError(t, model.CreateMoziaModelQuotaPolicy(&model.MoziaModelQuotaPolicy{
		ModelPattern:   "paid-only-catalog-model",
		MatchType:      model.MoziaQuotaPolicyMatchExact,
		AllowedSources: model.MoziaWalletSourcePaid,
		ConsumeOrder:   model.MoziaQuotaPolicyConsumePaidFirst,
		Enabled:        true,
	}))
	model.InvalidatePricingCache()

	filteredRecorder := httptest.NewRecorder()
	filteredCtx, _ := gin.CreateTestContext(filteredRecorder)
	filteredCtx.Request = httptest.NewRequest(http.MethodGet, "/api/sso/pricing", nil)
	filteredCtx.Set("id", 1004)

	GetPricing(filteredCtx)

	require.NotContains(t, pricingModelNames(decodePricingResponse(t, filteredRecorder)), "paid-only-catalog-model")

	catalogRecorder := httptest.NewRecorder()
	catalogCtx, _ := gin.CreateTestContext(catalogRecorder)
	catalogCtx.Request = httptest.NewRequest(http.MethodGet, "/api/sso/pricing?include_inaccessible=true", nil)
	catalogCtx.Set("id", 1004)

	GetPricing(catalogCtx)

	pricingByName := pricingByModelName(decodePricingResponse(t, catalogRecorder))
	item, ok := pricingByName["paid-only-catalog-model"]
	require.True(t, ok)
	require.NotNil(t, item.Access)
	assert.False(t, item.Access.Available)
	assert.Equal(t, model.MoziaPricingAccessReasonRequiresPaidQuota, item.Access.Reason)
	assert.Equal(t, []string{model.MoziaWalletSourcePaid}, item.Access.RequiredSources)
	assert.True(t, item.Access.SubscriptionAllowed)
}

func TestGetPricingProjectsRetailWithoutLeakingResellerMetadataOrMutatingCache(t *testing.T) {
	withTieredBillingConfig(t, map[string]string{
		"m3-tiered-model": "tiered_expr",
	}, map[string]string{
		"m3-tiered-model": `v1:tier("base", p * 2 + c * 10)|||when(header("x-fast") has "yes") * 2`,
	})
	originalRatios := ratio_setting.ModelRatio2JSONString()
	originalPrices := ratio_setting.ModelPrice2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(originalRatios))
		require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(originalPrices))
		model.InvalidatePricingCache()
	})
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&model.MoziaWalletBalance{}, &model.MoziaWalletTransaction{},
		&model.MoziaModelQuotaPolicy{}, &model.UserSubscription{},
	))
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"m3-ratio-model":2,"m3-tiered-model":2}`))
	require.NoError(t, ratio_setting.UpdateModelPriceByJSONString(`{"m3-price-model":0.004}`))
	const userID = 1005
	require.NoError(t, db.Create(&model.User{
		Id: userID, Username: "reseller-pricing-user", Password: "password", Group: "default",
		Quota: 1_000, Status: common.UserStatusEnabled, AffCode: "reseller-pricing-user-aff",
	}).Error)
	subject := "reseller-pricing-subject"
	require.NoError(t, db.Create(&model.UserSSO{SSOSub: subject, UserId: userID}).Error)
	reseller := model.Reseller{Name: "Pricing Projection Agency", Status: model.ResellerStatusActive}
	require.NoError(t, db.Create(&reseller).Error)
	customer := model.ResellerCustomer{ResellerId: reseller.Id, Subject: subject, Status: model.ResellerCustomerStatusActive}
	require.NoError(t, db.Create(&customer).Error)
	models := []string{"m3-ratio-model", "m3-price-model", "m3-tiered-model"}
	for index, modelName := range models {
		require.NoError(t, db.Create(&model.Ability{Group: "default", Model: modelName, ChannelId: index + 1, Enabled: true}).Error)
		zero := 0
		_, err := model.CreateResellerPriceRule(model.CreateResellerPriceRuleParams{
			ResellerId: reseller.Id, Kind: model.ResellerPriceRuleKindRetail, ModelName: modelName,
			CustomerId: customer.Id, MultiplierPPM: 1_500_000, ExpectedVersion: &zero,
			Enabled: true, EffectiveAt: common.GetTimestamp(), CreatedBy: "test",
		})
		require.NoError(t, err)
	}
	model.InvalidatePricingCache()
	globalBefore := pricingByModelName(model.GetPricing())

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/sso/pricing", nil)
	ctx.Set("id", userID)
	GetPricing(ctx)

	projected := pricingByModelName(decodePricingResponse(t, recorder))
	assert.InDelta(t, 3.0, projected["m3-ratio-model"].ModelRatio, 0.000001)
	assert.InDelta(t, 0.006, projected["m3-price-model"].ModelPrice, 0.000001)
	assert.InDelta(t, 3.0, projected["m3-tiered-model"].ModelRatio, 0.000001)
	assert.True(t, strings.HasPrefix(projected["m3-tiered-model"].BillingExpr, "v1:"))
	assert.True(t, strings.Contains(projected["m3-tiered-model"].BillingExpr, ") * 1.5|||"))

	body := recorder.Body.String()
	for _, forbidden := range []string{`"wholesale`, `"retail_multiplier"`, `"reseller_id"`, `"customer_id"`, `"rule_id"`, `"settlement"`} {
		assert.NotContains(t, body, forbidden)
	}
	globalAfter := pricingByModelName(model.GetPricing())
	assert.Equal(t, globalBefore["m3-ratio-model"].ModelRatio, globalAfter["m3-ratio-model"].ModelRatio)
	assert.Equal(t, globalBefore["m3-price-model"].ModelPrice, globalAfter["m3-price-model"].ModelPrice)
	assert.Equal(t, globalBefore["m3-tiered-model"].BillingExpr, globalAfter["m3-tiered-model"].BillingExpr)
}
