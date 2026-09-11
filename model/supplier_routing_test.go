package model

import (
	"context"
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestSupplierPublicationPreservesChannelSettingsAndRejectsStaleEditor(t *testing.T) {
	db := setupResellerPricingTestDB(t)
	require.NoError(t, db.AutoMigrate(&Channel{}, &Option{}, &Supplier{}, &SupplierPool{}, &RoutingRevision{}, &SupplierAttempt{}))
	ch := Channel{Name: "supplier", Type: constant.ChannelTypeOpenAI, Models: "test", Status: common.ChannelStatusEnabled, OtherSettings: `{"disable_store":true}`}
	require.NoError(t, db.Create(&ch).Error)
	cfg := SupplierRoutingConfig{Suppliers: []Supplier{{ID: 1, Name: "A", Enabled: true}}, Pools: []SupplierPool{{ID: 1, SupplierID: 1, Name: "pool", FailureDomain: "dc-a", Enabled: true, Limits: SupplierLimits{Concurrency: 10, RPM: 100, TPM: 10000}, MaxExecutionSeconds: 60, InputSafetyPercent: 110, Acceptance: "load-test-1", Models: []SupplierModelSpec{{Name: "test", Version: "v1", ContextTokens: 1000, MaxOutputTokens: 100}}}}, Bindings: []SupplierBinding{{ChannelID: ch.Id, Model: "test", PoolID: 1}}}
	require.NoError(t, ValidateSupplierRoutingConfig(&cfg))
	require.NoError(t, PublishSupplierRouting(context.Background(), &cfg, 0, 7))
	assert.Positive(t, cfg.Revision)
	require.NoError(t, db.First(&ch, ch.Id).Error)
	assert.True(t, ch.GetOtherSettings().DisableStore)
	assert.Equal(t, int64(1), ch.GetOtherSettings().SupplierPools["test"])
	assert.Equal(t, int64(1), ch.SupplierID)
	assert.ErrorContains(t, PublishSupplierRouting(context.Background(), &cfg, 0, 7), "changed")
	current, err := ReadSupplierRoutingConfig()
	require.NoError(t, err)
	assert.Equal(t, cfg.Revision, current.Revision)
	cfg.Pools[0].Limits.Concurrency = 0
	assert.ErrorContains(t, ValidateSupplierRoutingConfig(&cfg), "positive")
}

func TestSupplierDatabaseCompatibility(t *testing.T) {
	for _, target := range []struct {
		name, env string
		kind      common.DatabaseType
	}{
		{"mysql57", "SUPPLIER_TEST_MYSQL_DSN", common.DatabaseTypeMySQL},
		{"postgres96", "SUPPLIER_TEST_POSTGRES_DSN", common.DatabaseTypePostgreSQL},
	} {
		t.Run(target.name, func(t *testing.T) {
			dsn := os.Getenv(target.env)
			if dsn == "" {
				t.Skip("set " + target.env + " to a disposable test database")
			}
			var dialect gorm.Dialector = mysql.Open(dsn)
			if target.kind == common.DatabaseTypePostgreSQL {
				dialect = postgres.Open(dsn)
			}
			db, err := gorm.Open(dialect, &gorm.Config{})
			require.NoError(t, err)
			savedDB, savedType := DB, common.MainDatabaseType()
			DB = db
			common.SetDatabaseTypes(target.kind, common.LogDatabaseType())
			initCol()
			t.Cleanup(func() {
				DB = savedDB
				common.SetDatabaseTypes(savedType, common.LogDatabaseType())
				initCol()
				sqlDB, e := db.DB()
				if e == nil {
					_ = sqlDB.Close()
				}
			})
			tables := []any{&Channel{}, &Option{}, &Supplier{}, &SupplierPool{}, &RoutingRevision{}, &SupplierAttempt{}}
			require.NoError(t, db.Migrator().DropTable(tables...))
			require.NoError(t, db.AutoMigrate(tables...))
			require.NoError(t, db.AutoMigrate(tables...))
			channel := Channel{Name: "supplier-db", Type: constant.ChannelTypeOpenAI, Models: "test", Status: common.ChannelStatusEnabled}
			require.NoError(t, db.Create(&channel).Error)
			cfg := SupplierRoutingConfig{Suppliers: []Supplier{{ID: 1, Name: "A", Enabled: true}}, Pools: []SupplierPool{{ID: 1, SupplierID: 1, Name: "P", FailureDomain: "dc-a", Enabled: true, Limits: SupplierLimits{Concurrency: 2, RPM: 10, TPM: 10000}, MaxExecutionSeconds: 60, InputSafetyPercent: 110, Acceptance: "verified-test", Models: []SupplierModelSpec{{Name: "test", Version: "v1", ContextTokens: 1000, MaxOutputTokens: 100}}}}, Bindings: []SupplierBinding{{ChannelID: channel.Id, Model: "test", PoolID: 1}}}
			require.NoError(t, ValidateSupplierRoutingConfig(&cfg))
			require.NoError(t, PublishSupplierRouting(context.Background(), &cfg, 0, 1))
			first := cfg.Revision
			cfg.Pools[0].Enabled = false
			cfg.Pools[0].Limits.Concurrency = 1
			require.NoError(t, PublishSupplierRouting(context.Background(), &cfg, first, 1))
			read, err := ReadSupplierRoutingConfig()
			require.NoError(t, err)
			assert.False(t, read.Pools[0].Enabled)
			assert.Equal(t, int64(1), read.Pools[0].Limits.Concurrency)
			var persisted SupplierPool
			require.NoError(t, db.First(&persisted, 1).Error)
			assert.False(t, persisted.Enabled)
			attempt := SupplierAttempt{RequestID: "db-contract", Attempt: 1, ChannelID: channel.Id, PoolID: 1, Status: "pending", PriceJSON: `{"currency":"CNY"}`}
			require.NoError(t, db.Create(&attempt).Error)
			duplicate := SupplierAttempt{RequestID: "db-contract", Attempt: 1}
			assert.Error(t, db.Create(&duplicate).Error)
			cfg.Bindings = nil
			assert.ErrorContains(t, PublishSupplierRouting(context.Background(), &cfg, cfg.Revision, 1), "pending")
		})
	}
}

func TestSupplierCandidatesPreserveGroupAndChannelStatusAcrossCacheModes(t *testing.T) {
	db := setupResellerPricingTestDB(t)
	require.NoError(t, db.AutoMigrate(&Channel{}, &Ability{}))
	channels := []Channel{
		{Id: 1, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Models: "test", Group: "default"},
		{Id: 2, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusEnabled, Models: "test", Group: "premium"},
		{Id: 3, Type: constant.ChannelTypeOpenAI, Status: common.ChannelStatusManuallyDisabled, Models: "test", Group: "default"},
	}
	require.NoError(t, db.Create(&channels).Error)
	for _, channel := range channels {
		require.NoError(t, db.Create(&Ability{Group: channel.Group, Model: "test", ChannelId: channel.Id, Enabled: channel.Status == common.ChannelStatusEnabled}).Error)
	}
	old := common.MemoryCacheEnabled
	channelSyncLock.RLock()
	oldGroups, oldChannels, oldCustom := group2model2channels, channelsIDM, channel2advancedCustomConfig
	channelSyncLock.RUnlock()
	t.Cleanup(func() {
		common.MemoryCacheEnabled = old
		channelSyncLock.Lock()
		group2model2channels, channelsIDM, channel2advancedCustomConfig = oldGroups, oldChannels, oldCustom
		channelSyncLock.Unlock()
	})
	for _, cached := range []bool{false, true} {
		common.MemoryCacheEnabled = cached
		if cached {
			InitChannelCache()
		}
		candidates, err := GetSatisfiedChannelCandidates("default", "test", "/v1/chat/completions")
		require.NoError(t, err)
		require.Len(t, candidates, 1)
		assert.Equal(t, 1, candidates[0].Id)
		candidates, err = GetSatisfiedChannelCandidates("unknown-group", "test", "/v1/chat/completions")
		require.NoError(t, err)
		assert.Empty(t, candidates)
	}
}
