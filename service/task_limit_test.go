package service

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const tlModel = "minimax/minimax-h3-ref2va"

func tlSetup(t *testing.T, userId int) (now time.Time, insert func(status string, ageSec int64, quota int, opts ...func(*model.Task))) {
	t.Helper()
	require.NoError(t, model.DB.AutoMigrate(&model.MoziaUserTaskLimitOverride{}))
	require.NoError(t, model.DB.Where("user_id = ?", userId).Delete(&model.Task{}).Error)
	require.NoError(t, model.DeleteTaskLimitOverride(userId))
	now = time.Unix(1_800_000_000, 0)
	taskLimitNow = func() time.Time { return now }
	t.Cleanup(func() {
		taskLimitNow = time.Now
		setting.TaskLimitEnforce = false
		_ = setting.UpdateGroupTaskLimitsByJSONString("{}")
		_ = setting.UpdateTaskLimitScopeByJSONString("[]")
	})
	require.NoError(t, setting.UpdateTaskLimitScopeByJSONString(`["minimax/minimax-h3"]`))
	require.NoError(t, setting.UpdateGroupTaskLimitsByJSONString(`{"default":{"running":1,"queued":2,"per_minute":3,"daily_yuan":0,"fail_streak":3},"sub_basic":{"running":2,"queued":5}}`))
	setting.TaskLimitEnforce = true
	seq := 0
	insert = func(status string, ageSec int64, quota int, opts ...func(*model.Task)) {
		seq++
		tk := &model.Task{
			TaskID: model.GenerateTaskID(), Platform: constant.TaskPlatform("207"), ChannelId: 1, UserId: userId,
			Status: model.TaskStatus(status), SubmitTime: now.Unix() - ageSec, Quota: quota,
			Properties: model.Properties{OriginModelName: tlModel},
		}
		if status == model.TaskStatusSuccess || status == model.TaskStatusFailure {
			tk.Progress = "100%"
			tk.FinishTime = now.Unix() - ageSec + 60
		}
		for _, o := range opts {
			o(tk)
		}
		require.NoError(t, tk.Insert())
	}
	return now, insert
}

func TestTaskLimit_ScopeAndGroupGate(t *testing.T) {
	_, insert := tlSetup(t, 7001)
	insert(model.TaskStatusQueued, 10, 0)
	insert(model.TaskStatusQueued, 10, 0)
	insert(model.TaskStatusInProgress, 10, 0)
	assert.Nil(t, CheckTaskSubmitLimit(context.Background(), 7001, "default", "auto", "doubao/seedance-2.0"), "范围外模型不受限")
	assert.Nil(t, CheckTaskSubmitLimit(context.Background(), 7001, "internal", "auto", tlModel), "未配置的分组不受限")
	assert.NotNil(t, CheckTaskSubmitLimit(context.Background(), 7001, "default", "auto", tlModel))
}

func TestTaskLimit_RunningAndQueued(t *testing.T) {
	_, insert := tlSetup(t, 7002)
	// 0 运行 1 排队：放行
	insert(model.TaskStatusQueued, 10, 0)
	assert.Nil(t, CheckTaskSubmitLimit(context.Background(), 7002, "default", "auto", tlModel))
	// 1 运行 1 排队：运行满但排队有位，放行
	insert(model.TaskStatusInProgress, 10, 0)
	assert.Nil(t, CheckTaskSubmitLimit(context.Background(), 7002, "default", "auto", tlModel))
	// 1 运行 2 排队：拒绝
	insert(model.TaskStatusQueued, 10, 0)
	err := CheckTaskSubmitLimit(context.Background(), 7002, "default", "auto", tlModel)
	require.NotNil(t, err)
	assert.Equal(t, 429, err.StatusCode)
	assert.Equal(t, TaskLimitErrorCode, err.Code)
	d := err.Data.(*TaskLimitDecision)
	assert.Equal(t, "queued", d.Kind)
	assert.Equal(t, 1, d.Running)
	assert.Equal(t, 2, d.Queued)
	assert.Contains(t, err.Message, "已达套餐上限")
	// 付费档上限更高：同样的用量放行
	assert.Nil(t, CheckTaskSubmitLimit(context.Background(), 7002, "sub_basic", "auto", tlModel))
}

func TestTaskLimit_ObserveModeOnlyLogs(t *testing.T) {
	_, insert := tlSetup(t, 7003)
	insert(model.TaskStatusInProgress, 10, 0)
	insert(model.TaskStatusQueued, 10, 0)
	insert(model.TaskStatusQueued, 10, 0)
	setting.TaskLimitEnforce = false
	assert.Nil(t, CheckTaskSubmitLimit(context.Background(), 7003, "default", "auto", tlModel), "观察模式不拦截")
	usage := CollectTaskLimitUsage(7003, "default", setting.TaskLimitScopeMatches, tlModel)
	assert.NotNil(t, evaluateTaskLimit(usage, "default", taskLimitNow()), "但决策本身成立")
}

func TestTaskLimit_LeaseExcludesZombies(t *testing.T) {
	_, insert := tlSetup(t, 7004)
	insert(model.TaskStatusQueued, 4*3600, 0) // 4 小时前的僵尸
	insert(model.TaskStatusQueued, 4*3600, 0)
	insert(model.TaskStatusQueued, 4*3600, 0)
	assert.Nil(t, CheckTaskSubmitLimit(context.Background(), 7004, "default", "auto", tlModel))
}

func TestTaskLimit_PerMinute(t *testing.T) {
	_, insert := tlSetup(t, 7005)
	insert(model.TaskStatusSuccess, 50, 0)
	insert(model.TaskStatusFailure, 40, 0)
	insert(model.TaskStatusSuccess, 30, 0)
	err := CheckTaskSubmitLimit(context.Background(), 7005, "default", "auto", tlModel)
	require.NotNil(t, err)
	assert.Equal(t, "per_minute", err.Data.(*TaskLimitDecision).Kind)
}

func TestTaskLimit_FailStreakCooldown(t *testing.T) {
	now, insert := tlSetup(t, 7006)
	// 三连败，最后一次 2 分钟前结束 → 冷却中
	insert(model.TaskStatusFailure, 900, 0, func(tk *model.Task) { tk.FinishTime = now.Unix() - 800 })
	insert(model.TaskStatusFailure, 600, 0, func(tk *model.Task) { tk.FinishTime = now.Unix() - 500 })
	insert(model.TaskStatusFailure, 200, 0, func(tk *model.Task) { tk.FinishTime = now.Unix() - 120 })
	err := CheckTaskSubmitLimit(context.Background(), 7006, "default", "auto", tlModel)
	require.NotNil(t, err)
	d := err.Data.(*TaskLimitDecision)
	assert.Equal(t, "fail_streak", d.Kind)
	assert.InDelta(t, 480, d.RetryAfter, 2)

	// 中间夹一次成功就不算连续
	insert(model.TaskStatusSuccess, 100, 0)
	assert.Nil(t, CheckTaskSubmitLimit(context.Background(), 7006, "default", "auto", tlModel))
}

func TestTaskLimit_Override(t *testing.T) {
	now, insert := tlSetup(t, 7007)
	insert(model.TaskStatusQueued, 10, 0)

	// 暂停：永久
	require.NoError(t, model.UpsertTaskLimitOverride(&model.MoziaUserTaskLimitOverride{UserId: 7007, Reason: "批量注册团伙"}))
	err := CheckTaskSubmitLimit(context.Background(), 7007, "sub_basic", "auto", tlModel)
	require.NotNil(t, err)
	assert.Equal(t, "suspended", err.Data.(*TaskLimitDecision).Kind)
	assert.Contains(t, err.Message, "已被暂停")
	assert.Contains(t, err.Message, "批量注册团伙")

	// 冷却：30 分钟
	require.NoError(t, model.UpsertTaskLimitOverride(&model.MoziaUserTaskLimitOverride{UserId: 7007, Until: now.Add(30 * time.Minute).Unix()}))
	err = CheckTaskSubmitLimit(context.Background(), 7007, "sub_basic", "auto", tlModel)
	require.NotNil(t, err)
	assert.Contains(t, err.Message, "冷却期")
	assert.Equal(t, 1800, err.Data.(*TaskLimitDecision).RetryAfter)

	// 降速：只压排队到 1，其余沿用分组
	require.NoError(t, model.UpsertTaskLimitOverride(&model.MoziaUserTaskLimitOverride{UserId: 7007, Queued: 1, Until: now.Add(time.Hour).Unix()}))
	err = CheckTaskSubmitLimit(context.Background(), 7007, "sub_basic", "auto", tlModel)
	require.NotNil(t, err)
	assert.Equal(t, "queued", err.Data.(*TaskLimitDecision).Kind)
	assert.Equal(t, 1, err.Data.(*TaskLimitDecision).Limit)

	// 过期的覆盖不生效
	require.NoError(t, model.UpsertTaskLimitOverride(&model.MoziaUserTaskLimitOverride{UserId: 7007, Until: now.Add(-time.Minute).Unix()}))
	assert.Nil(t, CheckTaskSubmitLimit(context.Background(), 7007, "sub_basic", "auto", tlModel))

	// 人工解除
	require.NoError(t, model.DeleteTaskLimitOverride(7007))
	assert.Nil(t, CheckTaskSubmitLimit(context.Background(), 7007, "sub_basic", "auto", tlModel))
}

func TestTaskLimit_QueueView(t *testing.T) {
	now, insert := tlSetup(t, 7008)
	insert(model.TaskStatusQueued, 30, 0, func(tk *model.Task) { tk.ChannelId = 951; tk.PrivateData.UpstreamTaskID = "mine" })
	insert(model.TaskStatusInProgress, 300, 0, func(tk *model.Task) {
		tk.ChannelId = 951
		tk.PrivateData.UpstreamTaskID = "run"
		tk.StartTime = now.Unix() - 100
	})
	base := float64(now.Unix())
	storeWorkerSnapshot(&WorkerQueueSnapshot{
		ChannelId: 951, SampledAt: now.Unix(), Running: 1, Queued: 3, HasTimeline: true, P50RunSeconds: 400,
		Active: map[string]WorkerAuditEntry{
			"run":   {ID: "run", State: "running", DequeuedAt: base - 100},
			"early": {ID: "early", State: "queued", QueuedAt: base - 60},
			"mine":  {ID: "mine", State: "queued", QueuedAt: base - 30},
			"late":  {ID: "late", State: "queued", QueuedAt: base - 5},
		},
	})
	t.Cleanup(func() {
		h3SnapshotMu.Lock()
		delete(h3SnapshotStore, 951)
		h3SnapshotMu.Unlock()
	})

	view := BuildTaskQueueView(7008, "default", "auto", "")
	assert.Equal(t, "default", view.Group)
	assert.True(t, view.Enforce)
	require.NotNil(t, view.Limits)
	assert.Equal(t, 1, view.Limits.Running)
	assert.Equal(t, 1, view.Running)
	assert.Equal(t, 1, view.Queued)
	assert.Equal(t, 1, view.Pool.Workers)
	assert.Equal(t, 3, view.Pool.Queued)
	require.Len(t, view.Tasks, 2)
	running, queued := view.Tasks[0], view.Tasks[1]
	assert.Equal(t, string(model.TaskStatusInProgress), running.Status)
	require.NotNil(t, running.Ahead)
	assert.Equal(t, 0, *running.Ahead)
	require.NotNil(t, queued.Ahead)
	assert.Equal(t, 1, *queued.Ahead, "只有 early 排在我前面")
	require.NotNil(t, queued.EtaSeconds)
	assert.Equal(t, 600, *queued.EtaSeconds, "1×p50 + 当前 running 剩余半个 p50")
	assert.Nil(t, view.Blocked, "1 运行 1 排队未达上限")
}

func TestTaskLimit_PerModelOverrideKey(t *testing.T) {
	_, insert := tlSetup(t, 7009)
	require.NoError(t, setting.UpdateTaskLimitScopeByJSONString(`["minimax/minimax-h3","wan-ai/"]`))
	require.NoError(t, setting.UpdateGroupTaskLimitsByJSONString(`{"default":{"running":1,"queued":2},"default@wan-ai/":{"running":1,"queued":1}}`))
	// H3 已有 1 运行 2 排队（共用名额已满）
	insert(model.TaskStatusInProgress, 10, 0)
	insert(model.TaskStatusQueued, 10, 0)
	insert(model.TaskStatusQueued, 10, 0)
	// wan 有独立名额：H3 的任务不计入
	assert.Nil(t, CheckTaskSubmitLimit(context.Background(), 7009, "default", "auto", "wan-ai/wan3.0-video"))
	insert(model.TaskStatusQueued, 5, 0, func(tk *model.Task) { tk.Properties.OriginModelName = "wan-ai/wan3.0-video" })
	err := CheckTaskSubmitLimit(context.Background(), 7009, "default", "auto", "wan-ai/wan3.0-video")
	require.NotNil(t, err, "wan 独立名额 queued=1 已满")
	assert.Equal(t, 1, err.Data.(*TaskLimitDecision).Limit)
	// H3 走分组键，且 wan 的任务不占 H3 名额（仍是 1/2 → 拒绝是因为 H3 自己满了）
	err = CheckTaskSubmitLimit(context.Background(), 7009, "default", "auto", tlModel)
	require.NotNil(t, err)
	assert.Equal(t, 2, err.Data.(*TaskLimitDecision).Queued)
	view := BuildTaskQueueView(7009, "default", "auto", "wan-ai/")
	assert.Equal(t, "wan-ai/", view.ScopePrefix)
}

// 范围为空 = 限制不生效（安全默认），队列展示则显示全部
func TestTaskLimit_EmptyScopeMeansNoLimit(t *testing.T) {
	require.NoError(t, setting.UpdateTaskLimitScopeByJSONString("[]"))
	defer func() { _ = setting.UpdateTaskLimitScopeByJSONString(`["minimax/minimax-h3"]`) }()
	assert.False(t, setting.TaskLimitScopeMatches("minimax/minimax-h3-t2va"))
	assert.True(t, setting.TaskLimitScopeMatchesOrAll("minimax/minimax-h3-t2va"))
	assert.Nil(t, CheckTaskSubmitLimit(context.Background(), 1, "default", "auto", "minimax/minimax-h3-t2va"))
}
