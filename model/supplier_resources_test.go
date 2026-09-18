package model

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func verifySupplierResourceMigration(t *testing.T, db *gorm.DB) {
	t.Helper()
	baseline, err := ReadSupplierRoutingConfig()
	require.NoError(t, err)
	var option Option
	require.NoError(t, db.Where(&Option{Key: SupplierRoutingOptionKey}).First(&option).Error)
	require.NoError(t, db.AutoMigrate(&SupplierBinding{}, &SupplierRoutingRule{}))
	stale := Supplier{ID: 900, Name: "absent from active publication"}
	require.NoError(t, db.Create(&stale).Error)
	require.NoError(t, MigrateSupplierResources())
	current, err := ReadSupplierResources(db)
	require.NoError(t, err)
	assert.Equal(t, baseline.Revision, current.Revision)
	assert.Len(t, current.Suppliers, len(baseline.Suppliers))
	assert.Len(t, current.Pools, len(baseline.Pools))
	assert.Len(t, current.Bindings, len(baseline.Bindings))
	for i, s := range current.Suppliers {
		assert.Equal(t, baseline.Suppliers[i].ID, s.ID)
		assert.Equal(t, int64(1), s.Version)
	}
	for i, p := range current.Pools {
		assert.Equal(t, baseline.Pools[i].Models, p.Models)
		assert.Equal(t, baseline.Pools[i].Limits, p.Limits)
	}
	var after Option
	require.NoError(t, db.Where(&Option{Key: SupplierRoutingOptionKey}).First(&after).Error)
	assert.Equal(t, option.Value, after.Value)
	assert.ErrorIs(t, db.First(&Supplier{}, 900).Error, gorm.ErrRecordNotFound)
	created := Supplier{Name: "new supplier", SupplierResourceMeta: SupplierResourceMeta{Version: 1}}
	require.NoError(t, db.Create(&created).Error)
	assert.Greater(t, created.ID, int64(900))
	require.NoError(t, MigrateSupplierResources())
	require.NoError(t, db.First(&created, created.ID).Error)
	assert.ErrorContains(t, PublishSupplierRouting(context.Background(), baseline, baseline.Revision, 1), "retired")
	require.NoError(t, db.AutoMigrate(&Supplier{}, &SupplierPool{}, &SupplierBinding{}, &SupplierRoutingRule{}, &RoutingRevision{}))
}

func TestSupplierResourcesMigrateActivePublicationOnly(t *testing.T) {
	db := setupResellerPricingTestDB(t)
	require.NoError(t, db.AutoMigrate(&Channel{}, &Option{}, &Supplier{}, &SupplierPool{}, &SupplierBinding{}, &SupplierRoutingRule{}, &RoutingRevision{}, &SupplierAttempt{}))
	channel := Channel{Name: "legacy", Models: "test", Type: 1}
	require.NoError(t, db.Create(&channel).Error)
	cfg := SupplierRoutingConfig{Suppliers: []Supplier{{ID: 12, Name: "A"}}, Pools: []SupplierPool{{ID: 24, SupplierID: 12, Name: "P", FailureDomain: "dc", Limits: SupplierLimits{Concurrency: 2, RPM: 10, TPM: 10000}, MaxExecutionSeconds: 60, InputSafetyPercent: 110, Models: []SupplierModelSpec{{Name: "test", Version: "v1", ContextTokens: 1000, MaxOutputTokens: 100}}}}, Bindings: []SupplierBinding{{ChannelID: channel.Id, PoolID: 24, Model: "test"}}}
	require.NoError(t, PublishSupplierRouting(context.Background(), &cfg, 0, 1))
	verifySupplierResourceMigration(t, db)
}

func TestSupplierResourcePatchPreservesExplicitZeroAndArrayReplacement(t *testing.T) {
	rule := SupplierRoutingRule{UserID: 7, MaxSupplierPercent: 60, Targets: []SupplierTarget{{SupplierID: 1, Weight: 40}, {SupplierID: 2, Weight: 60}}, Health: SupplierHealthPolicy{MinSamples: 10, TrialPercent: 20}}
	require.NoError(t, PatchSupplierResource(&rule, []byte(`{"user_id":0,"max_supplier_percent":0,"health":{"trial_percent":10},"targets":[{"supplier_id":2,"weight":100}]}`)))
	assert.Zero(t, rule.UserID)
	assert.Zero(t, rule.MaxSupplierPercent)
	assert.Equal(t, int64(10), rule.Health.MinSamples)
	assert.Equal(t, int64(10), rule.Health.TrialPercent)
	assert.Equal(t, []SupplierTarget{{SupplierID: 2, Weight: 100}}, rule.Targets)
	supplier := Supplier{Name: "existing", Enabled: true, Contact: "old"}
	require.NoError(t, PatchSupplierResource(&supplier, []byte(`{"enabled":false,"contact":""}`)))
	assert.Equal(t, "existing", supplier.Name)
	assert.False(t, supplier.Enabled)
	assert.Empty(t, supplier.Contact)
	for _, patch := range []string{`{"health":{"window":3}}`, `{"targets":[{"supplier_id":1,"typo":2}]}`, `{"id":"overwrite"}`, `{"targets":null}`} {
		assert.Error(t, PatchSupplierResource(&rule, []byte(patch)))
	}
}
