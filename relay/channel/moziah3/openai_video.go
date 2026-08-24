package moziah3

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	taskcommon "github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
)

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
			if strings.TrimRight(resultURL, "/") == strings.TrimRight(taskcommon.BuildProxyURL(task.TaskID), "/") {
				resultURL = taskcommon.BuildSignedVideoProxyURL(task.UserId, task.TaskID, taskcommon.TaskVideoFilename(task))
			}
			video.ContentURL = resultURL
			video.SetMetadata("url", resultURL)
		}
	case model.TaskStatusFailure:
		video.Error = &dto.OpenAIVideoError{Message: task.FailReason, Code: "task_failed"}
	}
	return common.MarshalNoEscapeHTML(video)
}
