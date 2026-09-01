package mozia_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserThinkingDisabledRedirectExactMatchAndMutation(t *testing.T) {
	original := UserThinkingDisabledRedirects2JSONString()
	t.Cleanup(func() {
		require.NoError(t, UpdateUserThinkingDisabledRedirectsByJSONString(original))
	})
	require.NoError(t, UpdateUserThinkingDisabledRedirectsByJSONString(`{}`))

	rule := UserThinkingDisabledRedirect{
		UserId:      6218,
		SourceModel: " vendor/source ",
		TargetModel: " vendor/target ",
	}
	value, err := BuildUserThinkingDisabledRedirectUpsertJSON(rule)
	require.NoError(t, err)
	require.NoError(t, UpdateUserThinkingDisabledRedirectsByJSONString(value))

	got, ok := GetUserThinkingDisabledRedirect(6218, "vendor/source")
	require.True(t, ok)
	assert.Equal(t, "vendor/target", got.TargetModel)
	_, ok = GetUserThinkingDisabledRedirect(6218, "Vendor/source")
	assert.False(t, ok)
	assert.Equal(t, []UserThinkingDisabledRedirect{{
		UserId: 6218, SourceModel: "vendor/source", TargetModel: "vendor/target",
	}}, GetUserThinkingDisabledRedirects())

	value, err = BuildUserThinkingDisabledRedirectDeleteJSON(6218, "vendor/source")
	require.NoError(t, err)
	require.NoError(t, UpdateUserThinkingDisabledRedirectsByJSONString(value))
	_, ok = GetUserThinkingDisabledRedirect(6218, "vendor/source")
	assert.False(t, ok)
}

func TestUserThinkingDisabledRedirectValidation(t *testing.T) {
	for _, rule := range []UserThinkingDisabledRedirect{
		{SourceModel: "a", TargetModel: "b"},
		{UserId: 1, SourceModel: " ", TargetModel: "b"},
		{UserId: 1, SourceModel: "a", TargetModel: " "},
		{UserId: 1, SourceModel: "same", TargetModel: "same"},
	} {
		_, err := BuildUserThinkingDisabledRedirectUpsertJSON(rule)
		assert.Error(t, err)
	}
}

func TestUserThinkingDisabledRedirectMigratesBooleanFormat(t *testing.T) {
	original := UserThinkingDisabledRedirects2JSONString()
	t.Cleanup(func() {
		require.NoError(t, UpdateUserThinkingDisabledRedirectsByJSONString(original))
	})

	require.NoError(t, UpdateUserThinkingDisabledRedirectsByJSONString(`{"7073":true}`))
	rule, ok := GetUserThinkingDisabledRedirect(7073, "moonshotai/kimi-k3")
	require.True(t, ok)
	assert.Equal(t, "moonshotai/kimi-k2.6", rule.TargetModel)

	require.NoError(t, UpdateUserThinkingDisabledRedirectsByJSONString(`{
		"7073:vendor/source": {
			"user_id": 7073,
			"source_model": "vendor/source",
			"target_model": "vendor/target",
			"enabled": false
		}
	}`))
	_, ok = GetUserThinkingDisabledRedirect(7073, "vendor/source")
	assert.False(t, ok)
}
