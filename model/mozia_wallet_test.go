package model

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedMoziaWalletUser(t *testing.T, id int, quota int) {
	t.Helper()
	user := &User{
		Id:       id,
		Username: "mozia_wallet_user",
		Quota:    quota,
		Status:   common.UserStatusEnabled,
	}
	require.NoError(t, DB.Create(user).Error)
}

func getMoziaSourceBalance(t *testing.T, userId int, source string) int {
	t.Helper()
	var balance MoziaWalletBalance
	require.NoError(t, DB.Where("user_id = ? AND source = ?", userId, source).First(&balance).Error)
	return balance.Balance
}

func getMoziaUserQuota(t *testing.T, userId int) int {
	t.Helper()
	var user User
	require.NoError(t, DB.Select("quota").Where("id = ?", userId).First(&user).Error)
	return user.Quota
}

func TestMoziaQuotaPolicyPaidOnlyRejectsGiftOnly(t *testing.T) {
	truncateTables(t)

	seedMoziaWalletUser(t, 1001, 200)
	require.NoError(t, RecordMoziaInitialGiftQuota(1001, 200, "test", "gift-only"))
	require.NoError(t, CreateMoziaModelQuotaPolicy(&MoziaModelQuotaPolicy{
		ModelPattern:   "paid-only-model",
		MatchType:      MoziaQuotaPolicyMatchExact,
		AllowedSources: MoziaWalletSourcePaid,
		ConsumeOrder:   MoziaQuotaPolicyConsumePaidFirst,
		Enabled:        true,
	}))

	err := CheckMoziaQuotaPolicyAccess(1001, "paid-only-model")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrMoziaWalletSourceForbidden))

	require.NoError(t, GrantMoziaWalletQuota(MoziaWalletGrantInput{
		UserId:        1001,
		Source:        MoziaWalletSourcePaid,
		Amount:        50,
		EventType:     MoziaWalletEventTopUp,
		ReferenceType: "test",
		ReferenceId:   "paid-topup",
	}))
	require.NoError(t, CheckMoziaQuotaPolicyAccess(1001, "paid-only-model"))
}

func TestMoziaWalletReservationUsesPaidOnlyPolicy(t *testing.T) {
	truncateTables(t)

	seedMoziaWalletUser(t, 1002, 100)
	require.NoError(t, RecordMoziaInitialGiftQuota(1002, 100, "test", "gift"))
	require.NoError(t, GrantMoziaWalletQuota(MoziaWalletGrantInput{
		UserId:        1002,
		Source:        MoziaWalletSourcePaid,
		Amount:        50,
		EventType:     MoziaWalletEventTopUp,
		ReferenceType: "test",
		ReferenceId:   "paid",
	}))
	require.NoError(t, CreateMoziaModelQuotaPolicy(&MoziaModelQuotaPolicy{
		ModelPattern:   "premium-model",
		MatchType:      MoziaQuotaPolicyMatchExact,
		AllowedSources: MoziaWalletSourcePaid,
		ConsumeOrder:   MoziaQuotaPolicyConsumePaidFirst,
		Enabled:        true,
	}))

	require.NoError(t, ReserveMoziaWalletQuota("req-paid-only", 1002, "premium-model", 30))
	assert.Equal(t, 100, getMoziaSourceBalance(t, 1002, MoziaWalletSourceGift))
	assert.Equal(t, 20, getMoziaSourceBalance(t, 1002, MoziaWalletSourcePaid))
	assert.Equal(t, 120, getMoziaUserQuota(t, 1002))

	require.NoError(t, SettleMoziaWalletReservation("req-paid-only", 1002, "premium-model", 10))
	assert.Equal(t, 100, getMoziaSourceBalance(t, 1002, MoziaWalletSourceGift))
	assert.Equal(t, 40, getMoziaSourceBalance(t, 1002, MoziaWalletSourcePaid))
	assert.Equal(t, 140, getMoziaUserQuota(t, 1002))

	require.NoError(t, RefundMoziaWalletReservation("req-paid-only", 1002))
	assert.Equal(t, 100, getMoziaSourceBalance(t, 1002, MoziaWalletSourceGift))
	assert.Equal(t, 50, getMoziaSourceBalance(t, 1002, MoziaWalletSourcePaid))
	assert.Equal(t, 150, getMoziaUserQuota(t, 1002))
}

func TestAdjustMoziaWalletBalance(t *testing.T) {
	truncateTables(t)

	seedMoziaWalletUser(t, 1003, 100)
	require.NoError(t, RecordMoziaInitialGiftQuota(1003, 100, "test", "gift"))

	delta := 50
	wallet, err := AdjustMoziaWalletBalance(MoziaWalletAdjustInput{
		UserId: 1003,
		Source: MoziaWalletSourceGift,
		Delta:  &delta,
		Reason: "support adjustment",
	})
	require.NoError(t, err)
	assert.Equal(t, 150, wallet.Sources[MoziaWalletSourceGift])
	assert.Equal(t, 150, wallet.Total)
	assert.Equal(t, 150, getMoziaUserQuota(t, 1003))

	target := 20
	wallet, err = AdjustMoziaWalletBalance(MoziaWalletAdjustInput{
		UserId:        1003,
		Source:        MoziaWalletSourcePaid,
		TargetBalance: &target,
		Reason:        "paid correction",
	})
	require.NoError(t, err)
	assert.Equal(t, 20, wallet.Sources[MoziaWalletSourcePaid])
	assert.Equal(t, 170, wallet.Total)
	assert.Equal(t, 170, getMoziaUserQuota(t, 1003))

	tooMuch := -200
	_, err = AdjustMoziaWalletBalance(MoziaWalletAdjustInput{
		UserId: 1003,
		Source: MoziaWalletSourceGift,
		Delta:  &tooMuch,
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrMoziaWalletInsufficient))
}

func TestGetMoziaWalletConsumptionNetsRefundAgainstConsume(t *testing.T) {
	truncateTables(t)

	seedMoziaWalletUser(t, 1010, 0)
	require.NoError(t, RecordMoziaInitialGiftQuota(1010, 100, "test", "gift"))
	require.NoError(t, GrantMoziaWalletQuota(MoziaWalletGrantInput{
		UserId:        1010,
		Source:        MoziaWalletSourcePaid,
		Amount:        200,
		EventType:     MoziaWalletEventTopUp,
		ReferenceType: "test",
		ReferenceId:   "paid",
	}))

	// 预扣 150:先扣完 100 赠送,再扣 50 付费(默认 gift_first)
	require.NoError(t, ReserveMoziaWalletQuota("req-consume", 1010, "any-model", 150))
	// 实际只用了 120,多预扣的 30 退回付费来源
	require.NoError(t, SettleMoziaWalletReservation("req-consume", 1010, "any-model", 120))

	view, err := GetMoziaWalletConsumption(1010, 0, 0)
	require.NoError(t, err)
	// 净消费必须是 120 而不是预扣的 150 —— 只累加 consume 会高估
	assert.Equal(t, 120, view.Total)
	assert.Equal(t, 100, view.Sources[MoziaWalletSourceGift])
	assert.Equal(t, 20, view.Sources[MoziaWalletSourcePaid])
	assert.Equal(t, 0, view.Sources[MoziaWalletSourceLegacy])
	assert.Positive(t, view.LedgerStartTime)
}

func TestGetMoziaWalletConsumptionExcludesTopUpAndAdjust(t *testing.T) {
	truncateTables(t)

	seedMoziaWalletUser(t, 1011, 0)
	require.NoError(t, GrantMoziaWalletQuota(MoziaWalletGrantInput{
		UserId:        1011,
		Source:        MoziaWalletSourcePaid,
		Amount:        500,
		EventType:     MoziaWalletEventTopUp,
		ReferenceType: "test",
		ReferenceId:   "paid",
	}))
	delta := 300
	_, err := AdjustMoziaWalletBalance(MoziaWalletAdjustInput{
		UserId: 1011,
		Source: MoziaWalletSourcePaid,
		Delta:  &delta,
		Reason: "人工调账",
	})
	require.NoError(t, err)

	view, err := GetMoziaWalletConsumption(1011, 0, 0)
	require.NoError(t, err)
	// 充值与人工调账都不是消费。这也是不能用「充值合计 - 当前余额」推导消费的原因:
	// 调账会让推导值失真,而这里按事件类型统计不受影响。
	assert.Equal(t, 0, view.Total)
}

func TestGetMoziaWalletConsumptionRespectsTimeRange(t *testing.T) {
	truncateTables(t)

	seedMoziaWalletUser(t, 1012, 0)
	require.NoError(t, GrantMoziaWalletQuota(MoziaWalletGrantInput{
		UserId:        1012,
		Source:        MoziaWalletSourcePaid,
		Amount:        500,
		EventType:     MoziaWalletEventTopUp,
		ReferenceType: "test",
		ReferenceId:   "paid",
	}))
	require.NoError(t, ReserveMoziaWalletQuota("req-range", 1012, "any-model", 80))
	require.NoError(t, SettleMoziaWalletReservation("req-range", 1012, "any-model", 80))

	now := common.GetTimestamp()
	view, err := GetMoziaWalletConsumption(1012, 0, now)
	require.NoError(t, err)
	assert.Equal(t, 80, view.Total)

	// 窗口落在消费之后,统计为 0
	view, err = GetMoziaWalletConsumption(1012, now+3600, 0)
	require.NoError(t, err)
	assert.Equal(t, 0, view.Total)
}

func TestGetMoziaWalletConsumptionRejectsEmptyUser(t *testing.T) {
	_, err := GetMoziaWalletConsumption(0, 0, 0)
	assert.Error(t, err)
}
