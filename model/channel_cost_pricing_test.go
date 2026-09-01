package model

import (
	"testing"

	"github.com/QuantumNous/new-api/pkg/taskbilling"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelCostPricingUpsertAndValidation(t *testing.T) {
	db := setupResellerPricingTestDB(t)
	require.NoError(t, db.AutoMigrate(&Channel{}, &ChannelCostPricing{}))
	channel := Channel{Name: "cost source", Key: "test", Models: "video-model"}
	require.NoError(t, db.Create(&channel).Error)

	basePrice := 0.18
	cost := ChannelCostPricing{
		ChannelId: channel.Id,
		ModelName: "video-model",
		Currency:  "cny",
		Mode:      ChannelCostModePerSecond,
		Config: ChannelCostConfig{
			BasePrice: &basePrice,
			TaskBilling: &taskbilling.Config{
				Version: taskbilling.Version1,
				Mode:    taskbilling.ModePerSecond,
				Duration: &taskbilling.Dimension{
					Name: "duration", Kind: taskbilling.DimensionNumber,
					Paths: []string{"duration"}, Unit: 1, Round: taskbilling.RoundCeil,
				},
			},
		},
	}
	require.NoError(t, UpsertChannelCostPricing(&cost))
	assert.Equal(t, "CNY", cost.Currency)

	basePrice = 0.16
	cost.Config.BasePrice = &basePrice
	require.NoError(t, UpsertChannelCostPricing(&cost))
	costs, err := ListChannelCostPricing()
	require.NoError(t, err)
	require.Len(t, costs, 1)
	require.NotNil(t, costs[0].Config.BasePrice)
	assert.Equal(t, 0.16, *costs[0].Config.BasePrice)

	cost.Mode = ChannelCostModeParametric
	err = UpsertChannelCostPricing(&cost)
	assert.ErrorContains(t, err, "task billing mode must be parametric")

	cost.Mode = ChannelCostModeTokenParametric
	cost.Config.BasePrice = nil
	cost.Config.TaskBilling = &taskbilling.Config{
		Version: taskbilling.Version1,
		Mode:    taskbilling.ModeTokenParametric,
		TokenPrices: &taskbilling.TokenPriceTable{
			Paths: []string{"resolution"},
			Values: map[string]taskbilling.TokenUnitPrice{
				"720p": {Standard: 0.12},
			},
		},
	}
	require.NoError(t, UpsertChannelCostPricing(&cost))
	costs, err = ListChannelCostPricing()
	require.NoError(t, err)
	require.Len(t, costs, 1)
	assert.Equal(t, ChannelCostModeTokenParametric, costs[0].Mode)
	assert.Equal(t, 0.12, costs[0].Config.TaskBilling.TokenPrices.Values["720p"].Standard)

	deleted, err := DeleteChannelCostPricing(cost.Id)
	require.NoError(t, err)
	assert.True(t, deleted)
}
