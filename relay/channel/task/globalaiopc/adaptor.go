package globalaiopc

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
	globalaiopcchannel "github.com/QuantumNous/new-api/relay/channel/globalaiopc"
	taskcommon "github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayhelper "github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

const (
	specialSubmitPath  = "/seedance-special/videos"
	resultPathPrefix   = "/result/"
	maxSpecialImages   = 9
	maxSpecialVideos   = 3
	maxSpecialAudios   = 3
	minSpecialDuration = 4
	maxSpecialDuration = 15
)

type seedanceSpecialSpec struct {
	Name            string
	RequireVideoRef bool
}

type requestSummary struct {
	Prompt      string
	Duration    int
	HasDuration bool
	Images      []string
	Videos      []string
	Audios      []string
	FirstImage  string
	LastImage   string
}

// TaskAdaptor implements GlobalAiOpc's Seedance2.0 special asynchronous video protocol.
//
//	POST /v1/seedance-special/videos
//	GET  /v1/result/{task_id}
//
// Provider-native payloads are passed through after model remapping. Public
// /v1/videos and /v1/video/generations style flat requests are normalized into
// the official GlobalAiOpc Seedance-special schema.
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
	modelName, spec, err := resolveModelSpec(c, info, payload)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_model", http.StatusBadRequest)
	}
	setResolvedModel(info, modelName)

	summary, err := validatePayload(spec, payload)
	if err != nil {
		return classifyValidationError(err)
	}

	action := constant.TaskActionTextGenerate
	if len(summary.Images) > 0 || summary.FirstImage != "" {
		action = constant.TaskActionGenerate
	}
	if len(summary.Videos) > 0 || len(summary.Audios) > 0 || len(summary.Images) > 1 || summary.LastImage != "" {
		action = constant.TaskActionReferenceGenerate
	}
	info.Action = action
	c.Set("task_request", relaycommon.TaskSubmitReq{
		Prompt:   summary.Prompt,
		Model:    modelName,
		Images:   append(append([]string{}, summary.Images...), frameImages(summary.FirstImage, summary.LastImage)...),
		Duration: summary.Duration,
		Size:     stringField(payload, "size"),
	})
	return nil
}

func (a *TaskAdaptor) BuildRequestURL(info *relaycommon.RelayInfo) (string, error) {
	baseURL, err := apiBaseURL(a.baseURL)
	if err != nil {
		return "", err
	}
	return baseURL + specialSubmitPath, nil
}

func (a *TaskAdaptor) BuildRequestHeader(c *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+info.ApiKey)
	req.Header.Set("Content-Type", "application/json")
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
	modelName, spec, err := resolveModelSpec(c, info, payload)
	if err != nil {
		return nil, err
	}
	setResolvedModel(info, modelName)

	normalized, err := buildProviderPayload(spec, payload, modelName)
	if err != nil {
		return nil, err
	}
	body, err := common.Marshal(normalized)
	if err != nil {
		return nil, fmt.Errorf("marshal GlobalAiOpc request: %w", err)
	}
	return bytes.NewReader(body), nil
}

func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	resp, err := channel.DoTaskApiRequest(a, c, info, requestBody)
	if err == nil && resp != nil && resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
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
	if strings.EqualFold(stringField(root, "status"), "failed") {
		message := responseFailureMessage(root)
		if message == "" {
			message = "GlobalAiOpc task submission failed"
		}
		statusCode := resp.StatusCode
		if statusCode >= http.StatusOK && statusCode < http.StatusMultipleChoices {
			statusCode = http.StatusBadGateway
		}
		return "", nil, service.TaskErrorWrapper(fmt.Errorf("%s", message), "task_submit_failed", statusCode)
	}
	taskID := stringField(root, "id")
	if taskID == "" {
		message := responseFailureMessage(root)
		if message == "" {
			message = "GlobalAiOpc returned an empty task id"
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
	req, err := http.NewRequest(http.MethodGet, baseURL+resultPathPrefix+url.PathEscape(taskID), nil)
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

	status := strings.ToLower(stringField(root, "status"))
	result := &relaycommon.TaskInfo{
		Code:   0,
		TaskID: stringField(root, "id"),
	}
	if progress := progressString(root["progress"]); progress != "" {
		result.Progress = progress
	}

	switch status {
	case "queued":
		result.Status = model.TaskStatusQueued
	case "processing":
		result.Status = model.TaskStatusInProgress
	case "completed":
		result.Status = model.TaskStatusSuccess
		result.Url = responseVideoURL(root)
		if result.Url == "" {
			return nil, fmt.Errorf("GlobalAiOpc task completed without a video URL")
		}
	case "failed":
		result.Status = model.TaskStatusFailure
		result.Reason = responseFailureMessage(root)
		if result.Reason == "" {
			result.Reason = "task failed"
		}
	default:
		return nil, fmt.Errorf("unknown GlobalAiOpc task status: %q", status)
	}
	return result, nil
}

func (a *TaskAdaptor) GetModelList() []string {
	return globalaiopcchannel.ModelList
}

func (a *TaskAdaptor) GetChannelName() string {
	return globalaiopcchannel.ChannelName
}

func resolveModelSpec(c *gin.Context, info *relaycommon.RelayInfo, payload map[string]any) (string, seedanceSpecialSpec, error) {
	requestModel := ""
	if payload != nil {
		requestModel = stringField(payload, "model")
	}
	requestModel = firstString(requestModel, upstreamModel(info), channelMetaUpstreamModel(info), originModel(info))
	if requestModel == "" {
		return "", seedanceSpecialSpec{}, fmt.Errorf("model is required")
	}
	finalModel, err := resolveConfiguredUpstreamModel(c, requestModel)
	if err != nil {
		return "", seedanceSpecialSpec{}, err
	}
	return finalModel, seedanceSpecialSpec{
		Name:            finalModel,
		RequireVideoRef: strings.HasSuffix(strings.ToLower(finalModel), "_with_video_ref"),
	}, nil
}

func resolveConfiguredUpstreamModel(c *gin.Context, modelName string) (string, error) {
	modelName = strings.TrimSpace(modelName)
	if modelName == "" || c == nil {
		return modelName, nil
	}
	mappingInfo := &relaycommon.RelayInfo{
		OriginModelName: modelName,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: modelName,
		},
	}
	if err := relayhelper.ModelMappedHelper(c, mappingInfo, nil); err != nil {
		return "", err
	}
	return strings.TrimSpace(mappingInfo.UpstreamModelName), nil
}

func setResolvedModel(info *relaycommon.RelayInfo, modelName string) {
	if info == nil {
		return
	}
	info.UpstreamModelName = modelName
	if info.ChannelMeta != nil {
		info.ChannelMeta.UpstreamModelName = modelName
	}
}

func validatePayload(spec seedanceSpecialSpec, payload map[string]any) (requestSummary, error) {
	if hasContentArray(payload) {
		return validateSpecialContentPayload(spec, payload)
	}

	fields := mergedRequestFields(payload)
	prompt := strings.TrimSpace(stringField(fields, "prompt"))
	if prompt == "" {
		return requestSummary{}, fmt.Errorf("prompt is required")
	}

	duration, hasDuration, err := durationValue(fields)
	if err != nil {
		return requestSummary{}, err
	}
	if err := validateDuration(spec, duration, hasDuration); err != nil {
		return requestSummary{}, err
	}

	if size := stringField(fields, "size"); size != "" {
		if _, _, err := normalizeVideoSize(size); err != nil {
			return requestSummary{}, err
		}
	}
	if err := validateResolution(spec, fields); err != nil {
		return requestSummary{}, err
	}

	return validateSpecialFlatFields(spec, fields, prompt, duration, hasDuration)
}

func validateSpecialFlatFields(spec seedanceSpecialSpec, fields map[string]any, prompt string, duration int, hasDuration bool) (requestSummary, error) {
	if err := validateFrameAliasConsistency(fields); err != nil {
		return requestSummary{}, err
	}
	images, err := firstStringSlice(fields, "referenceImages", "image_urls", "images")
	if err != nil {
		return requestSummary{}, err
	}
	videos, err := firstStringSlice(fields, "referenceVideos", "video_urls", "videos")
	if err != nil {
		return requestSummary{}, err
	}
	audios, err := firstStringSlice(fields, "referenceAudios", "audio_urls", "audios")
	if err != nil {
		return requestSummary{}, err
	}
	firstImage := firstFrameValue(fields)
	lastImage := lastFrameValue(fields)
	if lastImage != "" && firstImage == "" {
		return requestSummary{}, fmt.Errorf("last_image requires first_image")
	}
	if (firstImage != "" || lastImage != "") && (len(images) > 0 || len(videos) > 0 || len(audios) > 0) {
		return requestSummary{}, fmt.Errorf("first_frame/last_frame cannot be mixed with reference materials")
	}
	if len(images) > maxSpecialImages {
		return requestSummary{}, materialLimitError("images", len(images), maxSpecialImages)
	}
	if len(videos) > maxSpecialVideos {
		return requestSummary{}, materialLimitError("videos", len(videos), maxSpecialVideos)
	}
	if len(audios) > maxSpecialAudios {
		return requestSummary{}, materialLimitError("audios", len(audios), maxSpecialAudios)
	}
	if spec.RequireVideoRef && len(videos) == 0 {
		return requestSummary{}, fmt.Errorf("model %s requires at least one reference video", spec.Name)
	}
	if !spec.RequireVideoRef && len(videos) > 0 {
		return requestSummary{}, fmt.Errorf("model %s does not support reference videos; map to a _with_video_ref model", spec.Name)
	}
	if len(audios) > 0 && len(images) == 0 && len(videos) == 0 {
		return requestSummary{}, fmt.Errorf("reference_audio requires at least one reference_image or reference_video")
	}
	return requestSummary{
		Prompt:      prompt,
		Duration:    duration,
		HasDuration: hasDuration,
		Images:      images,
		Videos:      videos,
		Audios:      audios,
		FirstImage:  firstImage,
		LastImage:   lastImage,
	}, nil
}

func validateSpecialContentPayload(spec seedanceSpecialSpec, payload map[string]any) (requestSummary, error) {
	content, ok := payload["content"].([]any)
	if !ok || len(content) == 0 {
		return requestSummary{}, fmt.Errorf("content must be a non-empty array")
	}
	if err := validateResolution(spec, payload); err != nil {
		return requestSummary{}, err
	}
	duration, hasDuration, err := durationValue(payload)
	if err != nil {
		return requestSummary{}, err
	}
	if err := validateDuration(spec, duration, hasDuration); err != nil {
		return requestSummary{}, err
	}

	summary := requestSummary{
		Duration:    duration,
		HasDuration: hasDuration,
	}
	for _, item := range content {
		object, ok := item.(map[string]any)
		if !ok {
			return requestSummary{}, fmt.Errorf("content items must be objects")
		}
		switch strings.TrimSpace(stringField(object, "type")) {
		case "text":
			if summary.Prompt == "" {
				summary.Prompt = strings.TrimSpace(stringField(object, "text"))
			}
		case "image_url":
			role := strings.TrimSpace(stringField(object, "role"))
			urlValue := nestedURL(object, "image_url")
			if urlValue == "" {
				return requestSummary{}, fmt.Errorf("image_url.url is required")
			}
			switch role {
			case "first_frame":
				summary.FirstImage = urlValue
			case "last_frame":
				summary.LastImage = urlValue
			case "reference_image":
				summary.Images = append(summary.Images, urlValue)
			default:
				return requestSummary{}, fmt.Errorf("unsupported image_url role %q", role)
			}
		case "video_url":
			role := strings.TrimSpace(stringField(object, "role"))
			if role != "reference_video" {
				return requestSummary{}, fmt.Errorf("unsupported video_url role %q", role)
			}
			urlValue := nestedURL(object, "video_url")
			if urlValue == "" {
				return requestSummary{}, fmt.Errorf("video_url.url is required")
			}
			summary.Videos = append(summary.Videos, urlValue)
		case "audio_url":
			role := strings.TrimSpace(stringField(object, "role"))
			if role != "reference_audio" {
				return requestSummary{}, fmt.Errorf("unsupported audio_url role %q", role)
			}
			urlValue := nestedURL(object, "audio_url")
			if urlValue == "" {
				return requestSummary{}, fmt.Errorf("audio_url.url is required")
			}
			summary.Audios = append(summary.Audios, urlValue)
		default:
			return requestSummary{}, fmt.Errorf("unsupported content type %q", stringField(object, "type"))
		}
	}

	if summary.Prompt == "" {
		return requestSummary{}, fmt.Errorf("content must include a text item")
	}
	if summary.LastImage != "" && summary.FirstImage == "" {
		return requestSummary{}, fmt.Errorf("last_frame requires first_frame")
	}
	if (summary.FirstImage != "" || summary.LastImage != "") && (len(summary.Images) > 0 || len(summary.Videos) > 0 || len(summary.Audios) > 0) {
		return requestSummary{}, fmt.Errorf("first_frame/last_frame cannot be mixed with reference materials")
	}
	if len(summary.Images) > maxSpecialImages {
		return requestSummary{}, materialLimitError("images", len(summary.Images), maxSpecialImages)
	}
	if len(summary.Videos) > maxSpecialVideos {
		return requestSummary{}, materialLimitError("videos", len(summary.Videos), maxSpecialVideos)
	}
	if len(summary.Audios) > maxSpecialAudios {
		return requestSummary{}, materialLimitError("audios", len(summary.Audios), maxSpecialAudios)
	}
	if spec.RequireVideoRef && len(summary.Videos) == 0 {
		return requestSummary{}, fmt.Errorf("model %s requires at least one reference video", spec.Name)
	}
	if !spec.RequireVideoRef && len(summary.Videos) > 0 {
		return requestSummary{}, fmt.Errorf("model %s does not support reference videos; map to a _with_video_ref model", spec.Name)
	}
	if len(summary.Audios) > 0 && len(summary.Images) == 0 && len(summary.Videos) == 0 {
		return requestSummary{}, fmt.Errorf("reference_audio requires at least one reference_image or reference_video")
	}
	return summary, nil
}

func buildProviderPayload(_ seedanceSpecialSpec, payload map[string]any, modelName string) (map[string]any, error) {
	if hasContentArray(payload) && !hasLegacyEnvelope(payload) {
		normalized, err := normalizeSpecialPayload(mergedRequestFields(payload), modelName)
		if err != nil {
			return nil, err
		}
		// Preserve provider-native content while removing compatibility-only
		// fields such as prompt, image, seconds, and aspect_ratio.
		normalized["content"] = payload["content"]
		return normalized, nil
	}
	fields := mergedRequestFields(payload)
	return normalizeSpecialPayload(fields, modelName)
}

func normalizeSpecialPayload(fields map[string]any, modelName string) (map[string]any, error) {
	if err := validateFrameAliasConsistency(fields); err != nil {
		return nil, err
	}
	normalized := copyWithoutKeys(fields,
		"model", "input", "parameters", "metadata", "size", "seconds",
		"images", "image_urls", "videos", "video_urls", "audios", "audio_urls",
		"referenceImages", "referenceVideos", "referenceAudios", "content",
		"prompt", "image", "input_reference", "first_image", "last_image", "lastFrameImage", "last_frame_image",
		"aspect_ratio", "generation_type",
		"callback_url", "priority", "safety_identifier", "service_tier", "execution_expires_after",
		"frames", "camera_fixed", "watermark", "draft",
	)
	normalized["model"] = modelName

	if duration, ok, err := durationValue(fields); err != nil {
		return nil, err
	} else if ok {
		normalized["duration"] = duration
	}
	if ratio := firstString(stringField(fields, "ratio"), stringField(fields, "aspect_ratio")); ratio != "" {
		normalized["ratio"] = ratio
	}
	if size := stringField(fields, "size"); size != "" {
		_, ratio, err := normalizeVideoSize(size)
		if err != nil {
			return nil, err
		}
		if _, exists := normalized["ratio"]; !exists && ratio != "" {
			normalized["ratio"] = ratio
		}
	}

	content := make([]any, 0, 1)
	if prompt := stringField(fields, "prompt"); prompt != "" {
		content = append(content, map[string]any{
			"type": "text",
			"text": prompt,
		})
	}
	firstImage := firstFrameValue(fields)
	lastImage := lastFrameValue(fields)
	if firstImage != "" {
		content = append(content, specialContentItem("image_url", "first_frame", firstImage))
	}
	if lastImage != "" {
		content = append(content, specialContentItem("image_url", "last_frame", lastImage))
	}
	if images, err := firstStringSlice(fields, "referenceImages", "image_urls", "images"); err != nil {
		return nil, err
	} else {
		for _, value := range images {
			content = append(content, specialContentItem("image_url", "reference_image", value))
		}
	}
	if videos, err := firstStringSlice(fields, "referenceVideos", "video_urls", "videos"); err != nil {
		return nil, err
	} else {
		for _, value := range videos {
			content = append(content, specialContentItem("video_url", "reference_video", value))
		}
	}
	if audios, err := firstStringSlice(fields, "referenceAudios", "audio_urls", "audios"); err != nil {
		return nil, err
	} else {
		for _, value := range audios {
			content = append(content, specialContentItem("audio_url", "reference_audio", value))
		}
	}
	normalized["content"] = content
	return normalized, nil
}

func specialContentItem(itemType, role, value string) map[string]any {
	field := itemType
	return map[string]any{
		"type": itemType,
		"role": role,
		field: map[string]any{
			"url": value,
		},
	}
}

func classifyValidationError(err error) *dto.TaskError {
	statusCode := http.StatusBadRequest
	switch {
	case strings.Contains(err.Error(), "supports at most"):
		return service.TaskErrorWrapperLocal(err, "material_limit_exceeded", statusCode)
	case strings.Contains(err.Error(), "duration"):
		return service.TaskErrorWrapperLocal(err, "invalid_duration", statusCode)
	case strings.Contains(err.Error(), "size"):
		return service.TaskErrorWrapperLocal(err, "invalid_size", statusCode)
	default:
		return service.TaskErrorWrapperLocal(err, "invalid_request", statusCode)
	}
}

func validateDuration(spec seedanceSpecialSpec, duration int, hasDuration bool) error {
	if !hasDuration {
		return nil
	}
	if duration <= 0 {
		return fmt.Errorf("duration must be a positive integer number of seconds")
	}
	if duration < minSpecialDuration || duration > maxSpecialDuration {
		return fmt.Errorf("duration for model %s must be between %d and %d seconds", spec.Name, minSpecialDuration, maxSpecialDuration)
	}
	return nil
}

func validateResolution(spec seedanceSpecialSpec, fields map[string]any) error {
	resolution, ratio, err := requestResolutionAndRatio(fields)
	if err != nil {
		return err
	}
	expected := modelResolution(spec.Name)
	if expected != "" && resolution != "" && resolution != expected {
		return fmt.Errorf("model %s requires %s resolution", spec.Name, expected)
	}
	if ratio != "" {
		allowed := ratio == "16:9" || ratio == "4:3" || ratio == "1:1" || ratio == "3:4" || ratio == "9:16" || ratio == "21:9" || ratio == "adaptive"
		if !allowed {
			return fmt.Errorf("model %s does not support ratio %q", spec.Name, ratio)
		}
	}
	return nil
}

func requestResolutionAndRatio(fields map[string]any) (string, string, error) {
	resolution := strings.ToLower(strings.TrimSpace(stringField(fields, "resolution")))
	ratio := strings.TrimSpace(firstString(stringField(fields, "ratio"), stringField(fields, "aspect_ratio")))
	if size := stringField(fields, "size"); size != "" {
		sizeResolution, sizeRatio, err := normalizeVideoSize(size)
		if err != nil {
			return "", "", err
		}
		if resolution == "" {
			resolution = sizeResolution
		}
		if ratio == "" {
			ratio = sizeRatio
		}
	}
	return resolution, ratio, nil
}

func modelResolution(modelName string) string {
	switch {
	case strings.Contains(modelName, "720p"):
		return "720p"
	case strings.Contains(modelName, "1080p"):
		return "1080p"
	case strings.Contains(modelName, "2k"):
		return "2k"
	case strings.Contains(modelName, "4k"):
		return "4k"
	default:
		return ""
	}
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

func mergedRequestFields(payload map[string]any) map[string]any {
	merged := make(map[string]any)
	if input, ok := objectField(payload, "input"); ok {
		for key, value := range input {
			merged[key] = value
		}
	}
	if metadata, ok := objectField(payload, "metadata"); ok {
		for key, value := range metadata {
			if _, exists := merged[key]; !exists {
				merged[key] = value
			}
		}
	}
	for key, value := range payload {
		if key == "input" || key == "parameters" {
			continue
		}
		merged[key] = value
	}
	if parameters, ok := objectField(payload, "parameters"); ok {
		for key, value := range parameters {
			if _, exists := merged[key]; !exists {
				merged[key] = value
			}
		}
	}
	return merged
}

func hasContentArray(payload map[string]any) bool {
	_, ok := payload["content"].([]any)
	return ok
}

func hasLegacyEnvelope(payload map[string]any) bool {
	_, hasInput := payload["input"]
	_, hasParameters := payload["parameters"]
	return hasInput || hasParameters
}

func firstFrameValue(fields map[string]any) string {
	return firstString(
		stringField(fields, "first_image"),
		stringField(fields, "image"),
		stringField(fields, "input_reference"),
	)
}

func validateFrameAliasConsistency(fields map[string]any) error {
	aliasGroups := [][]string{
		{"first_image", "image", "input_reference"},
		{"last_image", "lastFrameImage", "last_frame_image"},
	}
	for _, aliases := range aliasGroups {
		value := ""
		fieldName := ""
		for _, alias := range aliases {
			candidate := strings.TrimSpace(stringField(fields, alias))
			if candidate == "" {
				continue
			}
			if value != "" && candidate != value {
				return fmt.Errorf("conflicting frame fields %s and %s", fieldName, alias)
			}
			value = candidate
			fieldName = alias
		}
	}
	return nil
}

func lastFrameValue(fields map[string]any) string {
	return firstString(
		stringField(fields, "last_image"),
		stringField(fields, "lastFrameImage"),
		stringField(fields, "last_frame_image"),
	)
}

func firstStringSlice(fields map[string]any, keys ...string) ([]string, error) {
	for _, key := range keys {
		value, exists := fields[key]
		if !exists || value == nil {
			continue
		}
		return stringSlice(value)
	}
	return nil, nil
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
	case int64:
		return int(value), true
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
	case "480p", "720p", "1080p", "2k", "4k":
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
	case "2560x1440":
		return "2k", "16:9", nil
	case "1440x2560":
		return "2k", "9:16", nil
	case "3840x2160":
		return "4k", "16:9", nil
	case "2160x3840":
		return "4k", "9:16", nil
	default:
		return "", "", fmt.Errorf("unsupported video size %q", size)
	}
}

func materialLimitError(kind string, got, limit int) error {
	return fmt.Errorf("GlobalAiOpc supports at most %d %s, got %d", limit, kind, got)
}

func frameImages(firstImage, lastImage string) []string {
	result := make([]string, 0, 2)
	if firstImage != "" {
		result = append(result, firstImage)
	}
	if lastImage != "" {
		result = append(result, lastImage)
	}
	return result
}

func channelMetaUpstreamModel(info *relaycommon.RelayInfo) string {
	if info == nil || info.ChannelMeta == nil {
		return ""
	}
	return strings.TrimSpace(info.ChannelMeta.UpstreamModelName)
}

func upstreamModel(info *relaycommon.RelayInfo) string {
	if info == nil {
		return ""
	}
	return strings.TrimSpace(info.UpstreamModelName)
}

func originModel(info *relaycommon.RelayInfo) string {
	if info == nil {
		return ""
	}
	return strings.TrimSpace(info.OriginModelName)
}

func decodeResponse(body []byte) (map[string]any, error) {
	root := make(map[string]any)
	if err := common.Unmarshal(body, &root); err != nil {
		return nil, fmt.Errorf("decode GlobalAiOpc response: %w", err)
	}
	return root, nil
}

func responseVideoURL(root map[string]any) string {
	return firstString(stringField(root, "video_url"), stringField(root, "url"))
}

func responseFailureMessage(root map[string]any) string {
	return firstString(
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

func nestedURL(object map[string]any, field string) string {
	nested, ok := objectField(object, field)
	if !ok {
		return ""
	}
	return stringField(nested, "url")
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

func copyWithoutKeys(object map[string]any, keys ...string) map[string]any {
	ignored := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		ignored[key] = struct{}{}
	}
	copied := make(map[string]any, len(object))
	for key, value := range object {
		if _, skip := ignored[key]; skip {
			continue
		}
		copied[key] = value
	}
	return copied
}
