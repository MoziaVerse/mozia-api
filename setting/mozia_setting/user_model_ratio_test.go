package mozia_setting

import (
	"encoding/json"
	"testing"
	"time"

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
		{UserId: 12, Scope: UserRatioScopeModel, Model: "a-model", Ratio: 0.8},
		{UserId: 12, Scope: UserRatioScopeModel, Model: "z-model", Ratio: 1.2},
		{UserId: 396, Scope: UserRatioScopeModel, Model: "video-v1", Ratio: 0.36},
	}, GetUserModelRatios())
}

func TestUserRatioModelTakesPriorityOverChannelAndKeysDoNotCollide(t *testing.T) {
	original := UserModelRatios2JSONString()
	t.Cleanup(func() {
		require.NoError(t, UpdateUserModelRatiosByJSONString(original))
	})
	require.NoError(t, UpdateUserModelRatiosByJSONString(`{}`))

	channelValue, err := BuildUserModelRatioUpsertJSON(UserModelRatio{
		UserId:    396,
		Scope:     UserRatioScopeChannel,
		ChannelId: 35,
		Ratio:     0.5,
	})
	require.NoError(t, err)
	require.NoError(t, UpdateUserModelRatiosByJSONString(channelValue))

	modelValue, err := BuildUserModelRatioUpsertJSON(UserModelRatio{
		UserId: 396,
		Scope:  UserRatioScopeModel,
		Model:  "channel:35",
		Ratio:  0.36,
	})
	require.NoError(t, err)
	require.NoError(t, UpdateUserModelRatiosByJSONString(modelValue))

	ratio, ok := GetUserModelRatio(396, "other-model", 35)
	require.True(t, ok)
	assert.InDelta(t, 0.5, ratio, 1e-12)

	ratio, ok = GetUserModelRatio(396, "channel:35", 35)
	require.True(t, ok)
	assert.InDelta(t, 0.36, ratio, 1e-12)
	assert.Len(t, GetUserModelRatios(), 2)

	value, err := BuildUserRatioDeleteJSON(UserModelRatio{
		UserId:    396,
		Scope:     UserRatioScopeChannel,
		ChannelId: 35,
		Ratio:     1,
	})
	require.NoError(t, err)
	require.NoError(t, UpdateUserModelRatiosByJSONString(value))
	_, ok = GetUserModelRatio(396, "other-model", 35)
	assert.False(t, ok)
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
	_, err = BuildUserModelRatioUpsertJSON(UserModelRatio{UserId: 1, Scope: UserRatioScopeChannel, Ratio: 1})
	assert.ErrorContains(t, err, "channel_id")
	_, err = BuildUserModelRatioUpsertJSON(UserModelRatio{UserId: 1, Scope: "group", Ratio: 1})
	assert.ErrorContains(t, err, "scope")

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

func TestUserModelRatioMultiSourceTakesMinAndIgnoresExpired(t *testing.T) {
	original := UserModelRatios2JSONString()
	t.Cleanup(func() {
		require.NoError(t, UpdateUserModelRatiosByJSONString(original))
	})
	require.NoError(t, UpdateUserModelRatiosByJSONString(`{}`))

	now := time.Now().Unix()
	apply := func(rule UserModelRatio) {
		value, err := BuildUserModelRatioUpsertJSON(rule)
		require.NoError(t, err)
		require.NoError(t, UpdateUserModelRatiosByJSONString(value))
	}

	// 旧格式（无来源）手工规则 + 月卡规则并存，取最低
	apply(UserModelRatio{UserId: 7, Model: "kimi", Ratio: 0.9})
	apply(UserModelRatio{UserId: 7, Model: "kimi", Ratio: 0.8, Source: "model_card", ExpiresAt: now + 3600})
	ratio, ok := GetUserModelRatio(7, "kimi")
	require.True(t, ok)
	assert.InDelta(t, 0.8, ratio, 1e-12)
	assert.Len(t, GetUserModelRatios(), 2, "两条规则并存，不互相覆盖")

	// 场景包更低，仍取最低
	apply(UserModelRatio{UserId: 7, Model: "kimi", Ratio: 0.005, Source: "scenario_pack"})
	ratio, _ = GetUserModelRatio(7, "kimi")
	assert.InDelta(t, 0.005, ratio, 1e-12)

	// 删掉场景包规则（必须带 source），回到 0.8
	value, err := BuildUserRatioDeleteJSON(UserModelRatio{UserId: 7, Scope: UserRatioScopeModel, Model: "kimi", Source: "scenario_pack", Ratio: 1})
	require.NoError(t, err)
	require.NoError(t, UpdateUserModelRatiosByJSONString(value))
	ratio, _ = GetUserModelRatio(7, "kimi")
	assert.InDelta(t, 0.8, ratio, 1e-12)

	// 不带 source 删的是旧格式那条，月卡规则不受影响
	value, err = BuildUserRatioDeleteJSON(UserModelRatio{UserId: 7, Scope: UserRatioScopeModel, Model: "kimi", Ratio: 1})
	require.NoError(t, err)
	require.NoError(t, UpdateUserModelRatiosByJSONString(value))
	ratio, ok = GetUserModelRatio(7, "kimi")
	require.True(t, ok)
	assert.InDelta(t, 0.8, ratio, 1e-12)

	// 已过期的规则不参与计费、不出现在列表里
	apply(UserModelRatio{UserId: 7, Model: "kimi", Ratio: 0.1, Source: "expired_card", ExpiresAt: now - 1})
	ratio, ok = GetUserModelRatio(7, "kimi")
	require.True(t, ok)
	assert.InDelta(t, 0.8, ratio, 1e-12)
	for _, r := range GetUserModelRatios() {
		assert.NotEqual(t, "expired_card", r.Source)
	}

	// 唯一一条规则过期 → 视为无规则
	value, err = BuildUserRatioDeleteJSON(UserModelRatio{UserId: 7, Scope: UserRatioScopeModel, Model: "kimi", Source: "model_card", Ratio: 1})
	require.NoError(t, err)
	require.NoError(t, UpdateUserModelRatiosByJSONString(value))
	_, ok = GetUserModelRatio(7, "kimi")
	assert.False(t, ok)
}

func TestUserModelRatioExpiredRulesAreGarbageCollectedOnWrite(t *testing.T) {
	original := UserModelRatios2JSONString()
	t.Cleanup(func() {
		require.NoError(t, UpdateUserModelRatiosByJSONString(original))
	})
	require.NoError(t, UpdateUserModelRatiosByJSONString(`{}`))

	longAgo := time.Now().Add(-expiredRuleRetention - time.Hour).Unix()
	recently := time.Now().Add(-time.Hour).Unix()
	seed := map[string]UserModelRatio{
		"1:a#old":    {UserId: 1, Scope: UserRatioScopeModel, Model: "a", Ratio: 0.5, Source: "old", ExpiresAt: longAgo},
		"1:a#recent": {UserId: 1, Scope: UserRatioScopeModel, Model: "a", Ratio: 0.5, Source: "recent", ExpiresAt: recently},
		"1:a":        {UserId: 1, Scope: UserRatioScopeModel, Model: "a", Ratio: 0.7},
	}
	raw, err := json.Marshal(seed)
	require.NoError(t, err)
	require.NoError(t, UpdateUserModelRatiosByJSONString(string(raw)))

	// 任意一次写入触发 GC：超过保留期的删掉，刚过期的留着（便于排障）
	value, err := BuildUserModelRatioUpsertJSON(UserModelRatio{UserId: 2, Model: "b", Ratio: 0.9})
	require.NoError(t, err)
	var persisted map[string]UserModelRatio
	require.NoError(t, json.Unmarshal([]byte(value), &persisted))
	_, hasOld := persisted["1:a#old"]
	_, hasRecent := persisted["1:a#recent"]
	assert.False(t, hasOld)
	assert.True(t, hasRecent)
	assert.Contains(t, persisted, "1:a")
	assert.Contains(t, persisted, "2:b")
}

func TestUserModelRatioSourceValidation(t *testing.T) {
	_, err := BuildUserModelRatioUpsertJSON(UserModelRatio{UserId: 1, Model: "m", Ratio: 0.5, Source: "bad#src"})
	assert.ErrorContains(t, err, "source")
	_, err = BuildUserModelRatioUpsertJSON(UserModelRatio{UserId: 1, Model: "m", Ratio: 0.5, Source: "a b"})
	assert.ErrorContains(t, err, "source")
}
