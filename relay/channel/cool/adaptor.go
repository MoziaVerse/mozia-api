package cool

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	taskcommon "github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"github.com/pkg/errors"
)

// ============================
// Request / Response payloads
// ============================

type fileRef struct {
	URL  string `json:"url"`
	Type string `json:"type,omitempty"`
	Name string `json:"name,omitempty"`
}

type generateRequest struct {
	Prompt              string    `json:"prompt"`
	Model               string    `json:"model,omitempty"`
	Ratio               string    `json:"ratio,omitempty"`
	Duration            int       `json:"duration,omitempty"`
	Resolution          string    `json:"resolution,omitempty"`
	OptimizePromptAsync *bool     `json:"optimizePromptAsync,omitempty"`
	Timeout             int       `json:"timeout,omitempty"`
	Files               []fileRef `json:"files,omitempty"`
}

type submitResponse struct {
	TaskID  string `json:"task_id"`
	Status  string `json:"status"`
	Message string `json:"message"`
	PollURL string `json:"poll_url,omitempty"`
}

type taskResult struct {
	URL          string  `json:"url,omitempty"`
	MediaType    string  `json:"media_type,omitempty"`
	ResourceID   string  `json:"resource_id,omitempty"`
	Duration     float64 `json:"duration,omitempty"`
	Resolution   string  `json:"resolution,omitempty"`
	ThumbnailURL string  `json:"thumbnail_url,omitempty"`
}

type taskStatusResponse struct {
	TaskID      string      `json:"task_id"`
	Status      string      `json:"status"`
	Model       string      `json:"model"`
	MediaType   string      `json:"media_type"`
	Prompt      string      `json:"prompt"`
	Result      *taskResult `json:"result,omitempty"`
	Error       string      `json:"error,omitempty"`
	CreatedAt   string      `json:"created_at,omitempty"`
	CompletedAt string      `json:"completed_at,omitempty"`
}

// ============================
// Adaptor implementation
// ============================

type TaskAdaptor struct {
	taskcommon.BaseBilling
	ChannelType int
	baseURL     string
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.ChannelType = info.ChannelType
	a.baseURL = info.ChannelBaseUrl
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	if err := relaycommon.ValidateBasicTaskRequest(c, info, "generate"); err != nil {
		return err
	}
	// Cool 没有 firstTail / reference 之类的子动作；按图/文/视频统一走 generate。
	info.Action = "generate"
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	v, exists := c.Get("task_request")
	if !exists {
		return nil, fmt.Errorf("request not found in context")
	}
	req := v.(relaycommon.TaskSubmitReq)

	body, err := a.convertToRequestPayload(c, &req, info)
	if err != nil {
		return nil, err
	}
	data, err := common.Marshal(body)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(data), nil
}

func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	return fmt.Sprintf("%s/v1/cool/generate", a.baseURL), nil
}

func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+info.ApiKey)
	return nil
}

func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	return channel.DoTaskApiRequest(a, c, info, requestBody)
}

func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (taskID string, taskData []byte, taskErr *dto.TaskError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		taskErr = service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
		return
	}

	var sub submitResponse
	if err := common.Unmarshal(responseBody, &sub); err != nil {
		taskErr = service.TaskErrorWrapper(errors.Wrap(err, fmt.Sprintf("%s", responseBody)), "unmarshal_response_failed", http.StatusInternalServerError)
		return
	}
	if sub.TaskID == "" {
		taskErr = service.TaskErrorWrapperLocal(fmt.Errorf("empty task_id from cool: %s", responseBody), "task_submit_failed", resp.StatusCode)
		return
	}

	ov := dto.NewOpenAIVideo()
	ov.ID = info.PublicTaskID
	ov.TaskID = info.PublicTaskID
	ov.CreatedAt = time.Now().Unix()
	ov.Model = info.OriginModelName
	c.JSON(http.StatusOK, ov)

	return sub.TaskID, responseBody, nil
}

func (a *TaskAdaptor) FetchTask(baseUrl, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok || taskID == "" {
		return nil, fmt.Errorf("invalid task_id")
	}
	url := fmt.Sprintf("%s/v1/cool/task/%s", baseUrl, taskID)
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
	taskInfo := &relaycommon.TaskInfo{}

	var resp taskStatusResponse
	if err := common.Unmarshal(respBody, &resp); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal cool task response")
	}

	switch resp.Status {
	case "pending":
		taskInfo.Status = model.TaskStatusSubmitted
	case "running":
		taskInfo.Status = model.TaskStatusInProgress
	case "success":
		taskInfo.Status = model.TaskStatusSuccess
		if resp.Result != nil {
			taskInfo.Url = resp.Result.URL
		}
	case "failed":
		taskInfo.Status = model.TaskStatusFailure
		taskInfo.Reason = resp.Error
	default:
		return nil, fmt.Errorf("unknown cool task status: %s", resp.Status)
	}
	return taskInfo, nil
}

func (a *TaskAdaptor) GetModelList() []string {
	return ModelList
}

func (a *TaskAdaptor) GetChannelName() string {
	return ChannelName
}

// ============================
// helpers
// ============================

func (a *TaskAdaptor) convertToRequestPayload(c *gin.Context, req *relaycommon.TaskSubmitReq, info *relaycommon.RelayInfo) (*generateRequest, error) {
	modelName := taskcommon.DefaultString(info.UpstreamModelName, "")
	if modelName == "" {
		modelName = req.Model
	}
	if modelName == "" {
		if len(req.Images) > 0 {
			modelName = defaultVideoModel
		} else {
			modelName = defaultImageModel
		}
	}

	r := &generateRequest{
		Prompt:   req.Prompt,
		Ratio:    req.Size,
		Duration: req.Duration,
	}

	for _, u := range req.Images {
		if u == "" {
			continue
		}
		r.Files = append(r.Files, fileRef{URL: u, Type: "image"})
	}
	if req.Image != "" {
		r.Files = append(r.Files, fileRef{URL: req.Image, Type: "image"})
	}

	// 允许通过 metadata 传任意原生字段（model / resolution / optimizePromptAsync /
	// timeout / 自定义 files 列表等）。参考视频通过 files 里 type:video 的素材传入。
	if err := taskcommon.UnmarshalMetadata(req.Metadata, r); err != nil {
		return nil, errors.Wrap(err, "unmarshal metadata failed")
	}

	// Seedance SKU：把对外模型名（cool:seedance_2_480p_video 等）还原成 cool 上游
	// 真实 model key（seedance_2 / seedance_2_fast）+ 独立的 resolution 参数。
	// cool 的 model 字段只认基础 key，分辨率/参考视频后缀只是 mozia 侧的计费 SKU。
	if spec := parseSeedanceModel(modelName); spec.OK {
		r.Model = spec.Base
		if spec.Dynamic {
			// 动态 SKU：分辨率取自请求参数，缺省回落 720p（与计费一致）。
			resolution := resolveSeedanceResolution(c, req)
			if resolution == "" {
				resolution = seedanceDefaultResolution
			}
			r.Resolution = resolution
		} else {
			// 固定 SKU：分辨率后缀优先，忽略请求体（别名即 SKU，保证计费==转发）。
			r.Resolution = spec.Resolution
		}
		return r, nil
	}

	// 其它 cool 模型：剥掉对外 cool: 前缀（cool 的 model key 不带前缀），
	// 并兜底转发顶层 resolution（TaskSubmitReq 不含 resolution 字段会被丢弃）。
	if r.Model == "" {
		r.Model = stripCoolPrefix(modelName)
	}
	if resolution := resolveSeedanceResolution(c, req); resolution != "" {
		r.Resolution = resolution
	}
	return r, nil
}
