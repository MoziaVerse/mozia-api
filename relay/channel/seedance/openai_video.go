package seedance

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	taskcommon "github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
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
	completionTokens := int(gjson.GetBytes(originTask.Data, "usage.completion_tokens").Int())
	totalTokens := int(gjson.GetBytes(originTask.Data, "usage.total_tokens").Int())
	if totalTokens == 0 {
		totalTokens = completionTokens
	}
	if totalTokens > 0 {
		video.Usage = &dto.VideoTaskUsage{CompletionTokens: completionTokens, TotalTokens: totalTokens}
	}

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
	data, err := common.Marshal(video)
	if err != nil || video.ContentURL == "" {
		return data, err
	}
	if data, err = sjson.SetBytes(data, "content_url", video.ContentURL); err != nil {
		return nil, err
	}
	return sjson.SetBytes(data, "metadata.url", video.ContentURL)
}
