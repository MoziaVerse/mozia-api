package service

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestBillingRejectionLogsAndRollback(t *testing.T) {
	for _, table := range []interface{}{&model.SubscriptionPlan{}, &model.SubscriptionPreConsumeRecord{}} {
		if !model.DB.Migrator().HasTable(table) {
			require.NoError(t, model.DB.AutoMigrate(table))
		}
	}
	for _, tc := range []struct {
		name         string
		paid         int
		tokenQuota   int
		preConsume   int
		preference   string
		subscription bool
		databaseFail bool
		logsDisabled bool
		wantCode     types.ErrorCode
	}{
		{name: "paid balance below reservation", paid: 50, tokenQuota: 1000, preConsume: 100, wantCode: types.ErrorCodeInsufficientUserQuota},
		{name: "total balance insufficient", paid: 50, tokenQuota: 1000, preConsume: 200, wantCode: types.ErrorCodeInsufficientUserQuota},
		{name: "token quota insufficient", paid: 150, tokenQuota: 20, preConsume: 100, wantCode: types.ErrorCodePreConsumeTokenQuotaFailed},
		{name: "quota policy denies gift balance", tokenQuota: 1000, preConsume: 100, wantCode: types.ErrorCodeInsufficientUserQuota},
		{name: "subscription unavailable", paid: 50, tokenQuota: 1000, preConsume: 100, preference: "subscription_only", wantCode: types.ErrorCodeInsufficientUserQuota},
		{name: "wallet fallback succeeds", paid: 50, tokenQuota: 1000, preConsume: 100, preference: "wallet_first", subscription: true},
		{name: "database failure stays server error", paid: 150, tokenQuota: 1000, preConsume: 100, databaseFail: true, wantCode: types.ErrorCodeUpdateDataError},
		{name: "error log setting respected", paid: 50, tokenQuota: 1000, preConsume: 100, logsDisabled: true, wantCode: types.ErrorCodeInsufficientUserQuota},
	} {
		t.Run(tc.name, func(t *testing.T) {
			truncate(t)
			previous := constant.ErrorLogEnabled
			constant.ErrorLogEnabled = !tc.logsDisabled
			t.Cleanup(func() { constant.ErrorLogEnabled = previous })
			t.Cleanup(func() {
				require.NoError(t, model.DB.Where("1 = 1").Delete(&model.SubscriptionPlan{}).Error)
				require.NoError(t, model.DB.Where("1 = 1").Delete(&model.SubscriptionPreConsumeRecord{}).Error)
			})
			const userID, tokenID = 81, 82
			const modelName = "billing-rejection-model"
			seedUser(t, userID, 100)
			seedToken(t, tokenID, userID, "billing-test-key", tc.tokenQuota)
			require.NoError(t, model.RecordMoziaInitialGiftQuota(userID, 100, "test", "gift"))
			if tc.paid > 0 {
				require.NoError(t, model.GrantMoziaWalletQuota(model.MoziaWalletGrantInput{
					UserId: userID, Source: model.MoziaWalletSourcePaid, Amount: tc.paid,
					EventType: model.MoziaWalletEventTopUp, ReferenceType: "test", ReferenceId: "paid",
				}))
			}
			require.NoError(t, model.CreateMoziaModelQuotaPolicy(&model.MoziaModelQuotaPolicy{
				ModelPattern: modelName, MatchType: model.MoziaQuotaPolicyMatchExact,
				AllowedSources: model.MoziaWalletSourcePaid, ConsumeOrder: model.MoziaQuotaPolicyConsumePaidFirst, Enabled: true,
			}))
			if tc.subscription {
				plan := model.SubscriptionPlan{Title: "test", QuotaResetPeriod: "never"}
				require.NoError(t, model.DB.Create(&plan).Error)
				seedSubscription(t, 83, userID, 1000, 0)
				require.NoError(t, model.DB.Model(&model.UserSubscription{}).Where("id = ?", 83).Update("plan_id", plan.Id).Error)
			}
			if tc.databaseFail {
				require.NoError(t, model.DB.Callback().Create().Before("gorm:create").Register("test:wallet_failure", func(tx *gorm.DB) {
					if tx.Statement.Table == "mozia_wallet_reservations" {
						tx.AddError(errors.New("wallet database unavailable"))
					}
				}))
				t.Cleanup(func() { require.NoError(t, model.DB.Callback().Create().Remove("test:wallet_failure")) })
			}

			info := resellerBillingRelay(userID, "billing-error-request", modelName)
			info.IsPlayground, info.TokenId, info.TokenKey = false, tokenID, "billing-test-key"
			if tc.preference != "" {
				info.UserSetting.BillingPreference = tc.preference
			}
			c := testGinContext()
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"billing-rejection-model","api_key":"secret"}`))
			c.Request.Header.Set("Content-Type", "application/json")
			c.Set(common.RequestIdKey, info.RequestId)
			c.Set("id", userID)
			c.Set("token_id", tokenID)
			c.Set("token_name", "test_token")
			c.Set("group", "default")
			c.Set("channel_id", 99)
			apiErr := EnforceMoziaQuotaPolicy(c, userID, modelName)
			if apiErr == nil {
				apiErr = PreConsumeBilling(c, tc.preConsume, info)
			}
			assert.Equal(t, 100+tc.paid, getUserQuota(t, userID), "failed wallet reservation must roll back")
			if tc.wantCode == "" {
				require.Nil(t, apiErr)
				assert.Equal(t, BillingSourceSubscription, info.BillingSource)
				assert.Equal(t, int64(tc.preConsume), getSubscriptionUsed(t, 83))
				assert.Equal(t, tc.tokenQuota-tc.preConsume, getTokenRemainQuota(t, tokenID))
				assert.Zero(t, countLogs(t), "successful funding fallback must not record a rejection")
				return
			}
			require.NotNil(t, apiErr)
			assert.Equal(t, tc.wantCode, apiErr.GetErrorCode())
			assert.True(t, types.IsSkipRetryError(apiErr))
			assert.Equal(t, tc.tokenQuota, getTokenRemainQuota(t, tokenID), "failed funding must restore token quota")
			if tc.databaseFail {
				assert.Equal(t, http.StatusInternalServerError, apiErr.StatusCode)
				return
			}
			assert.Equal(t, http.StatusForbidden, apiErr.StatusCode)
			if tc.logsDisabled {
				assert.Zero(t, countLogs(t))
				return
			}
			require.Equal(t, int64(1), countLogs(t))
			entry := getLastLog(t)
			require.NotNil(t, entry)
			assert.Equal(t, model.LogTypeError, entry.Type)
			assert.Equal(t, info.RequestId, entry.RequestId)
			assert.Equal(t, modelName, entry.ModelName)
			assert.Equal(t, tokenID, entry.TokenId)
			assert.Zero(t, entry.ChannelId)
			assert.Zero(t, entry.Quota)
			var other map[string]interface{}
			require.NoError(t, common.UnmarshalJsonStr(entry.Other, &other))
			assert.Equal(t, "billing", other["error_stage"])
			assert.Equal(t, string(tc.wantCode), other["error_code"])
			assert.Equal(t, float64(http.StatusForbidden), other["status_code"])
			assert.Equal(t, c.Request.URL.Path, other["request_path"])
			assert.NotContains(t, entry.Other, "secret")
			assert.Contains(t, entry.Other, "[REDACTED]")
		})
	}
}

func TestBillingReserveInsufficientKeepsExistingReservation(t *testing.T) {
	truncate(t)
	seedUser(t, 84, 150)
	seedToken(t, 85, 84, "reserve-test-key", 1000)
	info := resellerBillingRelay(84, "reserve-quota-request", "reserve-quota-model")
	info.IsPlayground, info.TokenId, info.TokenKey = false, 85, "reserve-test-key"
	require.Nil(t, PreConsumeBilling(testGinContext(), 100, info))
	err := info.Billing.Reserve(200)
	var apiErr *types.NewAPIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusForbidden, apiErr.StatusCode)
	assert.Equal(t, types.ErrorCodeInsufficientUserQuota, apiErr.GetErrorCode())
	assert.Equal(t, 50, getUserQuota(t, 84))
	assert.Equal(t, 900, getTokenRemainQuota(t, 85))
	assert.Equal(t, 100, info.Billing.GetPreConsumedQuota())
}
