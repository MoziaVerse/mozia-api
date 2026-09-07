package relay

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestVideoTaskIDsForArtsAndSeedance(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/tasks.db"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	originalDB := model.DB
	model.DB = db
	t.Cleanup(func() {
		model.DB = originalDB
		require.NoError(t, sqlDB.Close())
	})
	require.NoError(t, db.AutoMigrate(&model.Task{}, &model.Channel{}))

	legacyTask := &model.Task{
		TaskID: "task_legacy", UserId: 42, ChannelId: constant.ChannelTypeMoziaArtsapi,
		Platform:    constant.TaskPlatform(strconv.Itoa(constant.ChannelTypeMoziaArtsapi)),
		Status:      model.TaskStatusSubmitted,
		PrivateData: model.TaskPrivateData{UpstreamTaskID: "cgt-legacy"},
	}
	require.NoError(t, legacyTask.Insert())
	tasks := []*model.Task{legacyTask}

	for _, tc := range []struct {
		channelType int
		wantID      string
	}{
		{constant.ChannelTypeMoziaArtsapi, "cgt-206"},
		{constant.ChannelTypeMoziaSeedanceGen, "task_203"},
		{constant.ChannelTypeMoziaSeedanceVideos, "task_204"},
	} {
		suffix := strconv.Itoa(tc.channelType)
		platform := constant.TaskPlatform(suffix)
		info := &relaycommon.RelayInfo{
			UserId: 42, OriginModelName: "public-model",
			TaskRelayInfo: &relaycommon.TaskRelayInfo{PublicTaskID: "task_" + suffix},
			ChannelMeta:   &relaycommon.ChannelMeta{ChannelId: tc.channelType, ChannelType: tc.channelType},
		}
		adaptor := GetTaskAdaptor(platform)
		require.NotNil(t, adaptor)
		adaptor.Init(info)
		writer := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(writer)
		responseBody := `{"id":"cgt-` + suffix + `","status":"pending"}`
		upstreamID, taskData, taskErr := adaptor.DoResponse(ctx, &http.Response{
			StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(responseBody)),
		}, info)
		require.Nil(t, taskErr)
		assert.Equal(t, "cgt-"+suffix, upstreamID)
		assert.JSONEq(t, responseBody, string(taskData))
		var submitted dto.OpenAIVideo
		require.NoError(t, common.Unmarshal(writer.Body.Bytes(), &submitted))
		assert.Equal(t, tc.wantID, submitted.ID)
		assert.Equal(t, tc.wantID, submitted.TaskID)

		task := model.InitTask(platform, info)
		task.PrivateData.UpstreamTaskID = upstreamID
		task.Data = taskData
		require.NoError(t, task.Insert())
		require.NoError(t, db.Create(&model.Channel{Id: tc.channelType, Type: tc.channelType}).Error)
		assert.Equal(t, tc.wantID, task.TaskID)
		tasks = append(tasks, task)
	}

	for _, task := range tasks {
		stored, exists, err := model.GetByTaskId(42, task.TaskID)
		require.NoError(t, err)
		require.True(t, exists)
		assert.Equal(t, task.PrivateData.UpstreamTaskID, stored.GetUpstreamTaskID())
		for _, route := range []string{"/v1/video/generations/", "/v1/videos/"} {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodGet, route+task.TaskID, nil)
			ctx.Params = gin.Params{{Key: "task_id", Value: task.TaskID}}
			ctx.Set("id", 42)
			body, taskErr := videoFetchByIDRespBodyBuilder(ctx)
			require.Nil(t, taskErr)
			var fetched dto.OpenAIVideo
			require.NoError(t, common.Unmarshal(body, &fetched))
			assert.Equal(t, task.TaskID, fetched.ID)
			assert.Equal(t, task.TaskID, fetched.TaskID)

			ctx.Set("id", 99)
			_, taskErr = videoFetchByIDRespBodyBuilder(ctx)
			require.NotNil(t, taskErr)
			assert.Equal(t, "task_not_exist", taskErr.Code)
		}
	}
}

func TestPublicVideoTaskResponseBodyFlatContract(t *testing.T) {
	originalServerAddress := system_setting.ServerAddress
	system_setting.ServerAddress = "https://gateway.example"
	t.Cleanup(func() {
		system_setting.ServerAddress = originalServerAddress
	})

	successTask := &model.Task{
		TaskID:    "task_success",
		UserId:    42,
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
			"usage": map[string]any{
				"completion_tokens": 68361,
				"total_tokens":      68361,
			},
			"output": map[string]any{
				"filename": "final.mp4",
			},
			"result": map[string]any{
				"duration":   5.5,
				"resolution": "720p",
			},
		}),
	}

	body, err := publicVideoTaskResponseBody(successTask, successTask.Data)
	require.NoError(t, err)
	assert.Contains(t, string(body), "&signature=")
	assert.NotContains(t, string(body), `\u0026`)

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
	signedURL, err := url.Parse(resp.Content.URL)
	require.NoError(t, err)
	assert.Equal(t, "/v1/video/generations/task_success/content/final.mp4", signedURL.Path)
	assert.Equal(t, "42", signedURL.Query().Get("uid"))
	assert.NotEmpty(t, signedURL.Query().Get("signature"))
	expiresAt, err := strconv.ParseInt(signedURL.Query().Get("expires"), 10, 64)
	require.NoError(t, err)
	assert.Equal(t, expiresAt, resp.Content.ExpiresAt)
	assert.WithinDuration(t, time.Now().Add(24*time.Hour), time.Unix(expiresAt, 0), 2*time.Second)
	assert.Nil(t, resp.Error)
	assert.Equal(t, "720p", resp.Resolution)
	assert.Equal(t, "16:9", resp.Ratio)
	require.NotNil(t, resp.Duration)
	assert.Equal(t, 5.5, *resp.Duration)
	require.NotNil(t, resp.Usage)
	assert.Equal(t, 68361, resp.Usage.CompletionTokens)
	assert.Equal(t, 68361, resp.Usage.TotalTokens)

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
	assert.Contains(t, raw, "usage")
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
