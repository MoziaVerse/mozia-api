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
	TaskID   string          `json:"task_id"`
	ID       string          `json:"id"`
	Status   string          `json:"status"`
	Progress *int            `json:"progress,omitempty"`
	Result   *taskResult     `json:"result,omitempty"`
	Data     []resultItem    `json:"data,omitempty"`
	Output   json.RawMessage `json:"output,omitempty"`
	Video    *resultItem     `json:"video,omitempty"`
	URL      string          `json:"url,omitempty"`
	VideoURL string          `json:"video_url,omitempty"`
	Message  string          `json:"message,omitempty"`
	Reason   string          `json:"reason,omitempty"`
	Error    json.RawMessage `json:"error,omitempty"`
}

type taskResult struct {
	Data []resultItem `json:"data,omitempty"`
	URL  string       `json:"url,omitempty"`
}

type TaskAdaptor struct {
	taskcommon.BaseBilling
	baseURL        string
	resourcePath   string
	configuredName string
}

func NewSeedanceVideosTaskAdaptor() *TaskAdaptor {
	return &TaskAdaptor{resourcePath: "/videos", configuredName: "seedance-compatible-videos"}
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
	summary, err := parseVideoContentSummary(&req)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	modelName := upstreamModel(info, &req, summary)
	if isMiniMaxH3Model(modelName) {
		fields := mergeMetadataFields(readRawRequest(c), req.Metadata)
		if len(summary.ReferenceVideos) > 0 || len(summary.ReferenceAudios) > 0 {
			return service.TaskErrorWrapperLocal(
				fmt.Errorf("MiniMax H3 does not support reference_video/reference_audio in this adapter"),
				"invalid_request",
				http.StatusBadRequest,
			)
		}
		if err := validateMiniMaxH3Images(modelName, &req, summary, fields); err != nil {
			return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
		}
		if _, err := resolveMiniMaxH3Size(&req, fields); err != nil {
			return service.TaskErrorWrapperLocal(err, "invalid_size", http.StatusBadRequest)
		}
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
	return apiBaseURL(a.baseURL) + a.taskResourcePath(), nil
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
	payload := mergeMetadataFields(readRawRequest(c), req.Metadata)
	// These fields control routing, billing, and task lifecycle. Never let either
	// the raw request or metadata override the normalized values below.
	for _, key := range []string{"model", "prompt", "duration", "seconds", "async"} {
		delete(payload, key)
	}
	summary, err := parseVideoContentSummary(&req)
	if err != nil {
		return nil, err
	}
	modelName := upstreamModel(info, &req, summary)
	if modelName == "" {
		return nil, fmt.Errorf("model is required; configure the channel model list and model mapping")
	}
	isMiniMaxH3 := isMiniMaxH3Model(modelName)
	if isMiniMaxH3 {
		size, err := resolveMiniMaxH3Size(&req, payload)
		if err != nil {
			return nil, err
		}
		delete(payload, "content")
		delete(payload, "image")
		delete(payload, "input_reference")
		delete(payload, "resolution")
		delete(payload, "ratio")
		delete(payload, "aspect_ratio")
		delete(payload, "size")
		images := summary.LegacyImages()
		if len(req.Content) == 0 {
			switch {
			case req.InputReference != "":
				images = []string{req.InputReference}
			case len(req.Images) > 0:
				images = append(images, req.Images...)
			default:
				images = nil
			}
		}
		if len(images) > 0 {
			payload["images"] = images
		} else if len(req.Content) > 0 {
			delete(payload, "images")
		}
		if size != "" {
			payload["size"] = size
		}
	} else {
		delete(payload, "size")
		delete(payload, "input_reference")
		_, hasImage := payload["image"]
		_, hasImages := payload["images"]
		_, hasRefAssets := payload["ref_assets"]
		if len(req.Content) == 0 && !hasImage && !hasImages && !hasRefAssets {
			switch {
			case req.InputReference != "":
				payload["image"] = req.InputReference
			case len(req.Images) > 0:
				payload["images"] = req.Images
			}
		}
		if _, hasRatio := payload["ratio"]; !hasRatio && req.Size != "" {
			if _, hasAspectRatio := payload["aspect_ratio"]; !hasAspectRatio {
				payload["ratio"] = req.Size
			}
		}
	}

	duration := resolveDuration(&req)
	if summary.Prompt != "" {
		payload["prompt"] = summary.Prompt
	} else {
		payload["prompt"] = req.Prompt
	}
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
		return "", nil, service.TaskErrorWrapper(fmt.Errorf("decode %s response: %w", a.GetChannelName(), err), "unmarshal_response_failed", http.StatusInternalServerError)
	}
	upstreamTaskID := firstNonEmpty(submitted.TaskID, submitted.ID)
	if upstreamTaskID == "" {
		return "", nil, service.TaskErrorWrapperLocal(fmt.Errorf("empty task_id from %s upstream", a.GetChannelName()), "task_submit_failed", resp.StatusCode)
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
	requestURL := apiBaseURL(baseURL) + a.taskResourcePath() + "/" + url.PathEscape(taskID)
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
		return nil, fmt.Errorf("decode %s task response: %w", a.GetChannelName(), err)
	}
	taskInfo := &relaycommon.TaskInfo{TaskID: firstNonEmpty(resp.TaskID, resp.ID)}
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
	case "failed", "error", "timeout", "timed_out", "expired", "cancelled", "canceled":
		taskInfo.Status = model.TaskStatusFailure
		taskInfo.Reason = parseFailureReason(resp.Error, firstNonEmpty(resp.Message, resp.Reason), resp.Status, a.GetChannelName())
	default:
		return nil, fmt.Errorf("unknown %s task status: %s", a.GetChannelName(), resp.Status)
	}
	return taskInfo, nil
}

func (a *TaskAdaptor) GetModelList() []string {
	return []string{}
}

func (a *TaskAdaptor) GetChannelName() string {
	if a.configuredName != "" {
		return a.configuredName
	}
	return channelName
}

func (a *TaskAdaptor) taskResourcePath() string {
	if a.resourcePath != "" {
		return a.resourcePath
	}
	return "/video/generations"
}

func apiBaseURL(baseURL string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if strings.HasSuffix(baseURL, "/v1") {
		return baseURL
	}
	return baseURL + "/v1"
}

func upstreamModel(info *relaycommon.RelayInfo, req *relaycommon.TaskSubmitReq, summary relaycommon.VideoContentSummary) string {
	if info == nil {
		return ""
	}
	if info.IsModelMapped && strings.TrimSpace(info.UpstreamModelName) != "" {
		return strings.TrimSpace(info.UpstreamModelName)
	}
	modelName := firstNonEmpty(info.OriginModelName, info.UpstreamModelName)
	if !isExternalMiniMaxH3Model(modelName) {
		return modelName
	}
	if len(summary.ReferenceImages) > 0 {
		return "minimax/minimax-h3-ref2va"
	}
	if req != nil && len(req.Content) == 0 && (strings.TrimSpace(req.InputReference) != "" || len(req.Images) > 0) {
		return "minimax/minimax-h3-ref2va"
	}
	return "minimax/minimax-h3-fl2va"
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
	if url := rawResultURL(resp.Output); url != "" {
		return url
	}
	var outputs []json.RawMessage
	if err := common.Unmarshal(resp.Output, &outputs); err == nil {
		for _, raw := range outputs {
			if url := rawResultURL(raw); url != "" {
				return url
			}
		}
	}
	return ""
}

func rawResultURL(raw json.RawMessage) string {
	var directURL string
	if err := common.Unmarshal(raw, &directURL); err == nil && strings.TrimSpace(directURL) != "" {
		return strings.TrimSpace(directURL)
	}
	var item resultItem
	if err := common.Unmarshal(raw, &item); err == nil {
		return strings.TrimSpace(item.URL)
	}
	return ""
}

func parseFailureReason(raw json.RawMessage, fallback, status, adaptorName string) string {
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
	return adaptorName + " task " + strings.ToLower(strings.TrimSpace(status))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func parseVideoContentSummary(req *relaycommon.TaskSubmitReq) (relaycommon.VideoContentSummary, error) {
	if req == nil {
		return relaycommon.VideoContentSummary{}, nil
	}
	if len(req.Content) == 0 {
		return relaycommon.VideoContentSummary{Prompt: strings.TrimSpace(req.Prompt)}, nil
	}
	return req.ParseVideoContent()
}

func mergeMetadataFields(payload map[string]any, metadata map[string]any) map[string]any {
	merged := make(map[string]any, len(payload)+len(metadata))
	for key, value := range payload {
		if key == "metadata" {
			continue
		}
		merged[key] = value
	}
	for key, value := range metadata {
		merged[key] = value
	}
	return merged
}

func isMiniMaxH3Model(modelName string) bool {
	switch strings.ToLower(strings.TrimSpace(modelName)) {
	case "minimax/minimax-h3-fl2va", "minimax/minimax-h3-ref2va",
		"minimax-h3-fl2va-int8", "minimax-h3-ref2va-int8":
		return true
	default:
		return false
	}
}

func isExternalMiniMaxH3Model(modelName string) bool {
	return strings.EqualFold(strings.TrimSpace(modelName), "MiniMax-H3")
}

func validateMiniMaxH3Images(modelName string, req *relaycommon.TaskSubmitReq, summary relaycommon.VideoContentSummary, fields map[string]any) error {
	imageCount := len(summary.LegacyImages())
	if imageCount == 0 && req != nil {
		switch {
		case strings.TrimSpace(req.InputReference) != "":
			imageCount = 1
		case len(req.Images) > 0:
			imageCount = len(req.Images)
		}
	}
	if imageCount == 0 {
		switch values := fields["images"].(type) {
		case []string:
			imageCount = len(values)
		case []any:
			imageCount = len(values)
		}
	}
	switch strings.ToLower(strings.TrimSpace(modelName)) {
	case "minimax/minimax-h3-ref2va", "minimax-h3-ref2va-int8":
		if imageCount == 0 {
			return fmt.Errorf("MiniMax H3 ref2va requires at least 1 reference image")
		}
		if imageCount > 9 {
			return fmt.Errorf("MiniMax H3 ref2va supports at most 9 reference images")
		}
	case "minimax/minimax-h3-fl2va", "minimax-h3-fl2va-int8":
		if imageCount > 2 {
			return fmt.Errorf("MiniMax H3 fl2va supports at most first+last images")
		}
	}
	return nil
}

func resolveMiniMaxH3Size(req *relaycommon.TaskSubmitReq, fields map[string]any) (string, error) {
	if size, handled, err := explicitMiniMaxH3Size(firstNonEmpty(req.Size, stringField(fields, "size"))); handled {
		return size, err
	}

	resolution := strings.ToLower(strings.TrimSpace(firstNonEmpty(
		derefString(req.Resolution),
		stringField(fields, "resolution"),
	)))
	ratio := strings.TrimSpace(firstNonEmpty(
		derefString(req.Ratio),
		derefString(req.AspectRatio),
		req.Size,
		stringField(fields, "ratio"),
		stringField(fields, "aspect_ratio"),
	))
	if resolution == "" && ratio == "" {
		return "", nil
	}
	if ratio == "adaptive" {
		return "", fmt.Errorf("MiniMax H3 does not support adaptive ratio in this adapter")
	}
	if resolution == "" {
		resolution = "768p"
	}
	switch resolution {
	case "768p":
		return miniMaxH3768Size(ratio)
	case "2k", "adaptive":
		return "", fmt.Errorf("MiniMax H3 does not support resolution %q in this adapter", resolution)
	default:
		return "", fmt.Errorf("MiniMax H3 requires an explicit size or 768P resolution, got %q", resolution)
	}
}

func explicitMiniMaxH3Size(size string) (string, bool, error) {
	size = strings.TrimSpace(size)
	if size == "" {
		return "", false, nil
	}
	lower := strings.ToLower(size)
	separator := ""
	switch {
	case strings.Contains(lower, "x"):
		separator = "x"
	case strings.Contains(lower, ":"):
		separator = ":"
	default:
		return "", false, nil
	}
	parts := strings.SplitN(lower, separator, 2)
	if len(parts) != 2 {
		return "", true, fmt.Errorf("MiniMax H3 size %q is invalid", size)
	}
	left, errLeft := strconv.Atoi(strings.TrimSpace(parts[0]))
	right, errRight := strconv.Atoi(strings.TrimSpace(parts[1]))
	if errLeft != nil || errRight != nil {
		return "", true, fmt.Errorf("MiniMax H3 size %q is invalid", size)
	}
	if separator == ":" && left < 32 && right < 32 {
		return "", false, nil
	}
	normalized := strconv.Itoa(left) + "x" + strconv.Itoa(right)
	if left <= 0 || right <= 0 {
		return "", true, fmt.Errorf("MiniMax H3 size %q must use positive dimensions", normalized)
	}
	if left%32 != 0 || right%32 != 0 {
		return "", true, fmt.Errorf("MiniMax H3 size %q must use width/height multiples of 32", normalized)
	}
	if left > 1344*768/right {
		return "", true, fmt.Errorf("MiniMax H3 size %q exceeds the 1344x768 limit", normalized)
	}
	return normalized, true, nil
}

func miniMaxH3768Size(ratio string) (string, error) {
	switch strings.TrimSpace(ratio) {
	case "", "16:9":
		return "1344x768", nil
	case "9:16":
		return "768x1344", nil
	case "1:1":
		return "768x768", nil
	case "4:3":
		return "1024x768", nil
	case "3:4":
		return "768x1024", nil
	case "21:9":
		return "1344x576", nil
	default:
		return "", fmt.Errorf("MiniMax H3 does not support ratio %q in this adapter", ratio)
	}
}

func stringField(fields map[string]any, key string) string {
	if fields == nil {
		return ""
	}
	value, ok := fields[key]
	if !ok || value == nil {
		return ""
	}
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(text)
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}
