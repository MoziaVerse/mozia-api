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

func TestResellerPricingModelAvailability(t *testing.T) {
	_, db, request := setupResellerM2Test(t)
	require.NoError(t, db.AutoMigrate(&model.Ability{}))
	require.NoError(t, db.Create(&[]model.Ability{
		{Group: "default", Model: "available", ChannelId: 1, Enabled: true},
		{Group: "default", Model: "delisted", ChannelId: 2, Enabled: false},
	}).Error)
	agency := seedResellerM2(t, db, "Agency", "pricing-availability.example.com", model.ResellerRoleOwner, "owner", "admin", "viewer")
	customer := seedCustomerM2(t, db, agency.Id, "customer", model.ResellerCustomerStatusActive)
	platform := fmt.Sprintf("/api/internal/v1/platform/resellers/%d", agency.Id)
	headers := map[string]string{
		"X-Reseller-Subject": "owner", "X-Reseller-Host": "pricing-availability.example.com",
		"X-Platform-Actor-Subject": "platform-admin",
	}
	endpoints := []struct {
		name       string
		list       string
		write      string
		token      string
		kind       string
		customerID int
	}{
		{"wholesale", platform + "/pricing", platform + "/pricing/wholesale", "mozia-mega-test-token", "wholesale", 0},
		{"reseller retail", "/api/internal/v1/reseller/management/pricing", "/api/internal/v1/reseller/management/pricing/retail", "matrix-reseller-management-test-token", "retail", 0},
		{"customer retail", fmt.Sprintf("%s/customers/%d/pricing", platform, customer.Id), fmt.Sprintf("%s/customers/%d/pricing/retail", platform, customer.Id), "mozia-mega-test-token", "retail", customer.Id},
	}
	for _, endpoint := range endpoints {
		for _, name := range []string{"delisted", "removed"} {
			require.NoError(t, db.Create(&model.ResellerPriceRule{
				ResellerId: agency.Id, Kind: endpoint.kind, ModelName: name, CustomerId: endpoint.customerID,
				Version: 1, MultiplierPPM: 1_000_000, Enabled: true, EffectiveAt: common.GetTimestamp(), CreatedBy: "owner",
			}).Error)
		}
	}
	for _, endpoint := range endpoints {
		t.Run(endpoint.name, func(t *testing.T) {
			response := request(http.MethodGet, endpoint.list, "", endpoint.token, "availability-list", headers)
			require.Equal(t, http.StatusOK, response.Code)
			var data resellerPricingListEnvelopeData
			require.NoError(t, common.Unmarshal(decodeM2Envelope(t, response).RawData, &data))
			assert.Equal(t, []string{"available"}, data.Models)
			assert.NotEmpty(t, data.Rules)
			for _, rule := range data.Rules {
				assert.Contains(t, []string{"delisted", "removed"}, rule.Model)
				assert.Equal(t, 1, rule.Version)
				assert.Equal(t, "1", rule.Multiplier)
			}
			for _, name := range []string{"delisted", "removed", "unknown"} {
				for _, value := range []string{`"multiplier":"1"`, `"official_discount":"10"`} {
					body := fmt.Sprintf(`{"model":%q,%s,"expected_version":1}`, name, value)
					blocked := request(http.MethodPost, endpoint.write, body, endpoint.token, "availability-write", headers)
					assert.Equal(t, http.StatusConflict, blocked.Code)
					assert.Equal(t, middleware.ResellerErrorModelUnavailable, decodeM2Envelope(t, blocked).Error.Code)
				}
			}
		})
	}
	var unchanged []model.ResellerPriceRule
	require.NoError(t, db.Find(&unchanged).Error)
	require.Len(t, unchanged, 6)
	for _, rule := range unchanged {
		assert.Equal(t, 1, rule.Version)
		assert.Equal(t, int64(1_000_000), rule.MultiplierPPM)
	}

	// A relisted model becomes selectable again and continues its original versions.
	require.NoError(t, db.Model(&model.Ability{}).Where("model = ?", "delisted").Update("enabled", true).Error)
	for _, endpoint := range endpoints {
		listed := request(http.MethodGet, endpoint.list, "", endpoint.token, "availability-relisted", headers)
		require.Equal(t, http.StatusOK, listed.Code)
		var data resellerPricingListEnvelopeData
		require.NoError(t, common.Unmarshal(decodeM2Envelope(t, listed).RawData, &data))
		assert.Equal(t, []string{"available", "delisted"}, data.Models)
		saved := request(http.MethodPost, endpoint.write, `{"model":"delisted","multiplier":"1","expected_version":1}`, endpoint.token, "availability-resave", headers)
		require.Equal(t, http.StatusCreated, saved.Code)
		var result resellerPricingRuleEnvelopeData
		require.NoError(t, common.Unmarshal(decodeM2Envelope(t, saved).RawData, &result))
		assert.Equal(t, 2, result.Rule.Version)
	}
}
