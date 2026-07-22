package modelrouter

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	modelrouterchannel "github.com/QuantumNous/new-api/relay/channel/modelrouter"
	taskcommon "github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

const (
	maxSeedanceImages = 4
	maxSeedanceVideos = 3
	maxSeedanceAudios = 1
	defaultDuration   = 5
)

// TaskAdaptor implements ModelRouter's asynchronous video protocol. The
// gateway uses a DashScope-style request envelope and returns a nested task ID:
//
//	POST /v1/videos/generations -> output.task_id
//	GET  /v1/tasks/{task_id}    -> output.task_status
//
// Native {model,input,parameters} requests are passed through. For the public
// /v1/videos API, a flat Seedance request is normalized into the native input
// envelope and constrained to this channel's 4-image/3-video/1-audio plan.
type TaskAdaptor struct {
	taskcommon.BaseBilling
	baseURL string
}

func (a *TaskAdaptor) Init(info *relaycommon.RelayInfo) {
	if info != nil && info.ChannelMeta != nil {
		a.baseURL = info.ChannelBaseUrl
	}
}

func (a *TaskAdaptor) ValidateRequestAndSetAction(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	if strings.TrimSpace(a.baseURL) == "" {
		return service.TaskErrorWrapperLocal(
			fmt.Errorf("channel base URL is required"),
			"invalid_channel_base_url",
			http.StatusBadRequest,
		)
	}

	payload, err := readRequestObject(c)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	input, nativeEnvelope := objectField(payload, "input")
	requestFields := payload
	if nativeEnvelope {
		requestFields = input
	}

	prompt := firstString(stringField(requestFields, "prompt"), stringField(payload, "prompt"))
	if prompt == "" {
		return service.TaskErrorWrapperLocal(fmt.Errorf("prompt is required"), "invalid_request", http.StatusBadRequest)
	}

	modelName := firstString(stringField(payload, "model"), originModel(info))
	if modelName == "" {
		return service.TaskErrorWrapperLocal(fmt.Errorf("model is required"), "missing_model", http.StatusBadRequest)
	}

	images, imageValues, err := materialValues(requestFields, []string{"image_urls", "images"}, "image")
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_materials", http.StatusBadRequest)
	}
	videos, _, err := materialValues(requestFields, []string{"video_urls", "videos"}, "")
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_materials", http.StatusBadRequest)
	}
	audios, _, err := materialValues(requestFields, []string{"audio_urls", "audios"}, "")
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_materials", http.StatusBadRequest)
	}

	// A flat request is the public 431 contract implemented by this adaptor.
	// Native envelopes remain usable by other ModelRouter video models; their
	// limits are only constrained when the selected model is Seedance.
	if !nativeEnvelope || requestTargetsSeedance(c, info, modelName) {
		if images > maxSeedanceImages {
			return materialLimitError("images", images, maxSeedanceImages)
		}
		if videos > maxSeedanceVideos {
			return materialLimitError("videos", videos, maxSeedanceVideos)
		}
		if audios > maxSeedanceAudios {
			return materialLimitError("audios", audios, maxSeedanceAudios)
		}
	}

	duration, durationPresent, err := durationValue(requestFields)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_duration", http.StatusBadRequest)
	}
	if !durationPresent {
		duration, durationPresent, err = durationValue(payload)
		if err != nil {
			return service.TaskErrorWrapperLocal(err, "invalid_duration", http.StatusBadRequest)
		}
	}
	if durationPresent && duration <= 0 {
		return service.TaskErrorWrapperLocal(
			fmt.Errorf("duration must be a positive number of seconds"),
			"invalid_duration",
			http.StatusBadRequest,
		)
	}
	size := ""
	if rawSize, exists := payload["size"]; exists && !nativeEnvelope {
		var ok bool
		size, ok = rawSize.(string)
		if !ok || strings.TrimSpace(size) == "" {
			return service.TaskErrorWrapperLocal(
				fmt.Errorf("size must be a supported aspect ratio, resolution, or WxH value"),
				"invalid_size",
				http.StatusBadRequest,
			)
		}
		if _, _, err := normalizeVideoSize(size); err != nil {
			return service.TaskErrorWrapperLocal(err, "invalid_size", http.StatusBadRequest)
		}
	}

	action := constant.TaskActionTextGenerate
	if images > 0 || videos > 0 || audios > 0 {
		action = constant.TaskActionGenerate
	}
	if videos > 0 || audios > 0 || images > 2 {
		action = constant.TaskActionReferenceGenerate
	}
	info.Action = action
	c.Set("task_request", relaycommon.TaskSubmitReq{
		Prompt:   prompt,
		Model:    modelName,
		Images:   imageValues,
		Size:     size,
		Duration: duration,
	})
	return nil
}

// EstimateBilling treats the configured ModelPrice as a per-second price for
// Seedance models. Different resolutions can be exposed as separate public
// model aliases with independent prices and model/parameter mappings.
func (a *TaskAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	if !isSeedanceModel(originModel(info)) && !isSeedanceModel(upstreamModel(info)) {
		return nil
	}
	req, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil
	}
	duration := req.Duration
	if duration <= 0 {
		duration = defaultDuration
	}
	return map[string]float64{"duration": float64(duration)}
}

func (a *TaskAdaptor) BuildRequestURL(_ *relaycommon.RelayInfo) (string, error) {
	baseURL, err := apiBaseURL(a.baseURL)
	if err != nil {
		return "", err
	}
	return baseURL + "/videos/generations", nil
}

func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+info.ApiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MR-Async", "true")
	idempotencyKey := ""
	if c != nil && c.Request != nil {
		idempotencyKey = strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	}
	if idempotencyKey == "" && info.TaskRelayInfo != nil {
		idempotencyKey = strings.TrimSpace(info.PublicTaskID)
	}
	if idempotencyKey != "" {
		req.Header.Set("Idempotency-Key", idempotencyKey)
	}
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	payload, err := readRequestObject(c)
	if err != nil {
		return nil, err
	}
	modelName := upstreamModel(info)
	if modelName == "" {
		return nil, fmt.Errorf("model is required; configure the channel model list and model mapping")
	}

	if _, nativeEnvelope := objectField(payload, "input"); nativeEnvelope {
		payload["model"] = modelName
	} else {
		payload = normalizeFlatRequest(payload, modelName)
	}

	body, err := common.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal ModelRouter request: %w", err)
	}
	return bytes.NewReader(body), nil
}

func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	resp, err := channel.DoTaskApiRequest(a, c, info, requestBody)
	if err == nil && resp != nil && resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
		// RelayTaskSubmit currently recognizes exactly 200 as a successful submit.
		resp.StatusCode = http.StatusOK
	}
	return resp, err
}

func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (string, []byte, *dto.TaskError) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
	}
	root, err := decodeResponse(body)
	if err != nil {
		return "", nil, service.TaskErrorWrapper(err, "unmarshal_response_failed", http.StatusInternalServerError)
	}
	output, _ := objectField(root, "output")
	taskID := stringField(output, "task_id")
	if taskID == "" {
		message := responseFailureMessage(root)
		if message == "" {
			message = "ModelRouter returned an empty output.task_id"
		}
		statusCode := resp.StatusCode
		if statusCode >= http.StatusOK && statusCode < http.StatusMultipleChoices {
			statusCode = http.StatusBadGateway
		}
		return "", nil, service.TaskErrorWrapper(fmt.Errorf("%s", message), "task_submit_failed", statusCode)
	}

	video := dto.NewOpenAIVideo()
	video.ID = info.PublicTaskID
	video.TaskID = info.PublicTaskID
	video.Model = info.OriginModelName
	video.CreatedAt = time.Now().Unix()
	c.JSON(http.StatusOK, video)
	return taskID, body, nil
}

func (a *TaskAdaptor) FetchTask(baseURL, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, _ := body["task_id"].(string)
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil, fmt.Errorf("invalid task_id")
	}
	baseURL, err := apiBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodGet, baseURL+"/tasks/"+url.PathEscape(taskID), nil)
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
	root, err := decodeResponse(respBody)
	if err != nil {
		return nil, err
	}
	output, ok := objectField(root, "output")
	if !ok {
		return nil, fmt.Errorf("ModelRouter task response is missing output")
	}

	status := strings.ToUpper(firstString(stringField(output, "task_status"), stringField(output, "status")))
	result := &relaycommon.TaskInfo{
		Code:   0,
		TaskID: stringField(output, "task_id"),
	}
	if progress := progressString(output["progress"]); progress != "" {
		result.Progress = progress
	}

	switch status {
	case "PENDING":
		result.Status = model.TaskStatusQueued
	case "RUNNING", "PROCESSING", "IN_PROGRESS":
		result.Status = model.TaskStatusInProgress
	case "SUCCEEDED", "SUCCESS", "COMPLETED":
		result.Status = model.TaskStatusSuccess
		result.Url = responseVideoURL(root)
		if result.Url == "" {
			return nil, fmt.Errorf("ModelRouter task succeeded without a video URL")
		}
	case "FAILED", "CANCELED", "CANCELLED", "UNKNOWN":
		result.Status = model.TaskStatusFailure
		result.Reason = responseFailureMessage(root)
		if result.Reason == "" {
			result.Reason = "task failed"
		}
	default:
		return nil, fmt.Errorf("unknown ModelRouter task status: %q", status)
	}
	return result, nil
}

func (a *TaskAdaptor) GetModelList() []string {
	return modelrouterchannel.ModelList
}

func (a *TaskAdaptor) GetChannelName() string {
	return modelrouterchannel.ChannelName
}

func apiBaseURL(rawURL string) (string, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(rawURL), "/")
	if baseURL == "" {
		return "", fmt.Errorf("channel base URL is required")
	}
	if strings.HasSuffix(baseURL, "/v1") {
		return baseURL, nil
	}
	return baseURL + "/v1", nil
}

func readRequestObject(c *gin.Context) (map[string]any, error) {
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, fmt.Errorf("read request body: %w", err)
	}
	body, err := storage.Bytes()
	if err != nil {
		return nil, fmt.Errorf("read request body: %w", err)
	}
	payload := make(map[string]any)
	if err := common.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("request body must be a JSON object: %w", err)
	}
	return payload, nil
}

func normalizeFlatRequest(payload map[string]any, modelName string) map[string]any {
	input := make(map[string]any)
	if metadata, ok := objectField(payload, "metadata"); ok {
		for key, value := range metadata {
			if key != "model" {
				input[key] = value
			}
		}
	}

	for _, key := range []string{
		"prompt", "generation_type", "duration", "aspect_ratio", "resolution",
		"generate_audio", "watermark", "web_search", "return_last_frame", "seed",
		"camera_fixed", "last_frame_image", "negative_prompt",
	} {
		if value, ok := payload[key]; ok {
			input[key] = value
		}
	}
	if _, ok := input["duration"]; !ok {
		if seconds, ok := normalizedSeconds(payload["seconds"]); ok {
			input["duration"] = seconds
		}
	}
	if size := stringField(payload, "size"); size != "" {
		resolution, aspectRatio, _ := normalizeVideoSize(size)
		if _, exists := input["resolution"]; !exists && resolution != "" {
			input["resolution"] = resolution
		}
		if _, exists := input["aspect_ratio"]; !exists && aspectRatio != "" {
			input["aspect_ratio"] = aspectRatio
		}
	}

	images := firstValue(payload, "image_urls", "images")
	if images == nil {
		if image := stringField(payload, "image"); image != "" {
			images = []any{image}
		}
	}
	if images != nil {
		input["image_urls"] = images
	}
	if videos := firstValue(payload, "video_urls", "videos"); videos != nil {
		input["video_urls"] = videos
	}
	if audios := firstValue(payload, "audio_urls", "audios"); audios != nil {
		input["audio_urls"] = audios
	}

	if isSeedanceModel(modelName) {
		if _, exists := input["generation_type"]; !exists {
			imageCount, _, _ := materialValues(input, []string{"image_urls"}, "")
			videoCount, _, _ := materialValues(input, []string{"video_urls"}, "")
			audioCount, _, _ := materialValues(input, []string{"audio_urls"}, "")
			switch {
			case videoCount > 0 || audioCount > 0 || imageCount > 2:
				input["generation_type"] = "reference-to-video"
			case imageCount > 0:
				input["generation_type"] = "image-to-video"
			default:
				input["generation_type"] = "text-to-video"
			}
		}
	}

	for _, key := range []string{
		"prompt", "generation_type", "duration", "seconds", "size", "aspect_ratio",
		"resolution", "generate_audio", "watermark", "web_search", "return_last_frame",
		"seed", "camera_fixed", "last_frame_image", "negative_prompt", "image", "images",
		"image_urls", "videos", "video_urls", "audios", "audio_urls", "metadata",
	} {
		delete(payload, key)
	}
	payload["model"] = modelName
	payload["input"] = input
	return payload
}

func materialValues(fields map[string]any, arrayKeys []string, singleKey string) (int, []string, error) {
	for _, key := range arrayKeys {
		value, exists := fields[key]
		if !exists || value == nil {
			continue
		}
		values, err := stringSlice(value)
		if err != nil {
			return 0, nil, fmt.Errorf("%s must be an array of strings", key)
		}
		return len(values), values, nil
	}
	if singleKey != "" {
		if value, exists := fields[singleKey]; exists && value != nil {
			text, ok := value.(string)
			if !ok {
				return 0, nil, fmt.Errorf("%s must be a string", singleKey)
			}
			text = strings.TrimSpace(text)
			if text != "" {
				return 1, []string{text}, nil
			}
		}
	}
	return 0, nil, nil
}

func stringSlice(value any) ([]string, error) {
	switch values := value.(type) {
	case []string:
		result := make([]string, 0, len(values))
		for _, value := range values {
			value = strings.TrimSpace(value)
			if value == "" {
				return nil, fmt.Errorf("material URL cannot be empty")
			}
			result = append(result, value)
		}
		return result, nil
	case []any:
		result := make([]string, 0, len(values))
		for _, value := range values {
			text, ok := value.(string)
			if !ok || strings.TrimSpace(text) == "" {
				return nil, fmt.Errorf("material URL must be a non-empty string")
			}
			result = append(result, strings.TrimSpace(text))
		}
		return result, nil
	default:
		return nil, fmt.Errorf("material list has invalid type")
	}
}

func materialLimitError(kind string, got, limit int) *dto.TaskError {
	return service.TaskErrorWrapperLocal(
		fmt.Errorf("ModelRouter Seedance 431 supports at most %d %s, got %d", limit, kind, got),
		"material_limit_exceeded",
		http.StatusBadRequest,
	)
}

func durationValue(fields map[string]any) (int, bool, error) {
	if fields == nil {
		return 0, false, nil
	}
	if raw, exists := fields["duration"]; exists {
		value, ok := normalizedSeconds(raw)
		if !ok {
			return 0, true, fmt.Errorf("duration must be an integer number of seconds")
		}
		return value, true, nil
	}
	if raw, exists := fields["seconds"]; exists {
		value, ok := normalizedSeconds(raw)
		if !ok {
			return 0, true, fmt.Errorf("seconds must be an integer number of seconds")
		}
		return value, true, nil
	}
	if parameters, ok := objectField(fields, "parameters"); ok {
		if raw, exists := parameters["duration"]; exists {
			value, valid := normalizedSeconds(raw)
			if !valid {
				return 0, true, fmt.Errorf("parameters.duration must be an integer number of seconds")
			}
			return value, true, nil
		}
	}
	return 0, false, nil
}

func normalizedSeconds(value any) (int, bool) {
	switch value := value.(type) {
	case float64:
		if value != float64(int(value)) {
			return 0, false
		}
		return int(value), true
	case int:
		return value, true
	case string:
		seconds, err := strconv.Atoi(strings.TrimSpace(value))
		return seconds, err == nil
	default:
		return 0, false
	}
}

func normalizeVideoSize(size string) (resolution string, aspectRatio string, err error) {
	size = strings.ToLower(strings.TrimSpace(size))
	switch size {
	case "16:9", "4:3", "1:1", "3:4", "9:16", "21:9", "adaptive":
		return "", size, nil
	case "480p", "720p", "1080p":
		return size, "", nil
	case "854x480":
		return "480p", "16:9", nil
	case "480x854":
		return "480p", "9:16", nil
	case "480x480":
		return "480p", "1:1", nil
	case "1280x720":
		return "720p", "16:9", nil
	case "720x1280":
		return "720p", "9:16", nil
	case "1024x1024":
		return "720p", "1:1", nil
	case "1920x1080", "1792x1024":
		return "1080p", "16:9", nil
	case "1080x1920", "1024x1792":
		return "1080p", "9:16", nil
	default:
		return "", "", fmt.Errorf("unsupported video size %q", size)
	}
}

func requestTargetsSeedance(c *gin.Context, info *relaycommon.RelayInfo, modelName string) bool {
	if isSeedanceModel(modelName) || isSeedanceModel(originModel(info)) || isSeedanceModel(upstreamModel(info)) {
		return true
	}
	mappingJSON := strings.TrimSpace(c.GetString("model_mapping"))
	if mappingJSON == "" || mappingJSON == "{}" {
		return false
	}
	mapping := make(map[string]string)
	if common.Unmarshal([]byte(mappingJSON), &mapping) != nil {
		return false
	}
	seen := make(map[string]bool)
	for current := modelName; current != "" && !seen[current]; {
		seen[current] = true
		mapped := strings.TrimSpace(mapping[current])
		if isSeedanceModel(mapped) {
			return true
		}
		current = mapped
	}
	return false
}

func isSeedanceModel(modelName string) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(modelName)), "seedance")
}

func originModel(info *relaycommon.RelayInfo) string {
	if info == nil {
		return ""
	}
	return strings.TrimSpace(info.OriginModelName)
}

func upstreamModel(info *relaycommon.RelayInfo) string {
	if info == nil {
		return ""
	}
	if info.ChannelMeta != nil {
		if modelName := strings.TrimSpace(info.UpstreamModelName); modelName != "" {
			return modelName
		}
	}
	return originModel(info)
}

func decodeResponse(body []byte) (map[string]any, error) {
	root := make(map[string]any)
	if err := common.Unmarshal(body, &root); err != nil {
		return nil, fmt.Errorf("decode ModelRouter response: %w", err)
	}
	return root, nil
}

func responseVideoURL(root map[string]any) string {
	output, _ := objectField(root, "output")
	if videoURL := firstString(stringField(output, "video_url"), stringField(output, "url")); videoURL != "" {
		return videoURL
	}
	if nested, ok := objectField(output, "output"); ok {
		if videoURL := firstString(stringField(nested, "video_url"), stringField(nested, "url")); videoURL != "" {
			return videoURL
		}
		if resultURL := firstResultURL(nested["results"]); resultURL != "" {
			return resultURL
		}
	}
	return firstResultURL(output["results"])
}

func firstResultURL(value any) string {
	items, ok := value.([]any)
	if !ok {
		return ""
	}
	for _, item := range items {
		if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
		if result, ok := item.(map[string]any); ok {
			if resultURL := firstString(stringField(result, "url"), stringField(result, "video_url")); resultURL != "" {
				return resultURL
			}
		}
	}
	return ""
}

func responseFailureMessage(root map[string]any) string {
	output, _ := objectField(root, "output")
	if nested, ok := objectField(output, "output"); ok {
		if message := firstString(stringField(nested, "message"), errorMessage(nested["error"])); message != "" {
			return message
		}
	}
	return firstString(
		stringField(output, "message"),
		errorMessage(output["error"]),
		stringField(root, "message"),
		errorMessage(root["error"]),
	)
}

func errorMessage(value any) string {
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text)
	}
	if object, ok := value.(map[string]any); ok {
		return firstString(stringField(object, "message"), stringField(object, "detail"), stringField(object, "code"))
	}
	return ""
}

func progressString(value any) string {
	switch value := value.(type) {
	case float64:
		return strconv.Itoa(int(value)) + "%"
	case int:
		return strconv.Itoa(value) + "%"
	case string:
		value = strings.TrimSpace(value)
		if value == "" {
			return ""
		}
		if strings.HasSuffix(value, "%") {
			return value
		}
		return value + "%"
	default:
		return ""
	}
}

func objectField(object map[string]any, key string) (map[string]any, bool) {
	if object == nil {
		return nil, false
	}
	value, ok := object[key].(map[string]any)
	return value, ok
}

func stringField(object map[string]any, key string) string {
	if object == nil {
		return ""
	}
	value, _ := object[key].(string)
	return strings.TrimSpace(value)
}

func firstString(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func firstValue(object map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, exists := object[key]; exists && value != nil {
			return value
		}
	}
	return nil
}
