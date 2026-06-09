package cool

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
)

func TestConvertToOpenAIVideoSuccess(t *testing.T) {
	task := &model.Task{
		TaskID:     "task_abc",
		Status:     model.TaskStatusSuccess,
		Progress:   "100%",
		CreatedAt:  100,
		UpdatedAt:  200,
		FailReason: "https://cdn-ap.cool.tv/videos/x.mp4", // worker 成功时把结果 URL 存这里
	}
	task.Properties.OriginModelName = "doubao/seedance-2.0"

	ov := convertVideo(t, task)
	if ov.Status != dto.VideoStatusCompleted {
		t.Fatalf("status: got %q want %q", ov.Status, dto.VideoStatusCompleted)
	}
	if ov.ID != "task_abc" || ov.Model != "doubao/seedance-2.0" {
		t.Fatalf("id/model: %q / %q", ov.ID, ov.Model)
	}
	if ov.Metadata["url"] != "https://cdn-ap.cool.tv/videos/x.mp4" {
		t.Fatalf("url: %v", ov.Metadata["url"])
	}
	if ov.Error != nil {
		t.Fatalf("unexpected error: %+v", ov.Error)
	}
}

func TestConvertToOpenAIVideoFailureSetsErrorNotURL(t *testing.T) {
	task := &model.Task{
		TaskID:     "task_def",
		Status:     model.TaskStatusFailure,
		FailReason: "input image may contain real person",
	}
	ov := convertVideo(t, task)
	if ov.Status != dto.VideoStatusFailed {
		t.Fatalf("status: got %q want %q", ov.Status, dto.VideoStatusFailed)
	}
	if ov.Error == nil || ov.Error.Message != "input image may contain real person" {
		t.Fatalf("error: %+v", ov.Error)
	}
	if _, ok := ov.Metadata["url"]; ok {
		t.Fatalf("failed task must not carry url, got %v", ov.Metadata)
	}
}

func TestConvertToOpenAIVideoInProgress(t *testing.T) {
	task := &model.Task{TaskID: "task_ghi", Status: model.TaskStatusInProgress, Progress: "30%"}
	ov := convertVideo(t, task)
	if ov.Status != dto.VideoStatusInProgress {
		t.Fatalf("status: got %q want %q", ov.Status, dto.VideoStatusInProgress)
	}
	if ov.Error != nil || ov.Metadata != nil {
		t.Fatalf("in-progress task should be clean: err=%+v meta=%v", ov.Error, ov.Metadata)
	}
}

func convertVideo(t *testing.T, task *model.Task) dto.OpenAIVideo {
	t.Helper()
	body, err := (&TaskAdaptor{}).ConvertToOpenAIVideo(task)
	if err != nil {
		t.Fatalf("ConvertToOpenAIVideo: %v", err)
	}
	var ov dto.OpenAIVideo
	if err := common.Unmarshal(body, &ov); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return ov
}
