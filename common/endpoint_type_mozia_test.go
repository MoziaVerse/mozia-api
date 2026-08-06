package common

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
)

func TestSeedanceCompatibleChannelsAdvertiseVideoEndpoint(t *testing.T) {
	for _, channelType := range []int{constant.ChannelTypeMoziaSeedanceGen, constant.ChannelTypeMoziaSeedanceVideos} {
		assert.Equal(t, []constant.EndpointType{constant.EndpointTypeOpenAIVideo}, GetEndpointTypesByChannelType(channelType, "video-model"))
	}
}
