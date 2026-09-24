package model

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Capture the real Redis commands without opening a network connection. TTL is
// present so the existing wallet cache protocol emits its HINCRBY transaction.
type quotaCacheCommandRecorder struct {
	key      string
	commands chan []interface{}
}

func (h *quotaCacheCommandRecorder) BeforeProcess(ctx context.Context, cmd redis.Cmder) (context.Context, error) {
	return ctx, redis.Nil
}

func (h *quotaCacheCommandRecorder) AfterProcess(_ context.Context, cmd redis.Cmder) error {
	if ttl, ok := cmd.(*redis.DurationCmd); ok && cmd.Name() == "ttl" {
		ttl.SetVal(time.Minute)
		ttl.SetErr(nil)
	} else if args := cmd.Args(); len(args) > 1 && args[1] == h.key {
		h.commands <- args
	}
	return nil
}

func (h *quotaCacheCommandRecorder) BeforeProcessPipeline(ctx context.Context, _ []redis.Cmder) (context.Context, error) {
	return ctx, redis.Nil
}

func (h *quotaCacheCommandRecorder) AfterProcessPipeline(_ context.Context, cmds []redis.Cmder) error {
	for _, cmd := range cmds {
		if args := cmd.Args(); cmd.Name() == "hincrby" && args[1] == h.key {
			h.commands <- args
		}
	}
	return nil
}

func TestAdminQuotaCacheDeltaUsesCommittedUserQuota(t *testing.T) {
	for _, tc := range []struct {
		name                string
		quota, gift, cached int
		mode                string
		value, wantDelta    int
		pendingConsumption  int
	}{
		{name: "delayed consumption stays additive", quota: 50, gift: 50, cached: 100, mode: "add", value: 10, wantDelta: 10, pendingConsumption: 50},
		{name: "repair uses old mirror instead of source total", quota: 0, gift: 100, cached: 0, mode: "override", value: 25, wantDelta: 25},
	} {
		t.Run(tc.name, func(t *testing.T) {
			truncateTables(t)
			seedMoziaWalletUser(t, 1062, tc.quota)
			require.NoError(t, DB.Create(&MoziaWalletBalance{UserId: 1062, Source: MoziaWalletSourceGift, Balance: tc.gift}).Error)
			originalRDB, originalEnabled := common.RDB, common.RedisEnabled
			client := redis.NewClient(&redis.Options{Addr: "unused", MaxRetries: -1})
			hook := &quotaCacheCommandRecorder{key: getUserCacheKey(1062), commands: make(chan []interface{}, 4)}
			client.AddHook(hook)
			common.RDB, common.RedisEnabled = client, true
			t.Cleanup(func() {
				common.RDB, common.RedisEnabled = originalRDB, originalEnabled
				_ = client.Close()
			})

			_, after, err := AdjustMoziaUserQuota(1062, tc.mode, tc.value)
			require.NoError(t, err)
			select {
			case args := <-hook.commands:
				require.Equal(t, []interface{}{"hincrby", hook.key, "Quota", int64(tc.wantDelta)}, args)
			case <-time.After(5 * time.Second):
				t.Fatal("admin quota adjustment did not send the cache increment")
			}
			cached := tc.cached + tc.wantDelta
			if tc.pendingConsumption > 0 {
				// Deliver the old committed consumption's cache update after the admin increment.
				_ = cacheDecrUserQuota(1062, int64(tc.pendingConsumption))
				require.Equal(t, []interface{}{"hincrby", hook.key, "Quota", -int64(tc.pendingConsumption)}, <-hook.commands)
				cached -= tc.pendingConsumption
			}
			assert.Equal(t, after, cached)
			assert.Equal(t, after, getMoziaUserQuota(t, 1062))
		})
	}
}
