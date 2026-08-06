package seedance

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	taskcommon "github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
)

func (a *TaskAdaptor) ConvertToOpenAIVideo(originTask *model.Task) ([]byte, error) {
	video := dto.NewOpenAIVideo()
	video.ID = originTask.TaskID
	video.TaskID = originTask.TaskID
	video.Model = originTask.Properties.OriginModelName
	video.Status = originTask.Status.ToVideoStatus()
	video.SetProgressStr(originTask.Progress)
	video.CreatedAt = originTask.CreatedAt
	video.CompletedAt = originTask.UpdatedAt

	switch originTask.Status {
	case model.TaskStatusSuccess:
		if resultURL := originTask.GetResultURL(); resultURL != "" {
			if strings.TrimRight(resultURL, "/") == strings.TrimRight(taskcommon.BuildProxyURL(originTask.TaskID), "/") {
				resultURL = taskcommon.BuildSignedVideoProxyURL(originTask.UserId, originTask.TaskID, taskcommon.TaskVideoFilename(originTask))
			}
			video.ContentURL = resultURL
			video.SetMetadata("url", resultURL)
		}
	case model.TaskStatusFailure:
		video.Error = &dto.OpenAIVideoError{
			Message: originTask.FailReason,
			Code:    "task_failed",
		}
	}
	return common.Marshal(video)
}
