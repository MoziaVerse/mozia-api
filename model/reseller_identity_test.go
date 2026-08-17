package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClaimAndResolveResellerVerifiedIdentity(t *testing.T) {
	db := setupResellerPricingTestDB(t)
	require.NoError(t, db.AutoMigrate(&ResellerIdentityRoute{}, &ResellerAssignmentConflict{}))
	target := Reseller{Name: "HDU", Status: ResellerStatusActive}
	current := Reseller{Name: "Current", Status: ResellerStatusActive}
	require.NoError(t, db.Create(&target).Error)
	require.NoError(t, db.Create(&current).Error)
	_, err := UpsertHduResellerIdentityRoute(target.Id)
	require.NoError(t, err)

	claim := func(subject string) *ResellerVerifiedIdentityClaimRecord {
		record, claimErr := ClaimHduResellerIdentity(subject, "", "")
		require.NoError(t, claimErr)
		return record
	}

	assigned := claim("unowned-subject")
	assert.Equal(t, ResellerVerifiedIdentityClaimAssigned, assigned.Status)
	assert.Equal(t, ResellerVerifiedIdentityClaimAlreadyInTarget, claim("unowned-subject").Status)
	var customer ResellerCustomer
	require.NoError(t, db.Where("subject = ?", "unowned-subject").Take(&customer).Error)

	require.NoError(t, db.Create(&ResellerCustomer{
		ResellerId: current.Id, Subject: "conflict-subject", Status: ResellerCustomerStatusActive,
	}).Error)
	conflicted := claim("conflict-subject")
	require.NotNil(t, conflicted.ConflictId)
	assert.Equal(t, ResellerVerifiedIdentityClaimOwnedByOther, conflicted.Status)

	resolved, err := ResolveResellerAssignmentConflict(*conflicted.ConflictId, ResellerAssignmentConflictActionTransfer, "mega-actor")
	require.NoError(t, err)
	assert.Equal(t, ResellerAssignmentConflictTransferred, resolved.Status)
	customer = ResellerCustomer{}
	require.NoError(t, db.Where("subject = ?", "conflict-subject").Take(&customer).Error)
	assert.Equal(t, target.Id, customer.ResellerId)
}
