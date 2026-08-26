package ratio_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVideoInputRatioConfigurationUsesPublicModelName(t *testing.T) {
	original := VideoInputRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, UpdateVideoInputRatioByJSONString(original))
	})

	require.NoError(t, UpdateVideoInputRatioByJSONString(`{
		"doubao/seedance-2.0-pro-480p": 0.6086956522,
		"doubao/seedance-2.0-pro-1080p": 0.6078431373
	}`))

	ratio, ok := GetVideoInputRatio("doubao/seedance-2.0-pro-480p")
	require.True(t, ok)
	assert.InDelta(t, 0.6086956522, ratio, 1e-12)

	_, ok = GetVideoInputRatio("artsdance-2-0-pro-260801")
	assert.False(t, ok)

	assert.Error(t, UpdateVideoInputRatioByJSONString(`{"video-model":0}`))
	ratio, ok = GetVideoInputRatio("doubao/seedance-2.0-pro-480p")
	require.True(t, ok)
	assert.InDelta(t, 0.6086956522, ratio, 1e-12)
}
