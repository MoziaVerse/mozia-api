package router

import (
	"fmt"
	"math"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type resellerEarningsMetric struct {
	WholesaleQuota        string `json:"wholesale_quota"`
	WholesaleQuotaDisplay string `json:"wholesale_quota_display"`
	MarginQuota           string `json:"margin_quota"`
	MarginQuotaDisplay    string `json:"margin_quota_display"`
	MarginRate            string `json:"margin_rate"`
}

type resellerEarningsRow struct {
	CustomerId    int                     `json:"customer_id"`
	Model         string                  `json:"model"`
	CustomerQuota string                  `json:"customer_quota"`
	Earnings      *resellerEarningsMetric `json:"earnings"`
}

type resellerEarningsData struct {
	Summary       resellerEarningsRow   `json:"summary"`
	Items         []resellerEarningsRow `json:"items"`
	CustomerSpend []resellerEarningsRow `json:"customer_spend"`
	ModelSpend    []resellerEarningsRow `json:"model_spend"`
	SubagentSpend []resellerEarningsRow `json:"subagent_spend"`
}

func TestResellerUsageEarningsContract(t *testing.T) {
	_, db, request := setupResellerM2Test(t)
	reseller := seedResellerM2(t, db, "Earnings A", "earnings-a.example.com", model.ResellerRoleOwner, "earnings-owner", "earnings-admin", "earnings-viewer")
	other := seedResellerM2(t, db, "Earnings B", "earnings-b.example.com", model.ResellerRoleOwner, "earnings-other-owner", "earnings-other-admin", "earnings-other-viewer")
	first := seedCustomerM2(t, db, reseller.Id, "earnings-customer-a", model.ResellerCustomerStatusActive)
	second := seedCustomerM2(t, db, reseller.Id, "earnings-customer-b", model.ResellerCustomerStatusSuspend)
	subagent := model.ResellerMember{ResellerId: reseller.Id, Subject: "earnings-subagent", Role: model.ResellerRoleSubagent, Status: model.ResellerMemberStatusActive, CanManagePricing: true}
	require.NoError(t, db.Create(&subagent).Error)
	require.NoError(t, db.Model(&first).Updates(map[string]any{"subagent_member_id": subagent.Id, "subagent_assigned_at": int64(150)}).Error)
	for _, row := range []struct {
		resellerID, customerID int
		modelName, status      string
		charge, cost, at       int64
	}{
		{reseller.Id, first.Id, "model-a", model.ResellerSettlementStatusSettled, 1_200_000, 800_000, 100},
		{reseller.Id, first.Id, "model-a", model.ResellerSettlementStatusSettled, 600_000, 300_000, 200},
		{reseller.Id, second.Id, "model-b", model.ResellerSettlementStatusSettled, 200_000, 150_000, 100},
		{reseller.Id, first.Id, "refunded", model.ResellerSettlementStatusRefunded, 9_000_000, 1, 100},
		{reseller.Id, first.Id, "failed", model.ResellerSettlementStatusFailed, 9_000_000, 1, 100},
		{reseller.Id, first.Id, "reserved", model.ResellerSettlementStatusReserved, 9_000_000, 1, 100},
		{reseller.Id, first.Id, "settling", model.ResellerSettlementStatusSettling, 9_000_000, 1, 100},
		{other.Id, first.Id, "other-tenant", model.ResellerSettlementStatusSettled, 9_000_000, 1, 100},
	} {
		seedResellerSettlement(t, db, model.ResellerRequestSettlement{
			RequestId:  fmt.Sprintf("earnings-%d-%s-%d", row.resellerID, row.modelName, row.at),
			ResellerId: row.resellerID, CustomerId: row.customerID, UserId: 1,
			ModelName: row.modelName, Status: row.status, CreatedAt: row.at, SettledAt: row.at + 1,
			ActualCustomerQuota: row.charge, ActualWholesaleQuota: row.cost,
			// These differ from the actual amounts: reports must use the settlement, not reprice it.
			EstimatedCustomerQuota: 99_000_000, EstimatedWholesaleQuota: 1,
			WholesaleMultiplierPPM: 2_000_000, RetailMultiplierPPM: 3_000_000,
			UsageJSON: `{"private":"hidden-provider-data"}`,
		})
	}
	const path = "/api/internal/v1/reseller/management/usage"
	headers := map[string]string{"X-Reseller-Host": "earnings-a.example.com", "X-Reseller-Subject": "earnings-owner"}
	get := func(query string) resellerEarningsData {
		t.Helper()
		response := request(http.MethodGet, path+query, "", "matrix-reseller-management-test-token", "earnings-request_123", headers)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		var data resellerEarningsData
		require.NoError(t, common.Unmarshal(decodeM2Envelope(t, response).RawData, &data))
		assert.NotContains(t, response.Body.String(), "hidden-provider-data")
		return data
	}

	all := get("")
	assert.Equal(t, "2000000", all.Summary.CustomerQuota)
	require.NotNil(t, all.Summary.Earnings)
	assert.Equal(t, resellerEarningsMetric{"1250000", "＄2.500000", "750000", "＄1.500000", "37.50%"}, *all.Summary.Earnings)
	require.Len(t, all.Items, 2)
	require.Len(t, all.CustomerSpend, 2)
	require.Len(t, all.ModelSpend, 2)
	for _, rows := range [][]resellerEarningsRow{all.Items, all.CustomerSpend, all.ModelSpend} {
		require.NotNil(t, rows[0].Earnings)
		require.NotNil(t, rows[1].Earnings)
		assert.Equal(t, "700000", rows[0].Earnings.MarginQuota)
		assert.Equal(t, "50000", rows[1].Earnings.MarginQuota)
	}
	require.Len(t, all.SubagentSpend, 1)
	require.NotNil(t, all.SubagentSpend[0].Earnings)
	assert.Equal(t, "300000", all.SubagentSpend[0].Earnings.MarginQuota)

	bounded := get("?start_timestamp=100&end_timestamp=100")
	assert.Equal(t, "450000", bounded.Summary.Earnings.MarginQuota)
	assert.Equal(t, "32.14%", bounded.Summary.Earnings.MarginRate)
	assert.Equal(t, "700000", get("?model=model-a").Summary.Earnings.MarginQuota)
	assert.Equal(t, "50000", get(fmt.Sprintf("?customer_id=%d", second.Id)).Summary.Earnings.MarginQuota)
	empty := get("?start_timestamp=300&end_timestamp=400")
	assert.Equal(t, "0", empty.Summary.Earnings.MarginQuota)
	assert.Equal(t, "0", empty.Summary.Earnings.WholesaleQuota)
	assert.Equal(t, "—", empty.Summary.Earnings.MarginRate)
	assert.Empty(t, empty.Items)

	headers["X-Reseller-Subject"] = "earnings-admin"
	assert.Equal(t, all.Summary, get("").Summary)
	for _, subject := range []string{"earnings-viewer", "earnings-subagent"} {
		headers["X-Reseller-Subject"] = subject
		response := request(http.MethodGet, path, "", "matrix-reseller-management-test-token", "earnings-redacted_123", headers)
		require.Equal(t, http.StatusOK, response.Code)
		assert.NotContains(t, response.Body.String(), `"earnings"`)
		assert.NotContains(t, response.Body.String(), "wholesale_quota")
		assert.NotContains(t, response.Body.String(), "margin_quota")
	}
	headers["X-Reseller-Subject"] = "earnings-owner"
	headers["X-Reseller-Host"] = "earnings-b.example.com"
	denied := request(http.MethodGet, path, "", "matrix-reseller-management-test-token", "earnings-cross-tenant_123", headers)
	assert.Equal(t, http.StatusNotFound, denied.Code)
	headers["X-Reseller-Host"] = "earnings-a.example.com"
	platform := request(http.MethodGet, fmt.Sprintf("/api/internal/v1/platform/resellers/%d/usage", reseller.Id), "", "mozia-mega-test-token", "earnings-platform_123", nil)
	require.Equal(t, http.StatusOK, platform.Code)
	var platformData resellerEarningsData
	require.NoError(t, common.Unmarshal(decodeM2Envelope(t, platform).RawData, &platformData))
	assert.Equal(t, all, platformData)

	// A refund removes both sides of the same historical request from every rollup.
	require.NoError(t, model.RefundResellerSettlement(fmt.Sprintf("earnings-%d-model-a-200", reseller.Id)))
	assert.Equal(t, bounded.Summary, get("").Summary)
	// Removing the current customer relationship must not erase the old reseller's earnings.
	require.NoError(t, db.Delete(&first).Error)
	assert.Equal(t, bounded.Summary, get("").Summary)

	seedResellerSettlement(t, db, model.ResellerRequestSettlement{
		RequestId: "earnings-overflow", ResellerId: reseller.Id, CustomerId: second.Id, UserId: 1,
		ModelName: "overflow", Status: model.ResellerSettlementStatusSettled,
		ActualCustomerQuota: math.MaxInt64, ActualWholesaleQuota: math.MaxInt64,
	})
	_, err := model.ListResellerUsage(reseller.Id, nil, nil, nil, nil, nil)
	assert.ErrorIs(t, err, model.ErrResellerQuotaOverflow)
}
