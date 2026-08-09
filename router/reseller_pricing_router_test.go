package router

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type resellerPricingRuleEnvelopeData struct {
	Rule model.ResellerPriceRuleRecord `json:"rule"`
}

type resellerPricingListEnvelopeData struct {
	Rules []model.ResellerPriceRuleRecord `json:"rules"`
}

type resellerPlatformPricingPreview struct {
	Model          string `json:"model"`
	BaseQuota      string `json:"base_quota"`
	Multiplier     string `json:"multiplier"`
	EffectiveQuota string `json:"effective_quota"`
	Source         string `json:"source"`
}

type resellerManagementPricingPreview struct {
	Model               string `json:"model"`
	BaseQuota           string `json:"base_quota"`
	RetailMultiplier    string `json:"retail_multiplier"`
	RetailQuota         string `json:"retail_quota"`
	WholesaleMultiplier string `json:"wholesale_multiplier"`
	WholesaleQuota      string `json:"wholesale_quota"`
}

func TestResellerM3PricingContract(t *testing.T) {
	_, db, request := setupResellerM2Test(t)
	resellerA := seedResellerM2(t, db, "Pricing Agency A", "pricing-a.example.com", model.ResellerRoleOwner, "pricing-owner-a", "pricing-admin-a", "pricing-viewer-a")
	resellerB := seedResellerM2(t, db, "Pricing Agency B", "pricing-b.example.com", model.ResellerRoleOwner, "pricing-owner-b", "pricing-admin-b", "pricing-viewer-b")
	customerA := seedCustomerM2(t, db, resellerA.Id, "pricing-customer-a", model.ResellerCustomerStatusActive)
	customerB := seedCustomerM2(t, db, resellerB.Id, "pricing-customer-b", model.ResellerCustomerStatusActive)
	platformBase := fmt.Sprintf("/api/internal/v1/platform/resellers/%d/pricing", resellerA.Id)
	managementHeaders := map[string]string{
		"X-Reseller-Subject": "pricing-owner-a",
		"X-Reseller-Host":    "pricing-a.example.com",
	}
	viewerHeaders := map[string]string{
		"X-Reseller-Subject": "pricing-viewer-a",
		"X-Reseller-Host":    "pricing-a.example.com",
	}

	t.Run("all platform endpoints require the dedicated platform token", func(t *testing.T) {
		for _, test := range []struct {
			method string
			path   string
			body   string
		}{
			{method: http.MethodGet, path: platformBase},
			{method: http.MethodPost, path: platformBase + "/wholesale", body: `{"model":"m3-model","multiplier":"0.8","expected_version":0}`},
			{method: http.MethodPost, path: platformBase + "/preview", body: `{"model":"m3-model","base_quota":"100"}`},
		} {
			recorder := request(test.method, test.path, test.body, "matrix-reseller-management-test-token", "platform-auth_123", nil)
			response := decodeM2Envelope(t, recorder)
			assert.Equal(t, http.StatusUnauthorized, recorder.Code)
			assert.Equal(t, middleware.ResellerErrorServiceUnauthorized, response.Error.Code)
		}
	})

	t.Run("platform writes immutable wholesale versions and previews string quotas", func(t *testing.T) {
		create := request(http.MethodPost, platformBase+"/wholesale", `{"model":"m3-model","multiplier":"0.8","expected_version":0}`, "mozia-mega-test-token", "platform-create_123", nil)
		createEnvelope := decodeM2Envelope(t, create)
		require.Equal(t, http.StatusCreated, create.Code)
		var created resellerPricingRuleEnvelopeData
		require.NoError(t, common.Unmarshal(createEnvelope.RawData, &created))
		assert.Equal(t, model.ResellerPriceRuleKindWholesale, created.Rule.Kind)
		assert.Equal(t, "0.8", created.Rule.Multiplier)
		assert.Equal(t, 1, created.Rule.Version)

		stale := request(http.MethodPost, platformBase+"/wholesale", `{"model":"m3-model","multiplier":"0.9","expected_version":0}`, "mozia-mega-test-token", "platform-stale_123", nil)
		staleEnvelope := decodeM2Envelope(t, stale)
		assert.Equal(t, http.StatusConflict, stale.Code)
		assert.Equal(t, middleware.ResellerErrorConflict, staleEnvelope.Error.Code)

		list := request(http.MethodGet, platformBase, "", "mozia-mega-test-token", "platform-list_123", nil)
		listEnvelope := decodeM2Envelope(t, list)
		require.Equal(t, http.StatusOK, list.Code)
		var listed resellerPricingListEnvelopeData
		require.NoError(t, common.Unmarshal(listEnvelope.RawData, &listed))
		require.Len(t, listed.Rules, 1)
		assert.Equal(t, model.ResellerPriceRuleKindWholesale, listed.Rules[0].Kind)

		preview := request(http.MethodPost, platformBase+"/preview", `{"model":"m3-model","base_quota":"9007199254740993"}`, "mozia-mega-test-token", "platform-preview_123", nil)
		previewEnvelope := decodeM2Envelope(t, preview)
		require.Equal(t, http.StatusOK, preview.Code)
		var data resellerPlatformPricingPreview
		require.NoError(t, common.Unmarshal(previewEnvelope.RawData, &data))
		assert.Equal(t, "9007199254740993", data.BaseQuota)
		assert.Equal(t, "7205759403792794", data.EffectiveQuota)
		assert.Equal(t, "0.8", data.Multiplier)
	})

	t.Run("all management endpoints require the management token", func(t *testing.T) {
		for _, test := range []struct {
			method string
			path   string
			body   string
		}{
			{method: http.MethodGet, path: "/api/internal/v1/reseller/management/pricing"},
			{method: http.MethodPost, path: "/api/internal/v1/reseller/management/pricing/retail", body: `{"model":"m3-model","multiplier":"1.2","expected_version":0}`},
			{method: http.MethodPost, path: "/api/internal/v1/reseller/management/pricing/preview", body: `{"model":"m3-model","base_quota":"100"}`},
		} {
			recorder := request(test.method, test.path, test.body, "mozia-mega-test-token", "management-auth_123", managementHeaders)
			response := decodeM2Envelope(t, recorder)
			assert.Equal(t, http.StatusUnauthorized, recorder.Code)
			assert.Equal(t, middleware.ResellerErrorServiceUnauthorized, response.Error.Code)
		}
	})

	t.Run("viewer reads and previews but cannot write retail", func(t *testing.T) {
		list := request(http.MethodGet, "/api/internal/v1/reseller/management/pricing", "", "matrix-reseller-management-test-token", "viewer-list_123", viewerHeaders)
		require.Equal(t, http.StatusOK, list.Code)

		preview := request(http.MethodPost, "/api/internal/v1/reseller/management/pricing/preview", `{"model":"m3-model","base_quota":101}`, "matrix-reseller-management-test-token", "viewer-preview_123", viewerHeaders)
		previewEnvelope := decodeM2Envelope(t, preview)
		require.Equal(t, http.StatusOK, preview.Code)
		var data resellerManagementPricingPreview
		require.NoError(t, common.Unmarshal(previewEnvelope.RawData, &data))
		assert.Equal(t, "101", data.BaseQuota)
		assert.Equal(t, "101", data.RetailQuota)
		assert.Equal(t, "81", data.WholesaleQuota)

		write := request(http.MethodPost, "/api/internal/v1/reseller/management/pricing/retail", `{"model":"m3-model","multiplier":"1.2","expected_version":0}`, "matrix-reseller-management-test-token", "viewer-write_123", viewerHeaders)
		writeEnvelope := decodeM2Envelope(t, write)
		assert.Equal(t, http.StatusForbidden, write.Code)
		assert.Equal(t, middleware.ResellerErrorForbidden, writeEnvelope.Error.Code)
	})

	t.Run("owner writes retail with optimistic concurrency and tenant scope", func(t *testing.T) {
		create := request(http.MethodPost, "/api/internal/v1/reseller/management/pricing/retail", fmt.Sprintf(`{"model":"m3-model","multiplier":"1.2","customer_id":%d,"expected_version":0}`, customerA.Id), "matrix-reseller-management-test-token", "retail-create_123", managementHeaders)
		createEnvelope := decodeM2Envelope(t, create)
		require.Equal(t, http.StatusCreated, create.Code)
		var created resellerPricingRuleEnvelopeData
		require.NoError(t, common.Unmarshal(createEnvelope.RawData, &created))
		assert.Equal(t, model.ResellerPriceRuleKindRetail, created.Rule.Kind)
		assert.Equal(t, "1.2", created.Rule.Multiplier)
		require.NotNil(t, created.Rule.CustomerId)
		assert.Equal(t, customerA.Id, *created.Rule.CustomerId)

		stale := request(http.MethodPost, "/api/internal/v1/reseller/management/pricing/retail", fmt.Sprintf(`{"model":"m3-model","multiplier":"1.3","customer_id":%d,"expected_version":0}`, customerA.Id), "matrix-reseller-management-test-token", "retail-stale_123", managementHeaders)
		staleEnvelope := decodeM2Envelope(t, stale)
		assert.Equal(t, http.StatusConflict, stale.Code)
		assert.Equal(t, middleware.ResellerErrorConflict, staleEnvelope.Error.Code)

		forged := request(http.MethodPost, "/api/internal/v1/reseller/management/pricing/retail", fmt.Sprintf(`{"model":"m3-model","multiplier":"1.3","customer_id":%d,"reseller_id":%d}`, customerA.Id, resellerB.Id), "matrix-reseller-management-test-token", "retail-forged_123", managementHeaders)
		forgedEnvelope := decodeM2Envelope(t, forged)
		assert.Equal(t, http.StatusBadRequest, forged.Code)
		assert.Equal(t, middleware.ResellerErrorInvalidRequest, forgedEnvelope.Error.Code)

		crossTenant := request(http.MethodPost, "/api/internal/v1/reseller/management/pricing/preview", fmt.Sprintf(`{"model":"m3-model","base_quota":"101","customer_id":%d}`, customerB.Id), "matrix-reseller-management-test-token", "retail-cross-tenant_123", managementHeaders)
		crossTenantEnvelope := decodeM2Envelope(t, crossTenant)
		assert.Equal(t, http.StatusNotFound, crossTenant.Code)
		assert.Equal(t, middleware.ResellerErrorNotFound, crossTenantEnvelope.Error.Code)

		preview := request(http.MethodPost, "/api/internal/v1/reseller/management/pricing/preview", fmt.Sprintf(`{"model":"m3-model","base_quota":101,"customer_id":%d}`, customerA.Id), "matrix-reseller-management-test-token", "retail-preview_123", managementHeaders)
		previewEnvelope := decodeM2Envelope(t, preview)
		require.Equal(t, http.StatusOK, preview.Code)
		var data resellerManagementPricingPreview
		require.NoError(t, common.Unmarshal(previewEnvelope.RawData, &data))
		assert.Equal(t, "101", data.BaseQuota)
		assert.Equal(t, "1.2", data.RetailMultiplier)
		assert.Equal(t, "121", data.RetailQuota)
		assert.Equal(t, "0.8", data.WholesaleMultiplier)
		assert.Equal(t, "81", data.WholesaleQuota)

		list := request(http.MethodGet, fmt.Sprintf("/api/internal/v1/reseller/management/pricing?customer_id=%d", customerA.Id), "", "matrix-reseller-management-test-token", "retail-list_123", viewerHeaders)
		listEnvelope := decodeM2Envelope(t, list)
		require.Equal(t, http.StatusOK, list.Code)
		var listed resellerPricingListEnvelopeData
		require.NoError(t, common.Unmarshal(listEnvelope.RawData, &listed))
		require.Len(t, listed.Rules, 2)
		assert.ElementsMatch(t, []string{model.ResellerPriceRuleKindWholesale, model.ResellerPriceRuleKindRetail}, []string{listed.Rules[0].Kind, listed.Rules[1].Kind})
	})
}
