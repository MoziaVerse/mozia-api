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
	err = CheckMoziaQuotaPolicyAccess(1001, "paid-only-model")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrMoziaWalletSourceForbidden))

	require.NoError(t, DB.Create(&UserSubscription{
		UserId:      1001,
		AmountTotal: 100,
		StartTime:   common.GetTimestamp() - 60,
		EndTime:     common.GetTimestamp() + 3600,
		Status:      "active",
	}).Error)
	require.NoError(t, CheckMoziaQuotaPolicyAccess(1001, "paid-only-model"))

	require.NoError(t, DB.Model(&UserSubscription{}).
		Where("user_id = ?", 1001).
		Update("amount_used", 100).Error)
	err = CheckMoziaQuotaPolicyAccess(1001, "paid-only-model")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrMoziaWalletSourceForbidden))
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
