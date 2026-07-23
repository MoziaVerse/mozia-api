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
