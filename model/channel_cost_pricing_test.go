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
	require.NoError(t, UpsertChannelCostPricing(db, &cost))
	assert.Equal(t, "CNY", cost.Currency)

	basePrice = 0.16
	cost.Config.BasePrice = &basePrice
	require.NoError(t, UpsertChannelCostPricing(db, &cost))
	costs, err := ListChannelCostPricing()
	require.NoError(t, err)
	require.Len(t, costs, 1)
	require.NotNil(t, costs[0].Config.BasePrice)
	assert.Equal(t, 0.16, *costs[0].Config.BasePrice)

	cost.ModelName = "unrelated-ocr"
	assert.ErrorContains(t, UpsertChannelCostPricing(db, &cost), "model does not belong")
	cost.ModelName = "video-model"
	require.NoError(t, db.Model(&channel).Update("deployment_type", ChannelDeploymentSelfHosted).Error)
	assert.ErrorContains(t, UpsertChannelCostPricing(db, &cost), "self-hosted")
	costs, err = ListChannelCostPricing()
	require.NoError(t, err)
	require.Len(t, costs, 1)
	assert.Equal(t, 0.16, *costs[0].Config.BasePrice)
	require.NoError(t, db.Model(&channel).Update("deployment_type", ChannelDeploymentThirdParty).Error)

	cost.Mode = ChannelCostModeParametric
	err = UpsertChannelCostPricing(db, &cost)
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
	require.NoError(t, UpsertChannelCostPricing(db, &cost))
	costs, err = ListChannelCostPricing()
	require.NoError(t, err)
	require.Len(t, costs, 1)
	assert.Equal(t, ChannelCostModeTokenParametric, costs[0].Mode)
	assert.Equal(t, 0.12, costs[0].Config.TaskBilling.TokenPrices.Values["720p"].Standard)

	deleted, err := DeleteChannelCostPricing(cost.Id)
	require.NoError(t, err)
	assert.True(t, deleted)
}

func TestChannelDeploymentUpdatesPreserveRouting(t *testing.T) {
	db := setupResellerPricingTestDB(t)
	require.NoError(t, db.AutoMigrate(&Channel{}, &Ability{}))
	channel := Channel{Name: "source", Key: "test", Models: "model", Type: 1}
	require.NoError(t, channel.Insert())
	assert.Equal(t, ChannelDeploymentUnknown, channel.DeploymentType)
	for _, deployment := range []string{ChannelDeploymentSelfHosted, ChannelDeploymentThirdParty, ChannelDeploymentUnknown} {
		patch := Channel{Id: channel.Id, DeploymentType: deployment}
		require.NoError(t, patch.Update())
		assert.Equal(t, deployment, patch.DeploymentType)
		assert.Equal(t, channel.Models, patch.Models)
		assert.Equal(t, channel.Key, patch.Key)
		assert.Equal(t, channel.Status, patch.Status)
		omitted := Channel{Id: channel.Id, Name: "renamed"}
		require.NoError(t, omitted.Update())
		assert.Equal(t, deployment, omitted.DeploymentType)
	}
}
