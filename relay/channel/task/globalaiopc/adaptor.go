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
	"unicode/utf8"

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
	videosSubmitPath = "/videos/videos"
	resultPathPrefix = "/result/"
)

type videosModelSpec struct {
	Name               string
	MaxPromptChars     int
	MaxImages          int
	MaxVideos          int
	MaxAudios          int
	MinDuration        int
	MaxDuration        int
	ExactDurations     []int
	AllowedRatios      []string
	AudioRequiresImage bool
	SupportsAutoFace   bool
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

// TaskAdaptor implements GlobalAiOpc's Videos asynchronous video protocol.
//
//	POST /v1/videos/videos
//	GET  /v1/result/{task_id}
//
// Public /v1/videos and /v1/video/generations requests are normalized into the
// official GlobalAiOpc Videos schema. Legacy content[] requests remain accepted
// as compatibility input, but the upstream request always uses root prompt,
// referenceImages, referenceVideos, and referenceAudios fields.
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

func (a *TaskAdaptor) BuildRequestURL(_ *relaycommon.RelayInfo) (string, error) {
	baseURL, err := apiBaseURL(a.baseURL)
	if err != nil {
		return "", err
	}
	return baseURL + videosSubmitPath, nil
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

func resolveModelSpec(c *gin.Context, info *relaycommon.RelayInfo, payload map[string]any) (string, videosModelSpec, error) {
	requestModel := ""
	if payload != nil {
		requestModel = stringField(payload, "model")
	}
	requestModel = firstString(requestModel, upstreamModel(info), channelMetaUpstreamModel(info), originModel(info))
	if requestModel == "" {
		return "", videosModelSpec{}, fmt.Errorf("model is required")
	}
	finalModel, err := resolveConfiguredUpstreamModel(c, requestModel)
	if err != nil {
		return "", videosModelSpec{}, err
	}
	spec, err := videosModelSpecification(finalModel)
	if err != nil {
		return "", videosModelSpec{}, err
	}
	return finalModel, spec, nil
}

func videosModelSpecification(modelName string) (videosModelSpec, error) {
	spec := videosModelSpec{
		Name:           modelName,
		MaxPromptChars: 5000,
		MaxImages:      9,
		MaxVideos:      3,
		MaxAudios:      3,
		MinDuration:    4,
		MaxDuration:    15,
		AllowedRatios:  []string{"21:9", "16:9", "4:3", "1:1", "3:4", "9:16"},
	}
	switch strings.ToLower(strings.TrimSpace(modelName)) {
	case "videos":
		spec.MaxPromptChars = 0
		spec.SupportsAutoFace = true
	case "videos_stable":
		spec.MaxImages = 4
		spec.MaxAudios = 1
		spec.AllowedRatios = []string{"16:9", "9:16", "1:1"}
	case "videos_stable_fast":
		spec.MaxImages = 4
		spec.MaxAudios = 1
		spec.ExactDurations = []int{10, 15}
		spec.AllowedRatios = []string{"16:9", "9:16", "1:1"}
	case "videos_pro", "videos_pro_fast":
		spec.MaxVideos = 0
		spec.ExactDurations = []int{10, 15}
		spec.AllowedRatios = []string{"16:9", "9:16", "1:1"}
		spec.AudioRequiresImage = true
	default:
		return videosModelSpec{}, fmt.Errorf(
			"unsupported GlobalAiOpc Videos model %q; configure model mapping to videos, videos_stable, videos_stable_fast, videos_pro, or videos_pro_fast",
			modelName,
		)
	}
	return spec, nil
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

func validatePayload(spec videosModelSpec, payload map[string]any) (requestSummary, error) {
	fields := mergedRequestFields(payload)
	if hasContentArray(payload) {
		summary, err := parseLegacyContentPayload(fields, payload)
		if err != nil {
			return requestSummary{}, err
		}
		if err := validateVideosSummary(spec, fields, summary); err != nil {
			return requestSummary{}, err
		}
		return summary, nil
	}

	prompt := strings.TrimSpace(stringField(fields, "prompt"))
	duration, hasDuration, err := durationValue(fields)
	if err != nil {
		return requestSummary{}, err
	}
	if err := validateFrameAliasConsistency(fields); err != nil {
		return requestSummary{}, err
	}
	firstImage := firstFrameValue(fields)
	lastImage := lastFrameValue(fields)
	images, err := firstStringSlice(fields, "referenceImages", "image_urls", "images")
	if err != nil {
		return requestSummary{}, err
	}
	if len(images) == 0 && firstImage == "" && lastImage == "" {
		if image := firstString(stringField(fields, "image"), stringField(fields, "input_reference")); image != "" {
			images = []string{image}
		}
	}
	videos, err := firstStringSlice(fields, "referenceVideos", "video_urls", "videos")
	if err != nil {
		return requestSummary{}, err
	}
	audios, err := firstStringSlice(fields, "referenceAudios", "audio_urls", "audios")
	if err != nil {
		return requestSummary{}, err
	}
	summary := requestSummary{
		Prompt:      prompt,
		Duration:    duration,
		HasDuration: hasDuration,
		Images:      images,
		Videos:      videos,
		Audios:      audios,
		FirstImage:  firstImage,
		LastImage:   lastImage,
	}
	if err := validateVideosSummary(spec, fields, summary); err != nil {
		return requestSummary{}, err
	}
	return summary, nil
}

func parseLegacyContentPayload(fields, payload map[string]any) (requestSummary, error) {
	content, ok := payload["content"].([]any)
	if !ok || len(content) == 0 {
		return requestSummary{}, fmt.Errorf("content must be a non-empty array")
	}
	duration, hasDuration, err := durationValue(fields)
	if err != nil {
		return requestSummary{}, err
	}

	summary := requestSummary{
		Prompt:      strings.TrimSpace(stringField(fields, "prompt")),
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
	return summary, nil
}

func validateVideosSummary(spec videosModelSpec, fields map[string]any, summary requestSummary) error {
	if summary.Prompt == "" {
		return fmt.Errorf("prompt is required")
	}
	if spec.MaxPromptChars > 0 && utf8.RuneCountInString(summary.Prompt) > spec.MaxPromptChars {
		return fmt.Errorf("prompt for model %s must not exceed %d characters", spec.Name, spec.MaxPromptChars)
	}
	if err := validateDuration(spec, summary.Duration, summary.HasDuration); err != nil {
		return err
	}
	if err := validateResolutionAndRatio(spec, fields); err != nil {
		return err
	}
	if (summary.FirstImage == "") != (summary.LastImage == "") {
		return fmt.Errorf("first_image and last_image must be provided together")
	}
	if (summary.FirstImage != "" || summary.LastImage != "") && (len(summary.Images) > 0 || len(summary.Videos) > 0 || len(summary.Audios) > 0) {
		return fmt.Errorf("first_image/last_image cannot be mixed with reference materials")
	}
	if len(summary.Images) > spec.MaxImages {
		return materialLimitError(spec.Name, "images", len(summary.Images), spec.MaxImages)
	}
	if len(summary.Videos) > spec.MaxVideos {
		return materialLimitError(spec.Name, "videos", len(summary.Videos), spec.MaxVideos)
	}
	if len(summary.Audios) > spec.MaxAudios {
		return materialLimitError(spec.Name, "audios", len(summary.Audios), spec.MaxAudios)
	}
	if spec.AudioRequiresImage && len(summary.Audios) > 0 && len(summary.Images) == 0 {
		return fmt.Errorf("model %s requires referenceImages when referenceAudios is provided", spec.Name)
	}
	if _, exists, err := autoFaceValue(fields); err != nil {
		return err
	} else if exists && !spec.SupportsAutoFace {
		return fmt.Errorf("model %s does not support autoFace", spec.Name)
	}
	return nil
}

func buildProviderPayload(spec videosModelSpec, payload map[string]any, modelName string) (map[string]any, error) {
	fields := mergedRequestFields(payload)
	summary, err := validatePayload(spec, payload)
	if err != nil {
		return nil, err
	}

	normalized := map[string]any{
		"model":    modelName,
		"prompt":   summary.Prompt,
		"duration": summary.Duration,
	}
	resolution, ratio, err := requestResolutionAndRatio(fields)
	if err != nil {
		return nil, err
	}
	if ratio != "" {
		normalized["ratio"] = ratio
	}
	if resolution != "" {
		normalized["resolution"] = resolution
	}
	if summary.FirstImage != "" {
		normalized["first_image"] = summary.FirstImage
	}
	if summary.LastImage != "" {
		normalized["last_image"] = summary.LastImage
	}
	if len(summary.Images) > 0 {
		normalized["referenceImages"] = summary.Images
	}
	if len(summary.Videos) > 0 {
		normalized["referenceVideos"] = summary.Videos
	}
	if len(summary.Audios) > 0 {
		normalized["referenceAudios"] = summary.Audios
	}
	if autoFace, exists, err := autoFaceValue(fields); err != nil {
		return nil, err
	} else if exists {
		normalized["autoFace"] = autoFace
	}
	return normalized, nil
}

func autoFaceValue(fields map[string]any) (bool, bool, error) {
	value, exists := fields["autoFace"]
	if !exists {
		return false, false, nil
	}
	autoFace, ok := value.(bool)
	if !ok {
		return false, true, fmt.Errorf("autoFace must be a boolean")
	}
	return autoFace, true, nil
}

func classifyValidationError(err error) *dto.TaskError {
	statusCode := http.StatusBadRequest
	switch {
	case strings.Contains(err.Error(), "supports at most"):
		return service.TaskErrorWrapperLocal(err, "material_limit_exceeded", statusCode)
	case strings.Contains(err.Error(), "duration"):
		return service.TaskErrorWrapperLocal(err, "invalid_duration", statusCode)
	case strings.Contains(err.Error(), "size"),
		strings.Contains(err.Error(), "resolution"),
		strings.Contains(err.Error(), "ratio"):
		return service.TaskErrorWrapperLocal(err, "invalid_size", statusCode)
	default:
		return service.TaskErrorWrapperLocal(err, "invalid_request", statusCode)
	}
}

func validateDuration(spec videosModelSpec, duration int, hasDuration bool) error {
	if !hasDuration {
		return fmt.Errorf("duration is required for model %s", spec.Name)
	}
	if duration <= 0 {
		return fmt.Errorf("duration must be a positive integer number of seconds")
	}
	if len(spec.ExactDurations) > 0 {
		for _, allowed := range spec.ExactDurations {
			if duration == allowed {
				return nil
			}
		}
		return fmt.Errorf("duration for model %s must be one of %v seconds", spec.Name, spec.ExactDurations)
	}
	if duration < spec.MinDuration || duration > spec.MaxDuration {
		return fmt.Errorf("duration for model %s must be between %d and %d seconds", spec.Name, spec.MinDuration, spec.MaxDuration)
	}
	return nil
}

func validateResolutionAndRatio(spec videosModelSpec, fields map[string]any) error {
	resolution, ratio, err := requestResolutionAndRatio(fields)
	if err != nil {
		return err
	}
	if resolution != "" && resolution != "720p" {
		return fmt.Errorf("model %s only supports 720p resolution", spec.Name)
	}
	if ratio != "" {
		for _, allowed := range spec.AllowedRatios {
			if ratio == allowed {
				return nil
			}
		}
		return fmt.Errorf("model %s does not support ratio %q", spec.Name, ratio)
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

func firstFrameValue(fields map[string]any) string {
	if firstImage := stringField(fields, "first_image"); firstImage != "" {
		return firstImage
	}
	if lastFrameValue(fields) == "" {
		return ""
	}
	return firstString(stringField(fields, "image"), stringField(fields, "input_reference"))
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

func materialLimitError(modelName, kind string, got, limit int) error {
	return fmt.Errorf("GlobalAiOpc model %s supports at most %d %s, got %d", modelName, limit, kind, got)
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
