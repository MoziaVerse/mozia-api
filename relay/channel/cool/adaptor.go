package cool

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
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
	var content relaycommon.VideoContentSummary
	if len(req.Content) > 0 {
		var err error
		content, err = req.ParseVideoContent()
		if err != nil {
			return nil, errors.Wrap(err, "parse content failed")
		}
	}

	modelName := taskcommon.DefaultString(info.UpstreamModelName, "")
	if modelName == "" {
		modelName = req.Model
	}
	if modelName == "" {
		if len(req.Images) > 0 || len(content.ReferenceVideos) > 0 || len(content.ReferenceAudios) > 0 {
			modelName = defaultVideoModel
		} else {
			modelName = defaultImageModel
		}
	}

	r := &generateRequest{
		Files: buildLegacyImageFiles(req),
	}

	// 允许通过 metadata 传任意原生字段（model / resolution / optimizePromptAsync /
	// timeout / 自定义 files 列表等）。参考视频通过 files 里 type:video 的素材传入。
	if err := taskcommon.UnmarshalMetadata(req.Metadata, r); err != nil {
		return nil, errors.Wrap(err, "unmarshal metadata failed")
	}

	if len(req.Content) > 0 {
		r.Files = buildContentFiles(content)
		r.Prompt = firstNonEmpty(content.Prompt, r.Prompt)
	} else if prompt := strings.TrimSpace(req.Prompt); prompt != "" {
		r.Prompt = prompt
	}
	if ratio := resolveCoolRatio(req); ratio != "" {
		r.Ratio = ratio
	}
	if duration, ok := resolveCoolDuration(req); ok {
		r.Duration = duration
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
	// 并转发统一接口的顶层 resolution。
	if r.Model == "" {
		r.Model = stripCoolPrefix(modelName)
	}
	if resolution := resolveCoolResolution(req); resolution != "" {
		r.Resolution = resolution
	}
	return r, nil
}

func buildLegacyImageFiles(req *relaycommon.TaskSubmitReq) []fileRef {
	files := make([]fileRef, 0, len(req.Images)+1)
	for _, u := range req.Images {
		if strings.TrimSpace(u) == "" {
			continue
		}
		files = append(files, fileRef{URL: u, Type: "image"})
	}
	if image := strings.TrimSpace(req.Image); image != "" {
		files = append(files, fileRef{URL: image, Type: "image"})
	}
	return files
}

func buildContentFiles(summary relaycommon.VideoContentSummary) []fileRef {
	files := make([]fileRef, 0, len(summary.ReferenceImages)+len(summary.ReferenceVideos)+len(summary.ReferenceAudios)+2)
	if summary.FirstFrameURL != "" {
		files = append(files, fileRef{URL: summary.FirstFrameURL, Type: "image", Name: "start"})
	}
	if summary.LastFrameURL != "" {
		files = append(files, fileRef{URL: summary.LastFrameURL, Type: "image", Name: "end"})
	}
	files = appendContentRefs(files, summary.ReferenceImages, "image")
	files = appendContentRefs(files, summary.ReferenceVideos, "video")
	files = appendContentRefs(files, summary.ReferenceAudios, "audio")
	return files
}

func appendContentRefs(files []fileRef, urls []string, fileType string) []fileRef {
	for _, u := range urls {
		if strings.TrimSpace(u) == "" {
			continue
		}
		files = append(files, fileRef{URL: u, Type: fileType})
	}
	return files
}

func resolveCoolRatio(req *relaycommon.TaskSubmitReq) string {
	if req == nil {
		return ""
	}
	return firstNonEmpty(derefString(req.Ratio), derefString(req.AspectRatio), req.Size)
}

func resolveCoolResolution(req *relaycommon.TaskSubmitReq) string {
	if req == nil {
		return ""
	}
	return strings.TrimSpace(derefString(req.Resolution))
}

func resolveCoolDuration(req *relaycommon.TaskSubmitReq) (int, bool) {
	if req == nil {
		return 0, false
	}
	if req.Duration > 0 {
		return req.Duration, true
	}
	if seconds, err := strconv.Atoi(strings.TrimSpace(req.Seconds)); err == nil && seconds > 0 {
		return seconds, true
	}
	return 0, false
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}
