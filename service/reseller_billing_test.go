package service

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func seedResellerBillingFixture(t *testing.T, userID int, initialQuota int, modelName string) (model.Reseller, model.ResellerCustomer) {
	t.Helper()
	seedUser(t, userID, initialQuota)
	require.NoError(t, model.RecordMoziaInitialGiftQuota(userID, initialQuota, "test", "reseller-billing"))
	subject := "reseller-billing-subject-" + common.GetRandomString(8)
	require.NoError(t, model.DB.Create(&model.UserSSO{SSOSub: subject, UserId: userID}).Error)
	reseller := model.Reseller{Name: "Billing Reseller", Status: model.ResellerStatusActive}
	require.NoError(t, model.DB.Create(&reseller).Error)
	customer := model.ResellerCustomer{ResellerId: reseller.Id, Subject: subject, Status: model.ResellerCustomerStatusActive}
	require.NoError(t, model.DB.Create(&customer).Error)
	zero := 0
	_, err := model.CreateResellerPriceRule(model.CreateResellerPriceRuleParams{
		ResellerId: reseller.Id, Kind: model.ResellerPriceRuleKindWholesale, ModelName: modelName,
		MultiplierPPM: 800_000, ExpectedVersion: &zero, Enabled: true,
		EffectiveAt: common.GetTimestamp(), CreatedBy: "test",
	})
	require.NoError(t, err)
	_, err = model.CreateResellerPriceRule(model.CreateResellerPriceRuleParams{
		ResellerId: reseller.Id, Kind: model.ResellerPriceRuleKindRetail, ModelName: modelName,
		MultiplierPPM: 1_200_000, ExpectedVersion: &zero, Enabled: true,
		EffectiveAt: common.GetTimestamp(), CreatedBy: "test",
	})
	require.NoError(t, err)
	return reseller, customer
}

func resellerBillingRelay(userID int, requestID string, modelName string) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		UserId: userID, RequestId: requestID, OriginModelName: modelName, IsPlayground: true,
		UserSetting: dto.UserSetting{BillingPreference: "wallet_only"},
	}
}

func testGinContext() *gin.Context {
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	return context
}

func TestResellerBillingLifecycleRejectsReplayAndFreezesRules(t *testing.T) {
	truncate(t)
	gin.SetMode(gin.TestMode)
	const userID, initialQuota = 61, 1_000
	const modelName = "reseller-billing-model"
	reseller, _ := seedResellerBillingFixture(t, userID, initialQuota, modelName)
	relayInfo := resellerBillingRelay(userID, "reseller-billing-request_61", modelName)

	require.Nil(t, PreConsumeBilling(testGinContext(), 100, relayInfo))
	assert.Equal(t, 120, relayInfo.FinalPreConsumedQuota)
	assert.Equal(t, initialQuota-120, getUserQuota(t, userID))
	settlement, err := model.GetResellerRequestSettlement(relayInfo.RequestId)
	require.NoError(t, err)
	assert.Equal(t, model.ResellerSettlementStatusReserved, settlement.Status)
	assert.Equal(t, int64(100), settlement.EstimatedBaseQuota)
	assert.Equal(t, int64(120), settlement.EstimatedCustomerQuota)
	assert.Equal(t, int64(80), settlement.EstimatedWholesaleQuota)

	replay := resellerBillingRelay(userID, relayInfo.RequestId, modelName)
	replayErr := PreConsumeBilling(testGinContext(), 100, replay)
	require.NotNil(t, replayErr)
	assert.Equal(t, initialQuota-120, getUserQuota(t, userID), "request replay must not reserve funds twice")

	CaptureResellerBillingUsage(relayInfo, map[string]any{"kind": "tokens", "total_tokens": 150})
	require.NoError(t, SettleBilling(testGinContext(), relayInfo, 150))
	assert.Equal(t, initialQuota-180, getUserQuota(t, userID))
	settlement, err = model.GetResellerRequestSettlement(relayInfo.RequestId)
	require.NoError(t, err)
	assert.Equal(t, model.ResellerSettlementStatusSettled, settlement.Status)
	assert.Equal(t, int64(150), settlement.ActualBaseQuota)
	assert.Equal(t, int64(180), settlement.ActualCustomerQuota)
	assert.Equal(t, int64(120), settlement.ActualWholesaleQuota)
	assert.JSONEq(t, `{"kind":"tokens","total_tokens":150}`, settlement.UsageJSON)

	one := 1
	_, err = model.CreateResellerPriceRule(model.CreateResellerPriceRuleParams{
		ResellerId: reseller.Id, Kind: model.ResellerPriceRuleKindRetail, ModelName: modelName,
		MultiplierPPM: 2_000_000, ExpectedVersion: &one, Enabled: true,
		EffectiveAt: common.GetTimestamp(), CreatedBy: "later-change",
	})
	require.NoError(t, err)
	require.NoError(t, SettleBilling(testGinContext(), relayInfo, 150))
	assert.Equal(t, initialQuota-180, getUserQuota(t, userID), "settlement replay must not charge again")
	settlement, err = model.GetResellerRequestSettlement(relayInfo.RequestId)
	require.NoError(t, err)
	assert.Equal(t, int64(1_200_000), settlement.RetailMultiplierPPM, "future rules must not rewrite a snapshot")
}

func TestPrepareResellerBillingRejectsRuntimeNegativeMargin(t *testing.T) {
	truncate(t)
	gin.SetMode(gin.TestMode)
	const userID, initialQuota = 62, 1_000
	const modelName = "runtime-margin-model"
	reseller, _ := seedResellerBillingFixture(t, userID, initialQuota, "fixture-model")
	now := common.GetTimestamp()
	require.NoError(t, model.DB.Create(&[]model.ResellerPriceRule{
		{ResellerId: reseller.Id, Kind: model.ResellerPriceRuleKindWholesale, ModelName: modelName, Version: 1, MultiplierPPM: 1_200_000, Enabled: true, EffectiveAt: now, CreatedBy: "legacy-import"},
		{ResellerId: reseller.Id, Kind: model.ResellerPriceRuleKindRetail, ModelName: modelName, Version: 1, MultiplierPPM: 800_000, Enabled: true, EffectiveAt: now, CreatedBy: "legacy-import"},
	}).Error)
	relayInfo := resellerBillingRelay(userID, "runtime-margin-request_62", modelName)

	apiErr := PreConsumeBilling(testGinContext(), 100, relayInfo)
	require.NotNil(t, apiErr)
	assert.Equal(t, initialQuota, getUserQuota(t, userID))
	assert.Nil(t, relayInfo.ResellerBilling)
	_, err := model.GetResellerRequestSettlement(relayInfo.RequestId)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

func TestResellerBillingRefundIncludesZeroQuotaSnapshot(t *testing.T) {
	truncate(t)
	gin.SetMode(gin.TestMode)
	const userID, initialQuota = 63, 1_000
	const modelName = "reseller-refund-model"
	seedResellerBillingFixture(t, userID, initialQuota, modelName)

	for _, test := range []struct {
		name      string
		requestID string
		baseQuota int
	}{
		{name: "reserved funds", requestID: "reseller-refund-request_63", baseQuota: 50},
		{name: "zero quota snapshot", requestID: "reseller-zero-request_63", baseQuota: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := getUserQuota(t, userID)
			relayInfo := resellerBillingRelay(userID, test.requestID, modelName)
			require.Nil(t, PreConsumeBilling(testGinContext(), test.baseQuota, relayInfo))
			require.NotNil(t, relayInfo.Billing)
			relayInfo.Billing.Refund(testGinContext())
			require.Eventually(t, func() bool {
				settlement, err := model.GetResellerRequestSettlement(test.requestID)
				return err == nil && settlement.Status == model.ResellerSettlementStatusRefunded
			}, 2*time.Second, 10*time.Millisecond)
			assert.Equal(t, before, getUserQuota(t, userID))
		})
	}
}

func TestOrdinaryBillingPathKeepsBaseQuotaAndNoSettlement(t *testing.T) {
	truncate(t)
	gin.SetMode(gin.TestMode)
	const userID, initialQuota = 64, 1_000
	seedUser(t, userID, initialQuota)
	require.NoError(t, model.RecordMoziaInitialGiftQuota(userID, initialQuota, "test", "ordinary-billing"))
	relayInfo := resellerBillingRelay(userID, "ordinary-billing-request_64", "ordinary-model")

	require.Nil(t, PreConsumeBilling(testGinContext(), 100, relayInfo))
	assert.Nil(t, relayInfo.ResellerBilling)
	assert.Equal(t, 100, relayInfo.FinalPreConsumedQuota)
	require.NoError(t, SettleBilling(testGinContext(), relayInfo, 150))
	assert.Equal(t, initialQuota-150, getUserQuota(t, userID))
	_, err := model.GetResellerRequestSettlement(relayInfo.RequestId)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

func TestRealtimeReserveUsesRetailQuotaAndFinalSettleDoesNotDoubleCharge(t *testing.T) {
	truncate(t)
	gin.SetMode(gin.TestMode)
	const userID, tokenID, initialQuota = 67, 67, 1_000
	const modelName = "reseller-realtime-model"
	seedResellerBillingFixture(t, userID, initialQuota, modelName)
	seedToken(t, tokenID, userID, "reseller-wss-key", initialQuota)
	originalRatios := ratio_setting.ModelRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(originalRatios))
	})
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"reseller-realtime-model":1}`))

	relayInfo := resellerBillingRelay(userID, "reseller-wss-request_67", modelName)
	relayInfo.IsPlayground = false
	relayInfo.TokenId = tokenID
	relayInfo.TokenKey = "reseller-wss-key"
	relayInfo.PriceData.GroupRatioInfo.GroupRatio = 1
	require.Nil(t, PreConsumeBilling(testGinContext(), 100, relayInfo))
	assert.Equal(t, 120, relayInfo.FinalPreConsumedQuota)

	usage := &dto.RealtimeUsage{
		TotalTokens: 50, InputTokens: 50,
		InputTokenDetails: dto.InputTokenDetails{TextTokens: 50},
	}
	require.NoError(t, PreWssConsumeQuota(testGinContext(), relayInfo, usage))
	assert.Equal(t, 180, relayInfo.FinalPreConsumedQuota, "50 base quota must reserve 60 retail quota")
	assert.Equal(t, initialQuota-180, getUserQuota(t, userID))
	assert.Equal(t, initialQuota-180, getTokenRemainQuota(t, tokenID))

	require.NoError(t, SettleBilling(testGinContext(), relayInfo, 150))
	assert.Equal(t, initialQuota-180, getUserQuota(t, userID), "final settle must target total retail quota, not add it again")
	assert.Equal(t, initialQuota-180, getTokenRemainQuota(t, tokenID))
	settlement, err := model.GetResellerRequestSettlement(relayInfo.RequestId)
	require.NoError(t, err)
	assert.Equal(t, int64(150), settlement.ActualBaseQuota)
	assert.Equal(t, int64(180), settlement.ActualCustomerQuota)
	assert.Equal(t, int64(120), settlement.ActualWholesaleQuota)
}
