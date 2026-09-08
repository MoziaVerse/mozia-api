package service

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type nativeBillingTestAdaptor struct {
	mockAdaptor
	result *relaycommon.TaskInfo
}

func (a *nativeBillingTestAdaptor) ParseTaskResult([]byte) (*relaycommon.TaskInfo, error) {
	return a.result, nil
}

func TestNativeVideoTerminalBillingIsAppliedOnce(t *testing.T) {
	for _, tc := range []struct {
		name      string
		status    string
		wantQuota int
	}{
		{"success adjusts actual usage", model.TaskStatusSuccess, 11000},
		{"confirmed cancellation refunds", model.TaskStatusFailure, 14000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			truncate(t)
			seedUser(t, 42, 10000)
			seedToken(t, 42, 42, "native-token", 10000)
			seedChannel(t, 42)
			task := makeTask(42, 42, 4000, 42, BillingSourceWallet, 0)
			task.TaskID = "cgt-native"
			task.Platform = constant.TaskPlatformVolcengineVideo
			task.Status = model.TaskStatusQueued
			task.SubmitTime = time.Now().Add(-72 * time.Hour).Unix()
			require.NoError(t, model.DB.Create(task).Error)
			stale := *task

			originalTimeout := constant.TaskTimeoutMinutes
			constant.TaskTimeoutMinutes = 1
			t.Cleanup(func() { constant.TaskTimeoutMinutes = originalTimeout })
			sweepTimedOutTasks(context.Background())
			assert.Equal(t, 10000, getUserQuota(t, 42), "local timeout must not refund an Ark task")

			adaptor := &nativeBillingTestAdaptor{
				mockAdaptor: mockAdaptor{adjustReturn: 3000},
				result:      &relaycommon.TaskInfo{TaskID: task.TaskID, Status: tc.status, Reason: "task cancelled"},
			}
			raw := []byte(`{"id":"cgt-native","seed":9007199254740993}`)
			adaptor.result.TaskID = "cgt-foreign"
			require.Error(t, ApplyVideoTaskResponse(context.Background(), adaptor, task, raw))
			assert.Equal(t, 10000, getUserQuota(t, 42))
			adaptor.result.TaskID = task.TaskID
			require.NoError(t, ApplyVideoTaskResponse(context.Background(), adaptor, task, raw))
			require.NoError(t, ApplyVideoTaskResponse(context.Background(), adaptor, &stale, raw), "a stale background poll loses the CAS")
			persisted, exists, err := model.GetByTaskId(42, task.TaskID)
			require.NoError(t, err)
			require.True(t, exists)
			require.NoError(t, ApplyVideoTaskResponse(context.Background(), adaptor, persisted, raw), "a later HTTP poll must not settle again")
			assert.Equal(t, tc.wantQuota, getUserQuota(t, 42))
			assert.Equal(t, tc.wantQuota, getTokenRemainQuota(t, 42))
			assert.Equal(t, int64(1), countLogs(t))
			assert.Equal(t, raw, []byte(persisted.Data))
		})
	}
}
