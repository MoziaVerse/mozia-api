package mozia_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserThinkingDisabledRedirectUsers(t *testing.T) {
	original := UserThinkingDisabledRedirects2JSONString()
	t.Cleanup(func() {
		require.NoError(t, UpdateUserThinkingDisabledRedirectsByJSONString(original))
	})
	require.NoError(t, UpdateUserThinkingDisabledRedirectsByJSONString(`{}`))

	_, err := BuildUserThinkingDisabledRedirectUpsertJSON(0)
	assert.ErrorContains(t, err, "user_id")

	value, err := BuildUserThinkingDisabledRedirectUpsertJSON(6218)
	require.NoError(t, err)
	require.NoError(t, UpdateUserThinkingDisabledRedirectsByJSONString(value))
	assert.True(t, IsUserThinkingDisabledRedirectEnabled(6218))
	assert.False(t, IsUserThinkingDisabledRedirectEnabled(6219))
	assert.Equal(t, []int{6218}, GetUserThinkingDisabledRedirectUserIds())

	value, err = BuildUserThinkingDisabledRedirectDeleteJSON(6218)
	require.NoError(t, err)
	require.NoError(t, UpdateUserThinkingDisabledRedirectsByJSONString(value))
	assert.False(t, IsUserThinkingDisabledRedirectEnabled(6218))
	_, err = BuildUserThinkingDisabledRedirectDeleteJSON(6218)
	assert.ErrorContains(t, err, "not found")
}

func TestUserThinkingDisabledRedirectLoadsLegacyRules(t *testing.T) {
	original := UserThinkingDisabledRedirects2JSONString()
	t.Cleanup(func() {
		require.NoError(t, UpdateUserThinkingDisabledRedirectsByJSONString(original))
	})

	require.NoError(t, UpdateUserThinkingDisabledRedirectsByJSONString(`{
		"2:moonshotai/kimi-k3": {
			"user_id": 2,
			"source_model": "moonshotai/kimi-k3",
			"target_model": "moonshotai/kimi-k2.6",
			"enabled": true
		}
	}`))
	assert.True(t, IsUserThinkingDisabledRedirectEnabled(2))
}
