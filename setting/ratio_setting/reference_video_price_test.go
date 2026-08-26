package ratio_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReferenceVideoPriceConfiguration(t *testing.T) {
	original := ReferenceVideoPrice2JSONString()
	t.Cleanup(func() {
		require.NoError(t, UpdateReferenceVideoPriceByJSONString(original))
	})

	require.NoError(t, UpdateReferenceVideoPriceByJSONString(`{"video-model":0.08}`))
	price, ok := GetReferenceVideoPrice("video-model")
	require.True(t, ok)
	assert.Equal(t, 0.08, price)
	assert.Error(t, UpdateReferenceVideoPriceByJSONString(`{"video-model":-1}`))
}
