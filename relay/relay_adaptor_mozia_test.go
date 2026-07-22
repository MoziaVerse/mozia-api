package relay

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetMoziaTaskAdaptorRegistersModelRouter(t *testing.T) {
	adaptor := GetMoziaTaskAdaptor(constant.ChannelTypeMoziaModelRouter)

	require.NotNil(t, adaptor)
	assert.Equal(t, "modelrouter", adaptor.GetChannelName())
}
