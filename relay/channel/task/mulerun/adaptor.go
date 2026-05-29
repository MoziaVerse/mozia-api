package mulerun

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	taskcommon "github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
)

// TaskAdaptor 把 mulerun studio 多模态端点（image / video / audio / music）
// 接入 new-api 的 task 子系统，对外暴露 OpenAI 标准 video task 接口：
//
//	POST /v1/video/generations              提交任务
//	GET  /v1/video/generations/:task_id     查询状态（new-api 自动管轮询）
//
// 路由层（router/video-router.go）已经实现了上述接入点，TaskAdaptor 只需要：
//   - 把客户端 model（例如 "bytedance/seedance-2.0-fast/text-to-video"）
//     翻译成上游 APIPath（查 registry.go）
//   - 透传 body（剥掉 model 字段以满足 upstream 的路径/模型一致性校验）
//   - 把上游 {task_info, videos/images/audios} 响应映射到 new-api TaskInfo
//
// 注：mulerun task_info.status 取值集合：
//
//	pending / queued / running / processing / completed / succeeded / failed
type TaskAdaptor struct {
	taskcommon.BaseBilling
	ChannelType int
	apiKey      string
	baseURL     string
}

// upstream 响应通用结构。
type taskInfoBody struct {
	ID        string      `json:"id"`
	Status    string      `json:"status"`
	Error     interface{} `json:"error,omitempty"`
	CreatedAt string      `json:"created_at,omitempty"`
	UpdatedAt string      `json:"updated_at,omitempty"`
}

type taskResponse struct {
	TaskInfo taskInfoBody  `json:"task_info"`
	Videos   []interface{} `json:"videos,omitempty"`
	Images   []interface{} `json:"images,omitempty"`
	Audios   []interface{} `json:"audios,omitempty"`
}

var (
	publicMulerunStudioPattern = regexp.MustCompile(`(?i)mulerun\s+studio`)
	publicMulerunPattern       = regexp.MustCompile(`(?i)mulerun`)
)

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.ChannelType = info.ChannelType
	a.baseURL = info.ChannelBaseUrl
	a.apiKey = info.ApiKey
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	if err := relaycommon.ValidateBasicTaskRequest(c, info, constant.TaskActionTextGenerate); err != nil {
		return err
	}
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return service.TaskErrorWrapper(err, "get_task_request_failed", http.StatusBadRequest)
	}

	// 优先用 UpstreamModelName（通过 channel model_mapping 映射后），
	// 没映射时退回客户端原始 model。
	modelID := strings.TrimSpace(info.UpstreamModelName)
	if modelID == "" {
		modelID = strings.TrimSpace(req.Model)
	}
	ep := LookupStudioEndpoint(modelID)
	if ep == nil {
		return service.TaskErrorWrapperLocal(
			fmt.Errorf("unknown multimodal task model: %q", modelID),
			"invalid_request", http.StatusBadRequest,
		)
	}

	// info.Action 会被 new-api 持久化进 task.Action（DB 列 varchar(40)），
	// polling 时由 FetchTask 接收 body["action"] 用来重建 upstream URL。
	//
	// 既不能存完整 APIPath（73 字符，超长 Insert 静默失败 → GET 报 task_not_exist），
	// 也不能存完整 cli_id（最长 56 字符）。改用 ShortKey（8 字符稳定 hash）。
	info.Action = ShortKey(ep.CLIID)
	return nil
}

func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	ep := LookupStudioByShortKey(info.Action)
	if ep == nil {
		return "", fmt.Errorf("task endpoint not resolved for action %q", info.Action)
	}
	return strings.TrimRight(strings.TrimSpace(a.baseURL), "/") + ep.APIPath, nil
}

func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.apiKey)
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	// 直接读原始 body，不走 dto 校验——mulerun 各 endpoint 参数 schema 不一样，
	// 强转 OpenAI dto.ImageRequest / dto.VideoRequest 会丢自定义字段。
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, errors.Wrap(err, "get_request_body_failed")
	}
	raw, err := storage.Bytes()
	if err != nil {
		return nil, errors.Wrap(err, "read_body_bytes_failed")
	}

	// upstream mulerun 会校验 body.model 必须等于 URL 路径的 model 段（短名，
	// 例如 "seedance-2.0-fast"），但客户端发的是完整 CLI ID
	// （"bytedance/seedance-2.0-fast/text-to-video"），不一致就被拒。
	// 大多数端点删掉 body.model 即可；少数端点（如 Google Veo）同时用
	// model 表示上游模型变体，需要删除网关路由 ID 后补回上游默认变体。
	ep := LookupStudioByShortKey(info.Action)
	prepared, err := prepareBodyForEndpoint(raw, ep)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(prepared), nil
}

func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	return channel.DoTaskApiRequest(a, c, info, requestBody)
}

func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (taskID string, taskData []byte, taskErr *dto.TaskError) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		taskErr = service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
		return
	}

	var tr taskResponse
	if err := common.Unmarshal(body, &tr); err != nil {
		taskErr = service.TaskErrorWrapper(
			errors.Wrap(err, fmt.Sprintf("body: %s", body)),
			"unmarshal_response_failed", http.StatusInternalServerError,
		)
		return
	}
	if tr.TaskInfo.ID == "" {
		taskErr = service.TaskErrorWrapperLocal(
			errors.New("empty task_info.id from upstream response"),
			"task_submit_failed", resp.StatusCode,
		)
		return
	}

	// 给客户端回写 OpenAI Sora 标准 video task 格式
	ov := dto.NewOpenAIVideo()
	ov.ID = info.PublicTaskID
	ov.TaskID = info.PublicTaskID
	ov.CreatedAt = time.Now().Unix()
	ov.Model = info.OriginModelName
	c.JSON(http.StatusOK, ov)

	return tr.TaskInfo.ID, body, nil
}

func (a *TaskAdaptor) FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, _ := body["task_id"].(string)
	shortKey, _ := body["action"].(string)
	if taskID == "" {
		return nil, fmt.Errorf("invalid task_id")
	}
	ep := LookupStudioByShortKey(shortKey)
	if ep == nil {
		return nil, fmt.Errorf("unknown task action %q; task endpoint may no longer be available", shortKey)
	}

	url := strings.TrimRight(strings.TrimSpace(baseUrl), "/") + ep.APIPath + "/" + taskID
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+key)

	client, err := service.GetHttpClientWithProxy(proxy)
	if err != nil {
		return nil, fmt.Errorf("new proxy http client failed: %w", err)
	}
	return client.Do(req)
}

func (a *TaskAdaptor) ParseTaskResult(respBody []byte) (*relaycommon.TaskInfo, error) {
	var tr taskResponse
	if err := common.Unmarshal(respBody, &tr); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal task response")
	}

	out := &relaycommon.TaskInfo{Code: 0}
	switch tr.TaskInfo.Status {
	case "pending", "queued":
		out.Status = model.TaskStatusQueued
	case "running", "processing":
		out.Status = model.TaskStatusInProgress
	case "completed", "succeeded":
		out.Status = model.TaskStatusSuccess
		out.Url = firstResultURL(tr)
	case "failed":
		out.Status = model.TaskStatusFailure
		if tr.TaskInfo.Error != nil {
			out.Reason = publicTaskMessage(fmt.Sprintf("%v", tr.TaskInfo.Error))
		} else {
			out.Reason = "task failed"
		}
	default:
		return nil, fmt.Errorf("unknown task status: %q", tr.TaskInfo.Status)
	}
	return out, nil
}

// ConvertToOpenAIVideo 在 GET /v1/video/generations/:task_id 时把 mulerun 原始
// 任务数据翻成 OpenAI Sora 标准 video object 返给客户端。
// 实现 channel.OpenAIVideoConverter 接口。
func (a *TaskAdaptor) ConvertToOpenAIVideo(originTask *model.Task) ([]byte, error) {
	var tr taskResponse
	_ = common.Unmarshal(originTask.Data, &tr)

	ov := dto.NewOpenAIVideo()
	ov.ID = originTask.TaskID
	ov.TaskID = originTask.TaskID
	ov.Status = originTask.Status.ToVideoStatus()
	ov.SetProgressStr(originTask.Progress)
	ov.CreatedAt = originTask.CreatedAt
	ov.CompletedAt = originTask.UpdatedAt

	if url := firstResultURL(tr); url != "" {
		ov.SetMetadata("url", url)
	}
	if tr.TaskInfo.Status == "failed" && tr.TaskInfo.Error != nil {
		msg := publicTaskMessage(fmt.Sprintf("%v", tr.TaskInfo.Error))
		ov.Error = &dto.OpenAIVideoError{Message: msg, Code: "task_failed"}
	}

	return common.Marshal(ov)
}

func (a *TaskAdaptor) GetModelList() []string {
	return StudioModelIDs()
}

func (a *TaskAdaptor) GetChannelName() string {
	return "mulerun"
}

// ============================================================================
// helpers
// ============================================================================

// firstResultURL 从 mulerun 成功响应里取第一个产物 URL。
// upstream 在 videos/images/audios 数组里可能返回字符串 URL，也可能返回
// {url: "..."} / {uri: "..."} 对象。
func firstResultURL(tr taskResponse) string {
	for _, list := range [][]interface{}{tr.Videos, tr.Images, tr.Audios} {
		for _, item := range list {
			switch v := item.(type) {
			case string:
				if u := strings.TrimSpace(v); u != "" {
					return u
				}
			case map[string]interface{}:
				if u, ok := v["url"].(string); ok && strings.TrimSpace(u) != "" {
					return strings.TrimSpace(u)
				}
				if u, ok := v["uri"].(string); ok && strings.TrimSpace(u) != "" {
					return strings.TrimSpace(u)
				}
			}
		}
	}
	return ""
}

// prepareBodyForEndpoint 删除网关路由用的 body.model，并按 endpoint 需要补上
// 上游自己的 model 变体字段。通过 map[string]any 中转保留嵌套结构。
func prepareBodyForEndpoint(raw []byte, ep *StudioEndpoint) ([]byte, error) {
	if len(raw) == 0 {
		return raw, nil
	}
	var m map[string]any
	if err := common.Unmarshal(raw, &m); err != nil {
		return raw, nil // 非 object 原样透传
	}
	upstreamModel := upstreamBodyModel(ep)
	if _, has := m["model"]; !has && upstreamModel == "" {
		return raw, nil
	}
	delete(m, "model")
	if upstreamModel != "" {
		m["model"] = upstreamModel
	}
	out, err := common.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("remarshal task body for upstream endpoint: %w", err)
	}
	return out, nil
}

func publicTaskMessage(message string) string {
	message = common.MaskSensitiveInfo(message)
	message = publicMulerunStudioPattern.ReplaceAllString(message, "upstream task")
	return publicMulerunPattern.ReplaceAllString(message, "upstream")
}

func upstreamBodyModel(ep *StudioEndpoint) string {
	if ep == nil {
		return ""
	}
	switch ep.CLIID {
	case "google/veo3/generation":
		return "veo-3.1"
	default:
		return ""
	}
}
