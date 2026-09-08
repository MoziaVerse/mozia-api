package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVolcengineVideoChannelSelectionFiltersBeforePriority(t *testing.T) {
	db := setupResellerPricingTestDB(t)
	require.NoError(t, db.AutoMigrate(&Channel{}, &Ability{}))
	high, middle, low := int64(20), int64(10), int64(1)
	channels := []*Channel{
		{Id: 1, Type: constant.ChannelTypeOpenAI, Key: "test", Models: "video", Group: "default", Status: common.ChannelStatusEnabled, Priority: &high},
		{Id: 2, Type: constant.ChannelTypeMoziaArtsapi, Key: "test", Models: "video", Group: "default", Status: common.ChannelStatusEnabled, Priority: &middle},
		{Id: 3, Type: constant.ChannelTypeDoubaoVideo, Key: "test", Models: "video", Group: "default", Status: common.ChannelStatusEnabled, Priority: &low},
	}
	for _, ch := range channels {
		require.NoError(t, db.Create(ch).Error)
		require.NoError(t, ch.AddAbilities(db))
	}
	for retry, id := range []int{2, 3, 3} {
		ch, err := GetChannel("default", "video", retry, constant.VolcengineVideoTaskPath)
		require.NoError(t, err)
		require.NotNil(t, ch)
		assert.Equal(t, id, ch.Id)
	}
	ch, err := GetChannel("default", "video", 0, "/v1/chat/completions")
	require.NoError(t, err)
	require.NotNil(t, ch)
	assert.Equal(t, 1, ch.Id)

	channelSyncLock.Lock()
	oldChannels := channelsIDM
	channelsIDM = map[int]*Channel{1: channels[0], 2: channels[1], 3: channels[2]}
	ids := filterChannelsByRequestPath([]int{1, 2, 3}, constant.VolcengineVideoTaskPath)
	channelsIDM = oldChannels
	channelSyncLock.Unlock()
	assert.Equal(t, []int{2, 3}, ids)
}
