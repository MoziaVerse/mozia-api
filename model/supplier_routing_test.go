package model

import (
	"context"
	"os"
	"strings"
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
	require.NoError(t, ValidateSupplierRoutingConfig(&cfg))
	cfg.Pools[0].Limits.Concurrency = -1
	assert.ErrorContains(t, ValidateSupplierRoutingConfig(&cfg), "non-negative")
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
			tables := []any{&Channel{}, &Option{}, &Supplier{}, &SupplierPool{}, &SupplierBinding{}, &SupplierRoutingRule{}, &RoutingRevision{}, &SupplierAttempt{}}
			require.NoError(t, db.Migrator().DropTable(tables...))
			require.NoError(t, db.AutoMigrate(tables...))
			require.NoError(t, db.AutoMigrate(tables...))
			channel := Channel{Name: "supplier-db", Type: constant.ChannelTypeOpenAI, Models: "test", Status: common.ChannelStatusEnabled}
			require.NoError(t, db.Create(&channel).Error)
			cfg := SupplierRoutingConfig{Suppliers: []Supplier{{ID: 1, Name: "A", Enabled: true}}, Pools: []SupplierPool{{ID: 1, SupplierID: 1, Name: "P", FailureDomain: "dc-a", Enabled: true, Limits: SupplierLimits{Concurrency: 2, RPM: 10, TPM: 10000}, MaxExecutionSeconds: 60, InputSafetyPercent: 110, Acceptance: "verified-test", Models: []SupplierModelSpec{{Name: "test", Version: "v1", ContextTokens: 1000, MaxOutputTokens: 100}}}}, Bindings: []SupplierBinding{{ChannelID: channel.Id, Model: "test", PoolID: 1}}}
			// The bounded supplier graph can exceed MySQL TEXT's 64 KiB limit.
			for id := int64(101); id < 106; id++ {
				cfg.Suppliers = append(cfg.Suppliers, Supplier{ID: id, Name: "large profile", Terms: strings.Repeat("x", 16000)})
			}

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
			verifySupplierResourceMigration(t, db)
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

func TestSupplierComparableProcurementQuotes(t *testing.T) {
	for _, tc := range []struct {
		name  string
		price ChannelCostPricing
		want  string
	}{
		{"complete", ChannelCostPricing{Currency: "CNY", Mode: "per_token", Config: ChannelCostConfig{Items: map[string]float64{"input": 1, "output": 10, "cache_read": 0.5}}}, "0.00065"},
		{"tiny-positive", ChannelCostPricing{Currency: "CNY", Mode: "per_token", Config: ChannelCostConfig{Items: map[string]float64{"input": 1e-18, "output": 1e-18}}}, "0.00000000000000000000025"},
		{"free", ChannelCostPricing{Currency: "CNY", Mode: "per_token", Config: ChannelCostConfig{Items: map[string]float64{"input": 0, "output": 0}}}, "0"},
		{"self-hosted", SupplierSelfHostedPrice(1, "test"), "0"},
		{"missing-output", ChannelCostPricing{Currency: "CNY", Mode: "per_token", Config: ChannelCostConfig{Items: map[string]float64{"input": 0}}}, ""},
		{"unsupported", ChannelCostPricing{Currency: "CNY", Mode: "per_second"}, ""},
		{"currency", ChannelCostPricing{Currency: "EUR", Mode: "per_token", Config: ChannelCostConfig{Items: map[string]float64{"input": 1, "output": 1}}}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			amount, err := SupplierPriceAmount(tc.price, 200, 50, 100)
			if tc.want == "" {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, amount.String())
		})
	}
	cfg := SupplierRoutingConfig{
		Pools:    []SupplierPool{{ID: 1, SupplierID: 1}},
		Bindings: []SupplierBinding{{PoolID: 1, ChannelID: 1, Model: "test"}, {PoolID: 1, ChannelID: 2, Model: "test"}},
		Prices: []ChannelCostPricing{
			{ChannelId: 1, ModelName: "test", Currency: "CNY", Mode: "per_token", Config: ChannelCostConfig{Items: map[string]float64{"input": 1, "output": 1}}},
			{ChannelId: 2, ModelName: "test", Currency: "USD", Mode: "per_token", Config: ChannelCostConfig{Items: map[string]float64{"input": 1, "output": 1}}},
		},
	}
	rule := SupplierRoutingRule{Model: "test", Targets: []SupplierTarget{{SupplierID: 1}}}
	require.ErrorContains(t, ValidateSupplierRulePrices(&cfg, rule), "one currency")
	cfg.Prices[1].Currency = "CNY"
	require.NoError(t, ValidateSupplierRulePrices(&cfg, rule))
	cfg.Prices[0] = SupplierSelfHostedPrice(1, "test")
	cfg.Prices[1].Currency = "USD"
	require.NoError(t, ValidateSupplierRulePrices(&cfg, rule), "self-hosted cost is currency-neutral")
	cfg.Prices = cfg.Prices[:1]
	require.NoError(t, ValidateSupplierRulePrices(&cfg, rule), "self-hosted-only rules need no quote")
}

func TestSupplierAdaptivePerformancePolicyValidation(t *testing.T) {
	for _, tc := range []struct {
		pass, slow int64
		valid      bool
	}{
		{90, 10, true}, {0, 0, true}, {100, 49, true}, {101, 10, false}, {-1, 10, false}, {90, 50, false}, {90, -1, false},
	} {
		rule := SupplierRoutingRule{Mode: "adaptive", Health: DefaultSupplierAdaptiveHealth()}
		rule.Health.PerformancePassPercent, rule.Health.SlowTrafficPercent = tc.pass, tc.slow
		err := ValidateSupplierAdaptiveRule(&rule)
		if tc.valid {
			require.NoError(t, err)
		} else {
			assert.Error(t, err)
		}
	}
}
