package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type pollingKeyCaptureAdaptor struct {
	key      string
	response []byte
}

func (a *pollingKeyCaptureAdaptor) Init(_ *relaycommon.RelayInfo) {}

func (a *pollingKeyCaptureAdaptor) FetchTask(_ string, key string, _ map[string]any, _ string) (*http.Response, error) {
	a.key = key
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(a.response)),
	}, nil
}

func (a *pollingKeyCaptureAdaptor) ParseTaskResult(_ []byte) (*relaycommon.TaskInfo, error) {
	return &relaycommon.TaskInfo{Status: model.TaskStatusInProgress}, nil
}

func (a *pollingKeyCaptureAdaptor) AdjustBillingOnComplete(_ *model.Task, _ *relaycommon.TaskInfo) int {
	return 0
}

func TestUpdateVideoSingleTaskPrefersPersistedSubmissionKey(t *testing.T) {
	task := &model.Task{
		TaskID:    "task_public",
		ChannelId: 200,
		Status:    model.TaskStatusInProgress,
		Progress:  "30%",
		StartTime: 1,
		PrivateData: model.TaskPrivateData{
			Key:            "selected-submission-key",
			UpstreamTaskID: "vendor_task",
		},
	}
	response, err := common.Marshal(dto.TaskResponse[model.Task]{
		Code: dto.TaskSuccessCode,
		Data: model.Task{
			TaskID:   task.TaskID,
			Status:   task.Status,
			Progress: task.Progress,
		},
	})
	require.NoError(t, err)
	task.Data = response

	adaptor := &pollingKeyCaptureAdaptor{response: response}
	channel := &model.Channel{Id: task.ChannelId, Key: "current-channel-key"}

	err = updateVideoSingleTask(
		context.Background(),
		adaptor,
		channel,
		task.GetUpstreamTaskID(),
		map[string]*model.Task{task.GetUpstreamTaskID(): task},
	)

	require.NoError(t, err)
	assert.Equal(t, "selected-submission-key", adaptor.key)
}
