package relay

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGlobalaiopcChannelRegistration(t *testing.T) {
	assert.Equal(t, "Globalaiopc", constant.ChannelTypeNames[constant.ChannelTypeMoziaGlobalaiopc])
	assert.Equal(t, "https://zcbservice.aizfw.cn/kyyReactApiServer", constant.ChannelBaseURLs[constant.ChannelTypeMoziaGlobalaiopc])

	adaptor := GetMoziaAdaptor(common.APITypeMoziaGlobalaiopc)
	require.NotNil(t, adaptor)
	assert.Equal(t, "globalaiopc", adaptor.GetChannelName())
	_, err := adaptor.GetRequestURL(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "only supports video task endpoints")

	taskAdaptor := GetMoziaTaskAdaptor(constant.ChannelTypeMoziaGlobalaiopc)
	require.NotNil(t, taskAdaptor)
	assert.Equal(t, "globalaiopc", taskAdaptor.GetChannelName())
}

func TestSeedanceCompatibleChannelRegistration(t *testing.T) {
	assert.Equal(t, "SeedanceCompatible-Gen", constant.ChannelTypeNames[constant.ChannelTypeMoziaSeedanceGen])
	assert.Equal(t, "SeedanceCompatible-Videos", constant.ChannelTypeNames[constant.ChannelTypeMoziaSeedanceVideos])
	require.Greater(t, len(constant.ChannelBaseURLs), constant.ChannelTypeMoziaSeedanceVideos)

	seedanceAdaptor := GetMoziaTaskAdaptor(constant.ChannelTypeMoziaSeedanceGen)
	require.NotNil(t, seedanceAdaptor)
	assert.Equal(t, "seedance-compatible-gen", seedanceAdaptor.GetChannelName())

	videosAdaptor := GetMoziaTaskAdaptor(constant.ChannelTypeMoziaSeedanceVideos)
	require.NotNil(t, videosAdaptor)
	assert.Equal(t, "seedance-compatible-videos", videosAdaptor.GetChannelName())
	require.NotNil(t, GetTaskAdaptor(constant.TaskPlatform("204")))
}
