package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
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
	require.NoError(t, model.GrantMoziaWalletQuota(model.MoziaWalletGrantInput{
		UserId:        1004,
		Source:        model.MoziaWalletSourcePaid,
		Amount:        20,
		EventType:     model.MoziaWalletEventTopUp,
		ReferenceType: "test",
		ReferenceId:   "paid-without-subscription",
	}))
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
