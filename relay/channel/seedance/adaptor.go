package seedance

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
)

type submitResponse struct {
	TaskID   string `json:"task_id"`
	ID       string `json:"id"`
	Status   string `json:"status"`
	Progress int    `json:"progress"`
	Model    string `json:"model"`
	Mode     string `json:"mode"`
}

type resultItem struct {
	URL string `json:"url"`
}

type taskStatusResponse struct {
	TaskID   string            `json:"task_id"`
	Status   string            `json:"status"`
	Progress *int              `json:"progress,omitempty"`
	Result   *taskResult       `json:"result,omitempty"`
	Data     []resultItem      `json:"data,omitempty"`
	Output   []json.RawMessage `json:"output,omitempty"`
	Video    *resultItem       `json:"video,omitempty"`
	URL      string            `json:"url,omitempty"`
	VideoURL string            `json:"video_url,omitempty"`
	Message  string            `json:"message,omitempty"`
	Reason   string            `json:"reason,omitempty"`
	Error    json.RawMessage   `json:"error,omitempty"`
}

type taskResult struct {
	Data []resultItem `json:"data,omitempty"`
	URL  string       `json:"url,omitempty"`
}

type TaskAdaptor struct {
	taskcommon.BaseBilling
	baseURL string
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	a.baseURL = info.ChannelBaseUrl
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	if strings.TrimSpace(info.ChannelBaseUrl) == "" {
		return service.TaskErrorWrapperLocal(fmt.Errorf("channel base URL is required"), "invalid_channel_base_url", http.StatusBadRequest)
	}
	if taskErr := relaycommon.ValidateBasicTaskRequest(c, info, "generate"); taskErr != nil {
		return taskErr
	}
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	duration := resolveDuration(&req)
	if duration <= 0 {
		return service.TaskErrorWrapperLocal(
			fmt.Errorf("duration is required and must be a positive number of seconds"),
			"invalid_duration",
			http.StatusBadRequest,
		)
	}
	info.Action = "generate"
	return nil
}

func (a *TaskAdaptor) EstimateBilling(c *gin.Context, _ *relaycommon.RelayInfo) map[string]float64 {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil
	}
	return map[string]float64{"duration": float64(resolveDuration(&req))}
}

func (a *TaskAdaptor) BuildRequestURL(_ *relaycommon.RelayInfo) (string, error) {
	return apiBaseURL(a.baseURL) + "/video/generations", nil
}

func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+info.ApiKey)
	req.Header.Set("Content-Type", "application/json")
	idempotencyKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if idempotencyKey == "" {
		idempotencyKey = info.PublicTaskID
	}
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil, err
	}
	payload := readRawRequest(c)
	delete(payload, "metadata")
	for key, value := range req.Metadata {
		payload[key] = value
	}
	// These fields control routing, billing, and task lifecycle. Never let either
	// the raw request or metadata override the normalized values below.
	for _, key := range []string{"model", "prompt", "duration", "seconds", "async"} {
		delete(payload, key)
	}
	if _, hasRatio := payload["ratio"]; !hasRatio && req.Size != "" {
		if _, hasAspectRatio := payload["aspect_ratio"]; !hasAspectRatio {
			payload["ratio"] = req.Size
		}
	}
	delete(payload, "size")
	delete(payload, "input_reference")
	_, hasImage := payload["image"]
	_, hasImages := payload["images"]
	_, hasRefAssets := payload["ref_assets"]
	if !hasImage && !hasImages && !hasRefAssets {
		switch {
		case req.InputReference != "":
			payload["image"] = req.InputReference
		case len(req.Images) > 0:
			payload["images"] = req.Images
		}
	}

	duration := resolveDuration(&req)
	modelName := upstreamModel(info)
	if modelName == "" {
		return nil, fmt.Errorf("model is required; configure the channel model list and model mapping")
	}
	payload["prompt"] = req.Prompt
	payload["model"] = modelName
	payload["duration"] = duration
	payload["async"] = true

	data, err := common.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(data), nil
}

func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	resp, err := channel.DoTaskApiRequest(a, c, info, requestBody)
	if err == nil && resp != nil && resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
		// The shared task relay currently treats only 200 as a successful submit.
		// Normalize valid async 201/202 responses for compatible providers.
		resp.StatusCode = http.StatusOK
	}
	return resp, err
}

func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (string, []byte, *dto.TaskError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
	}
	var submitted submitResponse
	if err := common.Unmarshal(responseBody, &submitted); err != nil {
		return "", nil, service.TaskErrorWrapper(fmt.Errorf("decode seedance-compatible response: %w", err), "unmarshal_response_failed", http.StatusInternalServerError)
	}
	upstreamTaskID := firstNonEmpty(submitted.TaskID, submitted.ID)
	if upstreamTaskID == "" {
		return "", nil, service.TaskErrorWrapperLocal(fmt.Errorf("empty task_id from seedance-compatible upstream"), "task_submit_failed", resp.StatusCode)
	}

	video := dto.NewOpenAIVideo()
	video.ID = info.PublicTaskID
	video.TaskID = info.PublicTaskID
	video.CreatedAt = time.Now().Unix()
	video.Model = info.OriginModelName
	c.JSON(http.StatusOK, video)
	return upstreamTaskID, responseBody, nil
}

func (a *TaskAdaptor) FetchTask(baseURL, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok || strings.TrimSpace(taskID) == "" {
		return nil, fmt.Errorf("invalid task_id")
	}
	requestURL := apiBaseURL(baseURL) + "/video/generations/" + url.PathEscape(taskID)
	req, err := http.NewRequest(http.MethodGet, requestURL, nil)
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
	var resp taskStatusResponse
	if err := common.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("decode seedance-compatible task response: %w", err)
	}
	taskInfo := &relaycommon.TaskInfo{TaskID: resp.TaskID}
	if resp.Progress != nil {
		taskInfo.Progress = strconv.Itoa(*resp.Progress) + "%"
	}

	switch strings.ToLower(strings.TrimSpace(resp.Status)) {
	case "pending":
		taskInfo.Status = model.TaskStatusSubmitted
	case "queued":
		taskInfo.Status = model.TaskStatusQueued
	case "running", "processing", "in_progress":
		taskInfo.Status = model.TaskStatusInProgress
	case "succeeded", "completed", "success", "done":
		taskInfo.Status = model.TaskStatusSuccess
		taskInfo.Url = taskResultURL(&resp)
		if taskInfo.Url == "" {
			return nil, fmt.Errorf("seedance-compatible task succeeded without result URL")
		}
	case "failed", "error", "timeout", "timed_out", "expired", "cancelled", "canceled":
		taskInfo.Status = model.TaskStatusFailure
		taskInfo.Reason = parseFailureReason(resp.Error, firstNonEmpty(resp.Message, resp.Reason), resp.Status)
	default:
		return nil, fmt.Errorf("unknown seedance-compatible task status: %s", resp.Status)
	}
	return taskInfo, nil
}

func (a *TaskAdaptor) GetModelList() []string {
	return []string{}
}

func (a *TaskAdaptor) GetChannelName() string {
	return channelName
}

func apiBaseURL(baseURL string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if strings.HasSuffix(baseURL, "/v1") {
		return baseURL
	}
	return baseURL + "/v1"
}

func upstreamModel(info *relaycommon.RelayInfo) string {
	if info == nil {
		return ""
	}
	if info.IsModelMapped && strings.TrimSpace(info.UpstreamModelName) != "" {
		return strings.TrimSpace(info.UpstreamModelName)
	}
	return firstNonEmpty(info.OriginModelName, info.UpstreamModelName)
}

func resolveDuration(req *relaycommon.TaskSubmitReq) int {
	if req == nil {
		return 0
	}
	if req.Duration > 0 {
		return req.Duration
	}
	if seconds, err := strconv.Atoi(strings.TrimSpace(req.Seconds)); err == nil && seconds > 0 {
		return seconds
	}
	if req.Metadata != nil {
		switch value := req.Metadata["duration"].(type) {
		case float64:
			if value > 0 && value == float64(int(value)) {
				return int(value)
			}
		case int:
			if value > 0 {
				return value
			}
		case string:
			if duration, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && duration > 0 {
				return duration
			}
		}
	}
	return 0
}

func readRawRequest(c *gin.Context) map[string]any {
	raw := make(map[string]any)
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return raw
	}
	data, err := storage.Bytes()
	if err != nil {
		return raw
	}
	_ = common.Unmarshal(data, &raw)
	return raw
}

func taskResultURL(resp *taskStatusResponse) string {
	if resp == nil {
		return ""
	}
	if resp.Result != nil {
		if len(resp.Result.Data) > 0 && resp.Result.Data[0].URL != "" {
			return resp.Result.Data[0].URL
		}
		if resp.Result.URL != "" {
			return resp.Result.URL
		}
	}
	if len(resp.Data) > 0 && resp.Data[0].URL != "" {
		return resp.Data[0].URL
	}
	if resp.Video != nil && resp.Video.URL != "" {
		return resp.Video.URL
	}
	if directURL := firstNonEmpty(resp.URL, resp.VideoURL); directURL != "" {
		return directURL
	}
	for _, raw := range resp.Output {
		var directURL string
		if err := common.Unmarshal(raw, &directURL); err == nil && strings.TrimSpace(directURL) != "" {
			return strings.TrimSpace(directURL)
		}
		var item resultItem
		if err := common.Unmarshal(raw, &item); err == nil && strings.TrimSpace(item.URL) != "" {
			return strings.TrimSpace(item.URL)
		}
	}
	return ""
}

func parseFailureReason(raw json.RawMessage, fallback, status string) string {
	if len(raw) > 0 && string(raw) != "null" {
		var message string
		if err := common.Unmarshal(raw, &message); err == nil && strings.TrimSpace(message) != "" {
			return message
		}
		var detail struct {
			Message string `json:"message"`
		}
		if err := common.Unmarshal(raw, &detail); err == nil && strings.TrimSpace(detail.Message) != "" {
			return detail.Message
		}
	}
	if strings.TrimSpace(fallback) != "" {
		return strings.TrimSpace(fallback)
	}
	return "seedance-compatible task " + strings.ToLower(strings.TrimSpace(status))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
