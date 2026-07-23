package globalaiopc

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
)

// ConvertToOpenAIVideo builds the public object from persisted task state.
// The upstream task ID and selected API key remain private task metadata.
func (a *TaskAdaptor) ConvertToOpenAIVideo(task *model.Task) ([]byte, error) {
	video := dto.NewOpenAIVideo()
	video.ID = task.TaskID
	video.TaskID = task.TaskID
	video.Model = task.Properties.OriginModelName
	video.Status = task.Status.ToVideoStatus()
	video.SetProgressStr(task.Progress)
	video.CreatedAt = task.CreatedAt
	video.CompletedAt = task.UpdatedAt

	switch task.Status {
	case model.TaskStatusSuccess:
		if resultURL := task.GetResultURL(); resultURL != "" {
			video.SetMetadata("url", resultURL)
		}
	case model.TaskStatusFailure:
		video.Error = &dto.OpenAIVideoError{
			Code:    "task_failed",
			Message: task.FailReason,
		}
	}
	return common.Marshal(video)
}
