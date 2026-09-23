package service

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 查不到 channel（DB 抖动 / 缓存刷新失败）时任务必须留在 pending，
// 不能被标成 FAILURE——那条路径不退款（2026-09-19 事故）。
func TestUpdateVideoTasksKeepsTasksPendingWhenChannelLookupFails(t *testing.T) {
	truncate(t)
	oldCache := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false // 直接查库，channel 不存在即查询失败
	t.Cleanup(func() { common.MemoryCacheEnabled = oldCache })

	const missingChannelID = 999
	task := seedPollingTask(t, missingChannelID, "task_public_x", "upstream_x")

	err := UpdateVideoTasks(context.Background(), constant.TaskPlatform("kling"), map[int][]string{
		missingChannelID: {task.GetUpstreamTaskID()},
	}, map[string]*model.Task{task.GetUpstreamTaskID(): task})
	require.NoError(t, err) // 单个 channel 的错误只记日志，不向上冒泡

	var stored model.Task
	require.NoError(t, model.DB.Where("id = ?", task.ID).First(&stored).Error)
	assert.NotEqual(t, model.TaskStatusFailure, stored.Status)
	assert.Empty(t, stored.FailReason)
}
