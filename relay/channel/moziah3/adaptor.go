package moziah3

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

const (
	channelName          = "moziah3"
	defaultUpstreamModel = "MiniMax/MiniMax-H3"
	minimumDuration      = 4
	maximumDuration      = 15
)

type TaskAdaptor struct {
	taskcommon.BaseBilling
	baseURL string
}

type clientRequest struct {
	Task                   string      `json:"task,omitempty"`
	Conditions             []condition `json:"conditions,omitempty"`
	Target                 *target     `json:"target,omitempty"`
	Duration               *float64    `json:"duration,omitempty"`
	Seconds                *float64    `json:"seconds,omitempty"`
	Seed                   *int64      `json:"seed,omitempty"`
	NumOutputsPerPrompt    *int        `json:"num_outputs_per_prompt,omitempty"`
	NumInferenceSteps      *int        `json:"num_inference_steps,omitempty"`
	FlowShift              *float64    `json:"flow_shift,omitempty"`
	AudioFlowShift         *float64    `json:"audio_flow_shift,omitempty"`
	Quality                string      `json:"quality,omitempty"`
	OutputCompression      *int        `json:"output_compression,omitempty"`
	PreserveReferenceAudio *bool       `json:"preserve_reference_audio,omitempty"`
}

type condition struct {
	Type             string   `json:"type"`
	URI              string   `json:"uri"`
	Role             string   `json:"role"`
	FrameIndex       *int     `json:"frame_index,omitempty"`
	StartTimeSeconds *float64 `json:"start_time_seconds,omitempty"`
}

type target struct {
	ShortEdge       *int     `json:"short_edge,omitempty"`
	AspectRatio     string   `json:"aspect_ratio,omitempty"`
	DurationSeconds *float64 `json:"duration_seconds,omitempty"`
}

type upstreamRequest struct {
	Model                  string      `json:"model"`
	Task                   string      `json:"task"`
	Prompt                 string      `json:"prompt"`
	Conditions             []condition `json:"conditions"`
	Target                 target      `json:"target"`
	Seconds                *float64    `json:"seconds,omitempty"`
	Seed                   *int64      `json:"seed,omitempty"`
	NumOutputsPerPrompt    *int        `json:"num_outputs_per_prompt,omitempty"`
	NumInferenceSteps      *int        `json:"num_inference_steps,omitempty"`
	FlowShift              *float64    `json:"flow_shift,omitempty"`
	AudioFlowShift         *float64    `json:"audio_flow_shift,omitempty"`
	Quality                string      `json:"quality,omitempty"`
	OutputCompression      *int        `json:"output_compression,omitempty"`
	PreserveReferenceAudio *bool       `json:"preserve_reference_audio,omitempty"`
}

type statusResponse struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	Progress *int   `json:"progress,omitempty"`
	Error    *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
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
	if _, err := normalizedRequest(c, info); err != nil {
		return service.TaskErrorWrapperLocal(err, "invalid_request", http.StatusBadRequest)
	}
	return nil
}

func (a *TaskAdaptor) EstimateBilling(c *gin.Context, info *relaycommon.RelayInfo) map[string]float64 {
	request, err := normalizedRequest(c, info)
	if err != nil {
		return nil
	}
	return map[string]float64{"duration": *request.Target.DurationSeconds}
}

func (a *TaskAdaptor) BuildRequestURL(_ *relaycommon.RelayInfo) (string, error) {
	return apiBaseURL(a.baseURL) + "/videos", nil
}

func (a *TaskAdaptor) BuildRequestHeader(_ *gin.Context, req *http.Request, info *relaycommon.RelayInfo) error {
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+info.ApiKey)
	req.Header.Set("Content-Type", "application/json")
	return nil
}

func (a *TaskAdaptor) BuildRequestBody(c *gin.Context, info *relaycommon.RelayInfo) (io.Reader, error) {
	request, err := normalizedRequest(c, info)
	if err != nil {
		return nil, err
	}
	data, err := common.Marshal(request)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(data), nil
}

func (a *TaskAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (*http.Response, error) {
	resp, err := channel.DoTaskApiRequest(a, c, info, requestBody)
	if err == nil && resp != nil && resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
		resp.StatusCode = http.StatusOK
	}
	return resp, err
}

func (a *TaskAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (string, []byte, *dto.TaskError) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, service.TaskErrorWrapper(err, "read_response_body_failed", http.StatusInternalServerError)
	}
	var submitted struct {
		ID string `json:"id"`
	}
	if err := common.Unmarshal(responseBody, &submitted); err != nil {
		return "", nil, service.TaskErrorWrapper(fmt.Errorf("decode MoziaH3 response: %w", err), "unmarshal_response_failed", http.StatusInternalServerError)
	}
	if submitted.ID == "" {
		return "", nil, service.TaskErrorWrapperLocal(fmt.Errorf("empty id from MoziaH3 upstream"), "task_submit_failed", resp.StatusCode)
	}

	video := dto.NewOpenAIVideo()
	video.ID = info.PublicTaskID
	video.TaskID = info.PublicTaskID
	video.CreatedAt = time.Now().Unix()
	video.Model = info.OriginModelName
	c.JSON(http.StatusOK, video)
	return submitted.ID, responseBody, nil
}

func (a *TaskAdaptor) FetchTask(baseURL, key string, body map[string]any, proxy string) (*http.Response, error) {
	taskID, ok := body["task_id"].(string)
	if !ok || strings.TrimSpace(taskID) == "" {
		return nil, fmt.Errorf("invalid task_id")
	}
	req, err := http.NewRequest(http.MethodGet, apiBaseURL(baseURL)+"/videos/"+url.PathEscape(taskID), nil)
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
	var response statusResponse
	if err := common.Unmarshal(respBody, &response); err != nil {
		return nil, fmt.Errorf("decode MoziaH3 task response: %w", err)
	}
	result := &relaycommon.TaskInfo{TaskID: response.ID}
	if response.Progress != nil {
		result.Progress = strconv.Itoa(*response.Progress) + "%"
	}
	switch response.Status {
	case "queued":
		result.Status = model.TaskStatusQueued
	case "completed":
		result.Status = model.TaskStatusSuccess
	case "failed", "cancelled":
		result.Status = model.TaskStatusFailure
		result.Reason = response.Status
		if response.Error != nil && response.Error.Message != "" {
			result.Reason = response.Error.Message
		}
	default:
		return nil, fmt.Errorf("unknown MoziaH3 task status: %s", response.Status)
	}
	return result, nil
}

func (a *TaskAdaptor) GetModelList() []string {
	return []string{"minimax/minimax-h3-fl2va", "minimax/minimax-h3-ref2va"}
}

func (a *TaskAdaptor) GetChannelName() string {
	return channelName
}

func normalizedRequest(c *gin.Context, info *relaycommon.RelayInfo) (*upstreamRequest, error) {
	clientReq, err := readClientRequest(c)
	if err != nil {
		return nil, err
	}
	taskReq, err := relaycommon.GetTaskRequest(c)
	if err != nil {
		return nil, err
	}
	summary, err := taskReq.ParseVideoContent()
	if err != nil {
		return nil, err
	}

	task := strings.ToLower(strings.TrimSpace(clientReq.Task))
	if task == "" {
		modelName := strings.ToLower(info.OriginModelName)
		switch {
		case strings.Contains(modelName, "fl2va"):
			task = "fl2va"
		case strings.Contains(modelName, "ref2va"):
			task = "ref2va"
		}
	}
	if task != "fl2va" && task != "ref2va" {
		return nil, fmt.Errorf("MoziaH3 task must be fl2va or ref2va")
	}

	duration := clientReq.Duration
	if clientReq.Target != nil && clientReq.Target.DurationSeconds != nil {
		duration = clientReq.Target.DurationSeconds
	}
	if duration == nil {
		duration = clientReq.Seconds
	}
	if duration == nil || *duration < minimumDuration || *duration > maximumDuration {
		return nil, fmt.Errorf("MoziaH3 duration must be between %d and %d seconds", minimumDuration, maximumDuration)
	}

	conditions, err := resolveConditions(clientReq, taskReq, summary, task)
	if err != nil {
		return nil, err
	}
	requestTarget, err := resolveTarget(clientReq.Target, taskReq, duration)
	if err != nil {
		return nil, err
	}

	modelName := defaultUpstreamModel
	if info.IsModelMapped && strings.TrimSpace(info.UpstreamModelName) != "" {
		modelName = strings.TrimSpace(info.UpstreamModelName)
	}
	numOutputs := clientReq.NumOutputsPerPrompt
	if numOutputs == nil {
		numOutputs = common.GetPointer(1)
	}
	numSteps := clientReq.NumInferenceSteps
	if numSteps == nil {
		numSteps = common.GetPointer(20)
	}
	flowShift := clientReq.FlowShift
	if flowShift == nil {
		flowShift = common.GetPointer(12.0)
	}
	audioFlowShift := clientReq.AudioFlowShift
	if audioFlowShift == nil {
		value := 2.0
		if task == "ref2va" {
			value = 3
		}
		audioFlowShift = &value
	}
	quality := clientReq.Quality
	if quality == "" {
		quality = "lossless"
	}
	compression := clientReq.OutputCompression
	if compression == nil {
		compression = common.GetPointer(100)
	}

	request := &upstreamRequest{
		Model:                  modelName,
		Task:                   task,
		Prompt:                 taskReq.Prompt,
		Conditions:             conditions,
		Target:                 requestTarget,
		Seed:                   clientReq.Seed,
		NumOutputsPerPrompt:    numOutputs,
		NumInferenceSteps:      numSteps,
		FlowShift:              flowShift,
		AudioFlowShift:         audioFlowShift,
		Quality:                quality,
		OutputCompression:      compression,
		PreserveReferenceAudio: clientReq.PreserveReferenceAudio,
	}
	if task == "fl2va" {
		request.Seconds = duration
	}
	return request, nil
}

func readClientRequest(c *gin.Context) (*clientRequest, error) {
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, err
	}
	data, err := storage.Bytes()
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := common.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	if rawMetadata := fields["metadata"]; len(rawMetadata) > 0 {
		var metadata map[string]json.RawMessage
		if err := common.Unmarshal(rawMetadata, &metadata); err != nil {
			return nil, fmt.Errorf("metadata must be an object: %w", err)
		}
		for key, value := range metadata {
			if _, exists := fields[key]; !exists {
				fields[key] = value
			}
		}
	}
	merged, err := common.Marshal(fields)
	if err != nil {
		return nil, err
	}
	var request clientRequest
	if err := common.Unmarshal(merged, &request); err != nil {
		return nil, err
	}
	return &request, nil
}

func resolveConditions(request *clientRequest, taskReq relaycommon.TaskSubmitReq, summary relaycommon.VideoContentSummary, task string) ([]condition, error) {
	if len(request.Conditions) > 0 && (len(taskReq.Content) > 0 || len(taskReq.Images) > 0) {
		return nil, fmt.Errorf("conditions cannot be mixed with content or images")
	}
	conditions := append([]condition(nil), request.Conditions...)
	if len(conditions) == 0 {
		if summary.FirstFrameURL != "" {
			conditions = append(conditions, condition{Type: "image", URI: summary.FirstFrameURL, Role: "keyframe", FrameIndex: common.GetPointer(0)})
		}
		if summary.LastFrameURL != "" {
			conditions = append(conditions, condition{Type: "image", URI: summary.LastFrameURL, Role: "keyframe", FrameIndex: common.GetPointer(-1)})
		}
		for _, image := range summary.ReferenceImages {
			conditions = append(conditions, condition{Type: "image", URI: image, Role: "reference"})
		}
		for _, video := range summary.ReferenceVideos {
			conditions = append(conditions, condition{Type: "video", URI: video, Role: "reference"})
		}
		for _, audio := range summary.ReferenceAudios {
			conditions = append(conditions, condition{Type: "audio", URI: audio, Role: "reference"})
		}
		if len(taskReq.Content) == 0 {
			for index, image := range taskReq.Images {
				item := condition{Type: "image", URI: image, Role: "reference"}
				if task == "fl2va" {
					item.Role = "keyframe"
					item.FrameIndex = common.GetPointer(0)
					if index == 1 {
						item.FrameIndex = common.GetPointer(-1)
					}
				}
				conditions = append(conditions, item)
			}
		}
	}
	if request.PreserveReferenceAudio != nil && *request.PreserveReferenceAudio {
		for index := range conditions {
			if conditions[index].Type == "video" {
				conditions[index].Type = "video_audio"
			}
		}
	}
	return conditions, nil
}

func resolveTarget(native *target, request relaycommon.TaskSubmitReq, duration *float64) (target, error) {
	result := target{}
	if native != nil {
		result = *native
	}
	result.DurationSeconds = duration

	shortEdge, ratio := targetForSize(request.Size)
	if result.ShortEdge == nil {
		if request.Resolution != nil {
			value := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(*request.Resolution)), "p")
			parsed, err := strconv.Atoi(value)
			if err != nil || parsed <= 0 {
				return target{}, fmt.Errorf("resolution must be a positive number such as 768 or 768p")
			}
			shortEdge = parsed
		}
		if shortEdge == 0 {
			shortEdge = 768
		}
		result.ShortEdge = &shortEdge
	}
	if result.AspectRatio == "" {
		if request.Ratio != nil {
			ratio = strings.TrimSpace(*request.Ratio)
		} else if request.AspectRatio != nil {
			ratio = strings.TrimSpace(*request.AspectRatio)
		}
		if ratio == "" {
			ratio = "16:9"
		}
		result.AspectRatio = ratio
	}
	return result, nil
}

func targetForSize(size string) (int, string) {
	switch strings.ToLower(strings.ReplaceAll(strings.TrimSpace(size), "*", "x")) {
	case "1344x768", "768x448":
		return 768, "16:9"
	case "768x1344", "448x768":
		return 768, "9:16"
	case "1024x768":
		return 768, "4:3"
	case "768x1024":
		return 768, "3:4"
	case "768x768":
		return 768, "1:1"
	default:
		return 0, ""
	}
}

func apiBaseURL(baseURL string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if strings.HasSuffix(baseURL, "/v1") {
		return baseURL
	}
	return baseURL + "/v1"
}
