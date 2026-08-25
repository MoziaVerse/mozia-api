package mozia_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserThinkingDisabledRedirectExactMatchAndEnabled(t *testing.T) {
	original := UserThinkingDisabledRedirects2JSONString()
	t.Cleanup(func() {
		require.NoError(t, UpdateUserThinkingDisabledRedirectsByJSONString(original))
	})
	require.NoError(t, UpdateUserThinkingDisabledRedirectsByJSONString(`{}`))

	value, err := BuildUserThinkingDisabledRedirectUpsertJSON(UserThinkingDisabledRedirect{
		UserId:      6218,
		SourceModel: " moonshotai/kimi-k3 ",
		TargetModel: " moonshotai/kimi-k2.6 ",
		Enabled:     true,
	})
	require.NoError(t, err)
	require.NoError(t, UpdateUserThinkingDisabledRedirectsByJSONString(value))

	rule, ok := GetUserThinkingDisabledRedirect(6218, "moonshotai/kimi-k3")
	require.True(t, ok)
	assert.Equal(t, "moonshotai/kimi-k2.6", rule.TargetModel)
	_, ok = GetUserThinkingDisabledRedirect(6218, "MoonshotAI/kimi-k3")
	assert.False(t, ok)
	_, ok = GetUserThinkingDisabledRedirect(6219, "moonshotai/kimi-k3")
	assert.False(t, ok)

	value, err = BuildUserThinkingDisabledRedirectUpsertJSON(UserThinkingDisabledRedirect{
		UserId:      6218,
		SourceModel: "moonshotai/kimi-k3",
		TargetModel: "moonshotai/kimi-k2.6",
		Enabled:     false,
	})
	require.NoError(t, err)
	require.NoError(t, UpdateUserThinkingDisabledRedirectsByJSONString(value))
	_, ok = GetUserThinkingDisabledRedirect(6218, "moonshotai/kimi-k3")
	assert.False(t, ok)
}

func TestUserThinkingDisabledRedirectValidationAndDelete(t *testing.T) {
	original := UserThinkingDisabledRedirects2JSONString()
	t.Cleanup(func() {
		require.NoError(t, UpdateUserThinkingDisabledRedirectsByJSONString(original))
	})
	require.NoError(t, UpdateUserThinkingDisabledRedirectsByJSONString(`{}`))

	invalid := []UserThinkingDisabledRedirect{
		{SourceModel: "a", TargetModel: "b", Enabled: true},
		{UserId: 1, SourceModel: " ", TargetModel: "b", Enabled: true},
		{UserId: 1, SourceModel: "a", TargetModel: " ", Enabled: true},
		{UserId: 1, SourceModel: "same", TargetModel: "same", Enabled: true},
	}
	for _, rule := range invalid {
		_, err := BuildUserThinkingDisabledRedirectUpsertJSON(rule)
		assert.Error(t, err)
	}

	value, err := BuildUserThinkingDisabledRedirectUpsertJSON(UserThinkingDisabledRedirect{
		UserId: 1, SourceModel: "a", TargetModel: "b", Enabled: true,
	})
	require.NoError(t, err)
	require.NoError(t, UpdateUserThinkingDisabledRedirectsByJSONString(value))
	value, err = BuildUserThinkingDisabledRedirectDeleteJSON(1, "a")
	require.NoError(t, err)
	require.NoError(t, UpdateUserThinkingDisabledRedirectsByJSONString(value))
	_, ok := GetUserThinkingDisabledRedirect(1, "a")
	assert.False(t, ok)
}
