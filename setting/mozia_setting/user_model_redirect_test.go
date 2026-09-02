package mozia_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserModelRedirectExactMatchAndMutation(t *testing.T) {
	original := UserModelRedirects2JSONString()
	t.Cleanup(func() {
		require.NoError(t, UpdateUserModelRedirectsByJSONString(original))
	})
	require.NoError(t, UpdateUserModelRedirectsByJSONString(`{}`))

	rule := UserModelRedirect{
		UserId:               6218,
		SourceModel:          " vendor/source ",
		TargetModel:          " vendor/target ",
		OnlyThinkingDisabled: false,
		Seamless:             true,
	}
	value, err := BuildUserModelRedirectUpsertJSON(rule)
	require.NoError(t, err)
	require.NoError(t, UpdateUserModelRedirectsByJSONString(value))

	got, ok := GetUserModelRedirect(6218, "vendor/source")
	require.True(t, ok)
	assert.Equal(t, "vendor/target", got.TargetModel)
	assert.True(t, got.Seamless)
	assert.False(t, got.OnlyThinkingDisabled)
	_, ok = GetUserModelRedirect(6218, "Vendor/source")
	assert.False(t, ok)
	assert.Equal(t, []UserModelRedirect{{
		UserId: 6218, SourceModel: "vendor/source", TargetModel: "vendor/target", Seamless: true,
	}}, GetUserModelRedirects())

	value, err = BuildUserModelRedirectDeleteJSON(6218, "vendor/source")
	require.NoError(t, err)
	require.NoError(t, UpdateUserModelRedirectsByJSONString(value))
	_, ok = GetUserModelRedirect(6218, "vendor/source")
	assert.False(t, ok)
}

func TestUserModelRedirectValidation(t *testing.T) {
	for _, rule := range []UserModelRedirect{
		{SourceModel: "a", TargetModel: "b"},
		{UserId: 1, SourceModel: " ", TargetModel: "b"},
		{UserId: 1, SourceModel: "a", TargetModel: " "},
		{UserId: 1, SourceModel: "same", TargetModel: "same"},
	} {
		_, err := BuildUserModelRedirectUpsertJSON(rule)
		assert.Error(t, err)
	}
}

func TestUserModelRedirectMigratesExistingFormats(t *testing.T) {
	original := UserModelRedirects2JSONString()
	t.Cleanup(func() {
		require.NoError(t, UpdateUserModelRedirectsByJSONString(original))
	})

	require.NoError(t, UpdateUserModelRedirectsByJSONString(`{"7073":true}`))
	rule, ok := GetUserModelRedirect(7073, "moonshotai/kimi-k3")
	require.True(t, ok)
	assert.Equal(t, "moonshotai/kimi-k2.6", rule.TargetModel)
	assert.True(t, rule.OnlyThinkingDisabled)

	require.NoError(t, UpdateUserModelRedirectsByJSONString(`{
		"7073:vendor/source": {
			"user_id": 7073,
			"source_model": "vendor/source",
			"target_model": "vendor/target"
		}
	}`))
	rule, ok = GetUserModelRedirect(7073, "vendor/source")
	require.True(t, ok)
	assert.True(t, rule.OnlyThinkingDisabled)
}
