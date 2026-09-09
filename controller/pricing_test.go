package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type pricingResponse struct {
	Success bool            `json:"success"`
	Data    []model.Pricing `json:"data"`
}

func TestGetPricingIncludesOnlyVisiblePerformanceAndSurvivesMetricsFailure(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(
		&model.PerfMetric{}, &model.MoziaWalletBalance{}, &model.MoziaWalletTransaction{},
		&model.MoziaModelQuotaPolicy{}, &model.UserSubscription{},
	))
	groups := setting.UserUsableGroups2JSONString()
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(groups))
		model.InvalidatePricingCache()
	})
	require.NoError(t, setting.UpdateUserUsableGroupsByJSONString(`{"default":"Default"}`))
	const userID = 1060
	require.NoError(t, db.Create(&model.User{
		Id: userID, Username: "performance-customer", Group: "default", Status: common.UserStatusEnabled,
	}).Error)
	require.NoError(t, db.Create(&model.UserSSO{UserId: userID, SSOSub: "performance-subject"}).Error)
	agency := model.Reseller{
		Name: "Performance Agency", Status: model.ResellerStatusActive,
		ModelAccess: model.ResellerModelAccess{Restricted: true, Models: []string{"perf-visible", "perf-no-data"}},
	}
	require.NoError(t, db.Create(&agency).Error)
	require.NoError(t, db.Create(&model.ResellerCustomer{
		ResellerId: agency.Id, Subject: "performance-subject", Status: model.ResellerCustomerStatusActive,
	}).Error)
	for i, name := range []string{"perf-visible", "perf-no-data", "perf-hidden"} {
		require.NoError(t, db.Create(&model.Ability{Group: "default", Model: name, ChannelId: i + 1, Enabled: true}).Error)
	}
	now := time.Now().Truncate(time.Hour)
	require.NoError(t, db.Create(&[]model.PerfMetric{
		{ModelName: "perf-visible", Group: "default", BucketTs: now.Add(-time.Hour).Unix(), RequestCount: 2, SuccessCount: 1, TotalLatencyMs: 6000, OutputTokens: 300, GenerationMs: 1500},
		{ModelName: "perf-visible", Group: "default", BucketTs: now.Add(-2 * time.Hour).Unix(), RequestCount: 8, SuccessCount: 7, TotalLatencyMs: 34000, OutputTokens: 700, GenerationMs: 3500},
		{ModelName: "perf-visible", Group: "private", BucketTs: now.Add(-time.Hour).Unix(), RequestCount: 100, SuccessCount: 100, TotalLatencyMs: 1000, OutputTokens: 1000, GenerationMs: 1000},
		{ModelName: "perf-visible", Group: "default", BucketTs: now.Add(-48 * time.Hour).Unix(), RequestCount: 100, SuccessCount: 100, TotalLatencyMs: 1000},
		{ModelName: "perf-hidden", Group: "default", BucketTs: now.Add(-time.Hour).Unix(), RequestCount: 10, SuccessCount: 10},
	}).Error)
	model.InvalidatePricingCache()

	for _, include := range []bool{true, false} {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		path := "/api/sso/pricing?include_inaccessible=true"
		if include {
			path += "&include_performance=true"
		}
		ctx.Request = httptest.NewRequest(http.MethodGet, path, nil)
		ctx.Set("id", userID)
		GetPricing(ctx)
		pricing := pricingByModelName(decodePricingResponse(t, recorder))
		require.Contains(t, pricing, "perf-visible")
		require.Contains(t, pricing, "perf-no-data")
		assert.NotContains(t, pricing, "perf-hidden")
		assert.Nil(t, pricing["perf-no-data"].Performance)
		if include {
			assert.Equal(t, &model.PricingPerformance{WindowHours: 24, AvgLatencyMs: 4000, SuccessRate: 80, AvgTps: 200}, pricing["perf-visible"].Performance)
		} else {
			assert.Nil(t, pricing["perf-visible"].Performance)
		}
		for _, field := range []string{`"request_count"`, `"success_count"`, `"total_latency_ms"`} {
			assert.NotContains(t, recorder.Body.String(), field)
		}
	}
	for _, pricing := range model.GetPricing() {
		assert.Nil(t, pricing.Performance, "customer metrics must not mutate the cached catalog")
	}

	require.NoError(t, db.Migrator().DropTable(&model.PerfMetric{}))
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/sso/pricing?include_inaccessible=true&include_performance=true", nil)
	ctx.Set("id", userID)
	GetPricing(ctx)
	pricing := pricingByModelName(decodePricingResponse(t, recorder))
	require.Contains(t, pricing, "perf-visible")
	assert.Nil(t, pricing["perf-visible"].Performance)
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
	require.NoError(t, db.Create(&model.Model{
		ModelName: "paid-only-catalog-model",
		Tags:      "category:video,视频",
		Status:    1,
		NameRule:  model.NameRuleExact,
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
	assert.Equal(t, "video", item.ModelCategory)
	require.NotNil(t, item.Access)
	assert.False(t, item.Access.Available)
	assert.Equal(t, model.MoziaPricingAccessReasonRequiresPaidQuota, item.Access.Reason)
	assert.Equal(t, []string{model.MoziaWalletSourcePaid}, item.Access.RequiredSources)
	assert.True(t, item.Access.SubscriptionAllowed)

	t.Run("include_inaccessible cannot bypass reseller authorization", func(t *testing.T) {
		agency := model.Reseller{Name: "restricted-agency", Status: model.ResellerStatusActive, ModelAccess: model.ResellerModelAccess{Restricted: true}}
		require.NoError(t, db.Create(&agency).Error)
		require.NoError(t, db.Create(&model.UserSSO{UserId: 1004, SSOSub: "restricted-customer"}).Error)
		require.NoError(t, db.Create(&model.ResellerCustomer{ResellerId: agency.Id, Subject: "restricted-customer", Status: model.ResellerCustomerStatusActive}).Error)
		for _, query := range []string{"", "?include_inaccessible=true"} {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodGet, "/api/sso/pricing"+query, nil)
			ctx.Set("id", 1004)
			GetPricing(ctx)
			assert.Empty(t, decodePricingResponse(t, recorder))
		}
		assert.Contains(t, pricingModelNames(model.GetPricing()), "paid-only-catalog-model", "the shared catalog must not be mutated")
	})
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
