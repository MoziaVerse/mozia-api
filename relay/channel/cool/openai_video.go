package cool

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
)

// ConvertToOpenAIVideo 把已落库的 cool 任务转成 OpenAI Video API 响应，
// 供 GET /v1/videos/{task_id} 使用（见 relay_task.videoFetchByIDRespBodyBuilder）。
//
// 任务最新状态由后台轮询（FetchTask + ParseTaskResult）写库：成功时结果 URL 落在
// PrivateData.ResultURL / FailReason（由 GetResultURL 统一读取，新旧数据都兼容），
// 失败原因落在 FailReason。这里只在成功时给 metadata.url、失败时给 error，
// 避免把失败原因误塞进 url（model.Task.ToOpenAIVideo 的通用实现会有此问题）。
func (a *TaskAdaptor) ConvertToOpenAIVideo(originTask *model.Task) ([]byte, error) {
	ov := dto.NewOpenAIVideo()
	ov.ID = originTask.TaskID
	ov.TaskID = originTask.TaskID
	ov.Model = originTask.Properties.OriginModelName
	ov.Status = originTask.Status.ToVideoStatus()
	ov.SetProgressStr(originTask.Progress)
	ov.CreatedAt = originTask.CreatedAt
	ov.CompletedAt = originTask.UpdatedAt

	switch originTask.Status {
	case model.TaskStatusSuccess:
		if url := originTask.GetResultURL(); url != "" {
			ov.SetMetadata("url", url)
		}
	case model.TaskStatusFailure:
		ov.Error = &dto.OpenAIVideoError{
			Message: originTask.FailReason,
			Code:    "task_failed",
		}
	}

	return common.Marshal(ov)
}
