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

func TestPlatformCustomerPricing(t *testing.T) {
	_, db, request := setupResellerM2Test(t)
	require.NoError(t, db.AutoMigrate(&model.Ability{}))
	require.NoError(t, db.Create(&model.Ability{Group: "default", Model: "model-a", ChannelId: 1, Enabled: true}).Error)
	agency := seedResellerM2(t, db, "Agency", "customer-pricing.example.com", model.ResellerRoleOwner, "owner", "admin", "viewer")
	otherAgency := seedResellerM2(t, db, "Other", "other-pricing.example.com", model.ResellerRoleOwner, "other-owner", "other-admin", "other-viewer")
	customer := seedCustomerM2(t, db, agency.Id, "customer-one", model.ResellerCustomerStatusActive)
	sibling := seedCustomerM2(t, db, agency.Id, "customer-two", model.ResellerCustomerStatusActive)
	outsider := seedCustomerM2(t, db, otherAgency.Id, "customer-other", model.ResellerCustomerStatusActive)
	require.NoError(t, db.Model(&customer).Updates(map[string]interface{}{"matrix_name": "客户名称", "remark": "客户备注"}).Error)
	now := common.GetTimestamp()
	rules := []model.ResellerPriceRule{
		{ResellerId: agency.Id, Kind: "wholesale", ModelName: "model-a", Version: 1, MultiplierPPM: 350000, Enabled: true, EffectiveAt: now - 10, CreatedBy: "platform"},
		{ResellerId: agency.Id, Kind: "retail", ModelName: "model-a", Version: 1, MultiplierPPM: 900000, Enabled: true, EffectiveAt: now - 10, CreatedBy: "owner"},
		{ResellerId: agency.Id, Kind: "retail", ModelName: "model-a", CustomerId: customer.Id, Version: 1, MultiplierPPM: 400000, Enabled: true, EffectiveAt: now - 10, CreatedBy: "owner"},
		{ResellerId: agency.Id, Kind: "retail", ModelName: "model-a", CustomerId: customer.Id, Version: 2, MultiplierPPM: 800000, Enabled: true, EffectiveAt: now + 3600, CreatedBy: "owner"},
		{ResellerId: agency.Id, Kind: "retail", ModelName: "model-a", CustomerId: sibling.Id, Version: 1, MultiplierPPM: 350000, Enabled: true, EffectiveAt: now - 10, CreatedBy: "owner"},
	}
	require.NoError(t, db.Create(&rules).Error)
	base := fmt.Sprintf("/api/internal/v1/platform/resellers/%d/customers/%d/pricing", agency.Id, customer.Id)
	headers := map[string]string{"X-Platform-Actor-Subject": "platform-admin-subject"}
	body := `{"model":"model-a","multiplier":"0.723","expected_version":2}`

	t.Run("requires platform credentials and keeps customer scope in the path", func(t *testing.T) {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			path := base
			if method == http.MethodPost {
				path += "/retail"
			}
			unauthorized := request(method, path, body, "matrix-reseller-management-test-token", "customer-price-auth", headers)
			assert.Equal(t, http.StatusUnauthorized, unauthorized.Code)
			foreignPath := fmt.Sprintf("/api/internal/v1/platform/resellers/%d/customers/%d/pricing", agency.Id, outsider.Id)
			if method == http.MethodPost {
				foreignPath += "/retail"
			}
			foreign := request(method, foreignPath, body, "mozia-mega-test-token", "customer-price-foreign", headers)
			assert.Equal(t, http.StatusNotFound, foreign.Code)
		}
		for _, invalid := range []string{
			`{"model":"model-a","multiplier":"0.723"}`,
			`{"model":"model-a","multiplier":"0.723","expected_version":2,"customer_id":999}`,
			`{"model":"model-a","multiplier":"0.723","expected_version":2,"reseller_id":999}`,
		} {
			response := request(http.MethodPost, base+"/retail", invalid, "mozia-mega-test-token", "customer-price-invalid", headers)
			assert.Equal(t, http.StatusBadRequest, response.Code)
		}
		missingActor := request(http.MethodPost, base+"/retail", body, "mozia-mega-test-token", "customer-price-no-actor", nil)
		assert.Equal(t, http.StatusBadRequest, missingActor.Code)
	})

	t.Run("returns identity and separates current prices from scheduled versions", func(t *testing.T) {
		response := request(http.MethodGet, base, "", "mozia-mega-test-token", "customer-price-read", nil)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		var data struct {
			Customer  model.ResellerCustomerRecord    `json:"customer"`
			Rules     []model.ResellerPriceRuleRecord `json:"rules"`
			Effective []model.ResellerPriceRuleRecord `json:"effective_rules"`
		}
		require.NoError(t, common.Unmarshal(decodeM2Envelope(t, response).RawData, &data))
		assert.Equal(t, customer.Id, data.Customer.Id)
		assert.Equal(t, "客户名称", data.Customer.MatrixName)
		require.NotNil(t, data.Customer.Remark)
		assert.Equal(t, "客户备注", *data.Customer.Remark)
		require.Len(t, data.Rules, 3)
		require.Len(t, data.Effective, 3)
		for _, record := range data.Rules {
			if record.CustomerId != nil {
				assert.Equal(t, customer.Id, *record.CustomerId)
				assert.Equal(t, 2, record.Version)
			}
		}
		for _, record := range data.Effective {
			if record.CustomerId != nil {
				assert.Equal(t, customer.Id, *record.CustomerId)
				assert.Equal(t, "0.4", record.Multiplier)
				assert.Equal(t, 1, record.Version)
			}
		}
	})

	t.Run("writes an attributed immutable customer version used by billing", func(t *testing.T) {
		response := request(http.MethodPost, base+"/retail", body, "mozia-mega-test-token", "customer-price-write", headers)
		require.Equal(t, http.StatusCreated, response.Code, response.Body.String())
		var data resellerPricingRuleEnvelopeData
		require.NoError(t, common.Unmarshal(decodeM2Envelope(t, response).RawData, &data))
		assert.Equal(t, "retail", data.Rule.Kind)
		assert.Equal(t, 3, data.Rule.Version)
		assert.Equal(t, "platform-admin-subject", data.Rule.CreatedBy)
		assert.Equal(t, "0.723", data.Rule.Multiplier)
		price, err := model.ResolveResellerRetailPrice(agency.Id, customer.Id, "model-a", common.GetTimestamp())
		require.NoError(t, err)
		assert.Equal(t, int64(723000), price.MultiplierPPM)
		original := model.ResellerPriceRule{}
		require.NoError(t, db.First(&original, rules[2].Id).Error)
		assert.Equal(t, int64(400000), original.MultiplierPPM)
		wholesale, err := model.ResolveResellerWholesalePrice(agency.Id, "model-a", now)
		require.NoError(t, err)
		assert.Equal(t, int64(350000), wholesale.MultiplierPPM)
		siblingPrice, err := model.ResolveResellerRetailPrice(agency.Id, sibling.Id, "model-a", now)
		require.NoError(t, err)
		assert.Equal(t, int64(350000), siblingPrice.MultiplierPPM)
	})

	t.Run("rejects stale versions and retail below wholesale without creating a rule", func(t *testing.T) {
		for _, tc := range []struct{ body, code string }{
			{body, middleware.ResellerErrorPricingVersion},
			{`{"model":"model-a","multiplier":"0.34","expected_version":3}`, middleware.ResellerErrorPricingMargin},
		} {
			response := request(http.MethodPost, base+"/retail", tc.body, "mozia-mega-test-token", "customer-price-conflict", headers)
			require.Equal(t, http.StatusConflict, response.Code)
			assert.Equal(t, tc.code, decodeM2Envelope(t, response).Error.Code)
		}
		var count int64
		require.NoError(t, db.Model(&model.ResellerPriceRule{}).Where("reseller_id = ? AND customer_id = ?", agency.Id, customer.Id).Count(&count).Error)
		assert.Equal(t, int64(3), count)
	})
}
