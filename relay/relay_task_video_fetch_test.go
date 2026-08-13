package relay

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPublicVideoTaskResponseBodyFlatContract(t *testing.T) {
	originalServerAddress := system_setting.ServerAddress
	system_setting.ServerAddress = "https://gateway.example"
	t.Cleanup(func() {
		system_setting.ServerAddress = originalServerAddress
	})

	successTask := &model.Task{
		TaskID:    "task_success",
		Status:    model.TaskStatusSuccess,
		Progress:  "30%",
		CreatedAt: 100,
		UpdatedAt: 200,
		Properties: model.Properties{
			OriginModelName:   "public-model",
			UpstreamModelName: "upstream-model",
		},
		PrivateData: model.TaskPrivateData{
			ResultURL: "https://cdn.example/private.mp4",
		},
		Data: mustMarshalTaskData(t, map[string]any{
			"ratio": "16:9",
			"result": map[string]any{
				"duration":   5.5,
				"resolution": "720p",
			},
		}),
	}

	body, err := publicVideoTaskResponseBody(successTask, successTask.Data)
	require.NoError(t, err)

	var resp dto.PublicVideoTaskResponse
	require.NoError(t, common.Unmarshal(body, &resp))
	assert.Equal(t, "task_success", resp.ID)
	assert.Equal(t, "task_success", resp.TaskID)
	assert.Equal(t, "video", resp.Object)
	assert.Equal(t, "public-model", resp.Model)
	assert.Equal(t, "succeeded", resp.Status)
	assert.Equal(t, 100, resp.Progress)
	assert.Equal(t, int64(100), resp.CreatedAt)
	assert.Equal(t, int64(200), resp.UpdatedAt)
	require.NotNil(t, resp.Content)
	assert.Equal(t, "https://gateway.example/v1/videos/task_success/content", resp.Content.URL)
	assert.Nil(t, resp.Error)
	assert.Equal(t, "720p", resp.Resolution)
	assert.Equal(t, "16:9", resp.Ratio)
	require.NotNil(t, resp.Duration)
	assert.Equal(t, 5.5, *resp.Duration)

	var raw map[string]any
	require.NoError(t, common.Unmarshal(body, &raw))
	assert.NotContains(t, raw, "code")
	assert.NotContains(t, raw, "data")
	assert.NotContains(t, raw, "user_id")
	assert.NotContains(t, raw, "channel_id")
	assert.NotContains(t, raw, "quota")
	assert.NotContains(t, raw, "fail_reason")
	assert.NotContains(t, raw, "result_url")
	assert.NotContains(t, raw, "properties")
	assert.NotContains(t, raw, "usage")
}

func TestPublicVideoTaskResponseStatusesAndConditionals(t *testing.T) {
	tests := []struct {
		name           string
		status         model.TaskStatus
		progress       string
		failReason     string
		data           map[string]any
		wantStatus     string
		wantProgress   int
		wantContent    bool
		wantError      bool
		wantRatio      string
		wantResolution string
		wantDuration   float64
	}{
		{
			name:         "not started is queued",
			status:       model.TaskStatusNotStart,
			progress:     "0%",
			wantStatus:   "queued",
			wantProgress: 0,
		},
		{
			name:         "queued from submitted",
			status:       model.TaskStatusSubmitted,
			progress:     "",
			wantStatus:   "queued",
			wantProgress: 0,
		},
		{
			name:         "running parses numeric progress",
			status:       model.TaskStatusInProgress,
			progress:     "30%",
			wantStatus:   "running",
			wantProgress: 30,
			data: map[string]any{
				"aspect_ratio": "9:16",
				"seconds":      "8",
			},
			wantRatio:    "9:16",
			wantDuration: 8,
		},
		{
			name:         "failure exposes only error",
			status:       model.TaskStatusFailure,
			progress:     "25%",
			failReason:   "provider failed",
			wantStatus:   "failed",
			wantProgress: 100,
			wantError:    true,
		},
		{
			name:         "cancelled is normalized",
			status:       model.TaskStatus("CANCELED"),
			progress:     "1%",
			wantStatus:   "cancelled",
			wantProgress: 100,
		},
		{
			name:         "expired is normalized",
			status:       model.TaskStatus("EXPIRED"),
			progress:     "99%",
			wantStatus:   "expired",
			wantProgress: 100,
		},
		{
			name:         "unknown status falls back",
			status:       model.TaskStatus("WHATEVER"),
			progress:     "bad",
			wantStatus:   "unknown",
			wantProgress: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			task := &model.Task{
				TaskID:     "task_case",
				Status:     tc.status,
				Progress:   tc.progress,
				FailReason: tc.failReason,
				Data:       mustMarshalTaskData(t, tc.data),
			}
			if tc.wantContent {
				task.PrivateData.ResultURL = "https://cdn.example/result.mp4"
			}

			resp := publicVideoTaskResponse(task, task.Data)
			assert.Equal(t, tc.wantStatus, resp.Status)
			assert.Equal(t, tc.wantProgress, resp.Progress)
			if tc.wantContent {
				require.NotNil(t, resp.Content)
			} else {
				assert.Nil(t, resp.Content)
			}
			if tc.wantError {
				require.NotNil(t, resp.Error)
				assert.Equal(t, "task_failed", resp.Error.Code)
				assert.Equal(t, tc.failReason, resp.Error.Message)
			} else {
				assert.Nil(t, resp.Error)
			}
			assert.Equal(t, tc.wantRatio, resp.Ratio)
			assert.Equal(t, tc.wantResolution, resp.Resolution)
			if tc.wantDuration > 0 {
				require.NotNil(t, resp.Duration)
				assert.Equal(t, tc.wantDuration, *resp.Duration)
			} else {
				assert.Nil(t, resp.Duration)
			}
		})
	}
}

func TestApplyRealtimeTaskInfoKeepsFailureDetails(t *testing.T) {
	task := &model.Task{
		Status:     model.TaskStatusInProgress,
		Progress:   "30%",
		FailReason: "stale error",
	}
	applyRealtimeTaskInfo(task, &relaycommon.TaskInfo{
		Status:   model.TaskStatusFailure,
		Progress: "100%",
		Reason:   "fresh upstream error",
	})

	assert.Equal(t, string(model.TaskStatusFailure), string(task.Status))
	assert.Equal(t, "100%", task.Progress)
	assert.Equal(t, "fresh upstream error", task.FailReason)

	resp := publicVideoTaskResponse(task, nil)
	require.NotNil(t, resp.Error)
	assert.Equal(t, "fresh upstream error", resp.Error.Message)

	applyRealtimeTaskInfo(task, &relaycommon.TaskInfo{Status: model.TaskStatusFailure})
	assert.Empty(t, task.FailReason)
}

func TestPublicVideoTaskResponseUsesFreshRealtimeMetadata(t *testing.T) {
	task := &model.Task{
		TaskID: "task_realtime",
		Status: model.TaskStatusInProgress,
		Data: mustMarshalTaskData(t, map[string]any{
			"duration":   4,
			"resolution": "480p",
		}),
	}
	freshData := mustMarshalTaskData(t, map[string]any{
		"duration":   8,
		"resolution": "1080p",
	})

	resp := publicVideoTaskResponse(task, freshData)
	require.NotNil(t, resp.Duration)
	assert.Equal(t, 8.0, *resp.Duration)
	assert.Equal(t, "1080p", resp.Resolution)
}

func TestTaskModel2DtoStillExposesProgressForInternalUI(t *testing.T) {
	task := &model.Task{
		ID:       7,
		TaskID:   "task_internal",
		Status:   model.TaskStatusInProgress,
		Progress: "42%",
	}

	resp := TaskModel2Dto(task)
	assert.Equal(t, "42%", resp.Progress)
	assert.Equal(t, "task_internal", resp.TaskID)
	assert.Equal(t, "IN_PROGRESS", resp.Status)
}

func mustMarshalTaskData(t *testing.T, data map[string]any) []byte {
	t.Helper()
	if data == nil {
		return nil
	}
	body, err := common.Marshal(data)
	require.NoError(t, err)
	return body
}
