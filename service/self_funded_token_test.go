package service

import (
	"sync"
	"sync/atomic"
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
	require.NoError(t, info.Billing.Reserve(350))
	assert.Equal(t, 150, getTokenRemainQuota(t, 9003))
	err := info.Billing.Reserve(501)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "token quota is not enough")
	assert.Equal(t, 350, info.FinalPreConsumedQuota, "failed reserve must not change refundable quota")
	assert.Equal(t, 150, getTokenRemainQuota(t, 9003))
	assert.Equal(t, 0, getUserQuota(t, 903))
	info.Billing.Refund(testGinContext())
	require.Eventually(t, func() bool { return getTokenRemainQuota(t, 9003) == 500 }, 3*time.Second, 20*time.Millisecond)
	assert.Equal(t, 0, getTokenUsedQuota(t, 9003), "refund includes the successful extra reserve only")
	assert.Equal(t, 0, getUserQuota(t, 903))
}

func TestSelfFundedToken_RealtimeReserveWithZeroWallet(t *testing.T) {
	truncate(t)
	gin.SetMode(gin.TestMode)
	seedUser(t, 908, 0)
	seedSelfFundedToken(t, 9008, 908, "sk-sf-wss", 1_000)
	originalRatios := ratio_setting.ModelRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(originalRatios))
	})
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"self-funded-realtime":1}`))
	info := selfFundedRelay(908, 9008, "sk-sf-wss", "wallet_only")
	info.OriginModelName = "self-funded-realtime"
	info.PriceData.GroupRatioInfo.GroupRatio = 1
	require.Nil(t, PreConsumeBilling(testGinContext(), 100, info))
	usage := &dto.RealtimeUsage{
		TotalTokens: 50, InputTokens: 50,
		InputTokenDetails: dto.InputTokenDetails{TextTokens: 50},
	}
	require.NoError(t, PreWssConsumeQuota(testGinContext(), info, usage))
	assert.Equal(t, 150, info.FinalPreConsumedQuota)
	assert.Equal(t, 850, getTokenRemainQuota(t, 9008))
	assert.Equal(t, 0, getUserQuota(t, 908))

	// Final usage is 50; the initial reservation must not become an extra charge.
	require.NoError(t, SettleBilling(testGinContext(), info, 50))
	assert.Equal(t, 950, getTokenRemainQuota(t, 9008))
	assert.Equal(t, 50, getTokenUsedQuota(t, 9008))
	assert.Equal(t, 0, getUserQuota(t, 908))
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

// 复现验收 P1：余额 500，两笔各 300 的预扣。旧实现先读后写，两笔都通过检查，余额变 -100。
// 原子条件扣减下只能成功一笔，余额剩 200，失败的一笔不产生扣款。
func TestSelfFundedToken_ConcurrentPreConsumeCannotOverdraw(t *testing.T) {
	truncate(t)
	seedUser(t, 905, 0)
	seedSelfFundedToken(t, 9005, 905, "sk-sf-5", 500)

	// 模拟两笔请求都已通过"余额足够"的读取判断后再落扣减
	first := model.DecreaseSelfFundedTokenQuota(9005, "sk-sf-5", 300)
	second := model.DecreaseSelfFundedTokenQuota(9005, "sk-sf-5", 300)
	require.NoError(t, first)
	require.ErrorIs(t, second, model.ErrTokenQuotaInsufficient)
	assert.Equal(t, 200, getTokenRemainQuota(t, 9005))
	assert.Equal(t, 300, getTokenUsedQuota(t, 9005))

	// 真并发：N 个 goroutine 同时抢 200 余额，每笔 100，最多两笔成功
	var wg sync.WaitGroup
	var okCount int32
	start := make(chan struct{})
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if err := model.DecreaseSelfFundedTokenQuota(9005, "sk-sf-5", 100); err == nil {
				atomic.AddInt32(&okCount, 1)
			}
		}()
	}
	close(start)
	wg.Wait()
	assert.Equal(t, int32(2), okCount)
	assert.Equal(t, 0, getTokenRemainQuota(t, 9005), "余额永不为负")
}

// 走完整预扣入口：第二笔被拒且返回额度不足
func TestSelfFundedToken_PreConsumeUsesAtomicPath(t *testing.T) {
	truncate(t)
	gin.SetMode(gin.TestMode)
	seedUser(t, 906, 0)
	seedSelfFundedToken(t, 9006, 906, "sk-sf-6", 500)
	info := selfFundedRelay(906, 9006, "sk-sf-6", "wallet_first")
	require.NoError(t, PreConsumeTokenQuota(info, 300))
	err := PreConsumeTokenQuota(info, 300)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "token quota is not enough")
	assert.Equal(t, 200, getTokenRemainQuota(t, 9006))
}

// 不经 BillingSession 的旧结算入口（违规罚金等）也只动 key，不碰钱包
func TestSelfFundedToken_PostConsumeQuotaSkipsWallet(t *testing.T) {
	truncate(t)
	seedUser(t, 907, 0)
	seedSelfFundedToken(t, 9007, 907, "sk-sf-7", 1_000)
	info := selfFundedRelay(907, 9007, "sk-sf-7", "wallet_first")
	info.BillingSource = BillingSourceToken
	require.NoError(t, PostConsumeQuota(info, 400, 0, false))
	assert.Equal(t, 600, getTokenRemainQuota(t, 9007))
	assert.Equal(t, 0, getUserQuota(t, 907), "钱包不动")
}
