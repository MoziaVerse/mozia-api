package service

import (
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedSelfFundedToken(t *testing.T, id, userId int, key string, remain int) {
	t.Helper()
	tk := &model.Token{Id: id, UserId: userId, Key: key, Name: "pack", Status: common.TokenStatusEnabled, RemainQuota: remain, SelfFunded: true}
	require.NoError(t, model.DB.Create(tk).Error)
}

func selfFundedRelay(userId, tokenId int, key string, pref string) *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		UserId: userId, TokenId: tokenId, TokenKey: key, TokenSelfFunded: true,
		RequestId: "sf-" + key, OriginModelName: "minimax/minimax-h3-t2va",
		UserSetting: dto.UserSetting{BillingPreference: pref},
	}
}

// 钱包 0、无订阅，只有一把自带资金的 key：预扣与结算只动 key 余量
func TestSelfFundedToken_ChargesKeyOnly(t *testing.T) {
	truncate(t)
	gin.SetMode(gin.TestMode)
	seedUser(t, 901, 0)
	seedSelfFundedToken(t, 9001, 901, "sk-sf-1", 1_000)
	info := selfFundedRelay(901, 9001, "sk-sf-1", "subscription_first")

	require.Nil(t, PreConsumeBilling(testGinContext(), 300, info))
	assert.Equal(t, BillingSourceToken, info.BillingSource)
	assert.Equal(t, 700, getTokenRemainQuota(t, 9001))
	assert.Equal(t, 0, getUserQuota(t, 901))

	require.NoError(t, SettleBilling(testGinContext(), info, 450))
	assert.Equal(t, 550, getTokenRemainQuota(t, 9001))
	assert.Equal(t, 0, getUserQuota(t, 901), "钱包始终不动")
}

// 余量不足直接拒绝，不回落钱包（哪怕钱包有钱）
func TestSelfFundedToken_NoFallbackToWallet(t *testing.T) {
	truncate(t)
	gin.SetMode(gin.TestMode)
	seedUser(t, 902, 5_000)
	seedSelfFundedToken(t, 9002, 902, "sk-sf-2", 100)
	info := selfFundedRelay(902, 9002, "sk-sf-2", "wallet_first")

	apiErr := PreConsumeBilling(testGinContext(), 300, info)
	require.NotNil(t, apiErr)
	assert.Equal(t, 100, getTokenRemainQuota(t, 9002))
	assert.Equal(t, 5_000, getUserQuota(t, 902))
}

// 请求失败退款：只退 key
func TestSelfFundedToken_RefundReturnsToKey(t *testing.T) {
	truncate(t)
	gin.SetMode(gin.TestMode)
	seedUser(t, 903, 0)
	seedSelfFundedToken(t, 9003, 903, "sk-sf-3", 500)
	info := selfFundedRelay(903, 9003, "sk-sf-3", "subscription_first")
	require.Nil(t, PreConsumeBilling(testGinContext(), 200, info))
	assert.Equal(t, 300, getTokenRemainQuota(t, 9003))
	info.Billing.Refund(testGinContext())
	require.Eventually(t, func() bool { return getTokenRemainQuota(t, 9003) == 500 }, 3*time.Second, 20*time.Millisecond)
	assert.Equal(t, 0, getUserQuota(t, 903))
}

// 异步任务：资金分支不碰钱包，令牌分支照常
func TestSelfFundedToken_TaskFundingIsNoop(t *testing.T) {
	truncate(t)
	seedUser(t, 904, 0)
	task := &model.Task{UserId: 904, Quota: 400}
	task.PrivateData.BillingSource = BillingSourceToken
	require.NoError(t, taskAdjustFunding(task, -400))
	require.NoError(t, taskAdjustFunding(task, 50))
	assert.Equal(t, 0, getUserQuota(t, 904))
}
