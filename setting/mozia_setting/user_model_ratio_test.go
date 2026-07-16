package mozia_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserModelRatioExactMatchAndStableList(t *testing.T) {
	original := UserModelRatios2JSONString()
	t.Cleanup(func() {
		require.NoError(t, UpdateUserModelRatiosByJSONString(original))
	})
	require.NoError(t, UpdateUserModelRatiosByJSONString(`{}`))

	value, err := BuildUserModelRatioUpsertJSON(UserModelRatio{
		UserId: 396,
		Model:  "video-v1",
		Ratio:  0.36,
	})
	require.NoError(t, err)
	require.NoError(t, UpdateUserModelRatiosByJSONString(value))

	value, err = BuildUserModelRatioUpsertJSON(UserModelRatio{
		UserId: 12,
		Model:  "z-model",
		Ratio:  1.2,
	})
	require.NoError(t, err)
	require.NoError(t, UpdateUserModelRatiosByJSONString(value))

	value, err = BuildUserModelRatioUpsertJSON(UserModelRatio{
		UserId: 12,
		Model:  "a-model",
		Ratio:  0.8,
	})
	require.NoError(t, err)
	require.NoError(t, UpdateUserModelRatiosByJSONString(value))

	ratio, ok := GetUserModelRatio(396, "video-v1")
	require.True(t, ok)
	assert.InDelta(t, 0.36, ratio, 1e-12)

	_, ok = GetUserModelRatio(396, "Video-V1")
	assert.False(t, ok, "model matching must remain case-sensitive and exact")

	assert.Equal(t, []UserModelRatio{
		{UserId: 12, Model: "a-model", Ratio: 0.8},
		{UserId: 12, Model: "z-model", Ratio: 1.2},
		{UserId: 396, Model: "video-v1", Ratio: 0.36},
	}, GetUserModelRatios())
}

func TestUserModelRatioValidationAndDelete(t *testing.T) {
	original := UserModelRatios2JSONString()
	t.Cleanup(func() {
		require.NoError(t, UpdateUserModelRatiosByJSONString(original))
	})
	require.NoError(t, UpdateUserModelRatiosByJSONString(`{}`))

	_, err := BuildUserModelRatioUpsertJSON(UserModelRatio{UserId: 0, Model: "model", Ratio: 1})
	assert.ErrorContains(t, err, "user_id")
	_, err = BuildUserModelRatioUpsertJSON(UserModelRatio{UserId: 1, Model: " ", Ratio: 1})
	assert.ErrorContains(t, err, "model")
	_, err = BuildUserModelRatioUpsertJSON(UserModelRatio{UserId: 1, Model: "model", Ratio: -0.01})
	assert.ErrorContains(t, err, "ratio")
	_, err = BuildUserModelRatioUpsertJSON(UserModelRatio{UserId: 1, Model: "model", Ratio: 0})
	assert.ErrorContains(t, err, "ratio")

	value, err := BuildUserModelRatioUpsertJSON(UserModelRatio{UserId: 396, Model: "video/v1", Ratio: 0.36})
	require.NoError(t, err)
	require.NoError(t, UpdateUserModelRatiosByJSONString(value))

	value, err = BuildUserModelRatioDeleteJSON(396, "video/v1")
	require.NoError(t, err)
	require.NoError(t, UpdateUserModelRatiosByJSONString(value))
	_, ok := GetUserModelRatio(396, "video/v1")
	assert.False(t, ok)

	_, err = BuildUserModelRatioDeleteJSON(396, "video/v1")
	assert.ErrorContains(t, err, "not found")
}
