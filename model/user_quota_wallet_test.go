package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdminQuotaAdjustmentMigratesUnclassifiedBalance(t *testing.T) {
	truncateTables(t)
	seedMoziaWalletUser(t, 1060, 100)
	before, after, err := AdjustMoziaUserQuota(1060, "subtract", 30)
	require.NoError(t, err)
	assert.Equal(t, 100, before)
	assert.Equal(t, 70, after)
	assert.Equal(t, 70, getMoziaUserQuota(t, 1060))
	assert.Equal(t, 70, getMoziaSourceBalance(t, 1060, MoziaWalletSourceLegacy))
	history, err := GetMoziaWalletHistory(1060, 0, 50)
	require.NoError(t, err)
	require.Len(t, history.Items, 1)
	assert.Equal(t, MoziaWalletEventAdjust, history.Items[0].EventType)
	assert.Equal(t, -30, history.Items[0].Delta)
}

func TestUserProfileUpdatesPreserveWalletQuota(t *testing.T) {
	truncateTables(t)
	seedMoziaWalletUser(t, 1061, 100)
	require.NoError(t, RecordMoziaInitialGiftQuota(1061, 100, "test", "initial"))
	stale, err := GetUserById(1061, false)
	require.NoError(t, err)
	_, _, err = AdjustMoziaUserQuota(1061, "override", 0)
	require.NoError(t, err)

	stale.Status = common.UserStatusDisabled
	require.NoError(t, stale.Update(false))
	assert.Zero(t, getMoziaUserQuota(t, 1061))
	assert.Zero(t, stale.Quota, "cache update must use the current quota")
	assert.Zero(t, getMoziaSourceBalance(t, 1061, MoziaWalletSourceGift))
	current, err := GetUserById(1061, false)
	require.NoError(t, err)
	assert.Equal(t, common.UserStatusDisabled, current.Status)

	current.Quota = 999 // A profile-edit payload must not become another quota write path.
	current.DisplayName = "Edited profile"
	require.NoError(t, current.Edit(false))
	assert.Zero(t, getMoziaUserQuota(t, 1061))
	view, err := GetMoziaWalletView(1061)
	require.NoError(t, err)
	assert.Zero(t, view.Total)
}
