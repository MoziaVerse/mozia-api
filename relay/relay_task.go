package relay

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel"
	"github.com/QuantumNous/new-api/relay/channel/task/taskcommon"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

type TaskSubmitResult struct {
	UpstreamTaskID string
	TaskData       []byte
	Platform       constant.TaskPlatform
	Quota          int
	//PerCallPrice   types.PriceData
}

// ResolveOriginTask 处理基于已有任务的提交（remix / continuation）：
// 查找原始任务、从中提取模型名称、将渠道锁定到原始任务的渠道
// （通过 info.LockedChannel，重试时复用同一渠道并轮换 key），
// 以及提取 OtherRatios（时长、分辨率）。
// 该函数在控制器的重试循环之前调用一次，其结果通过 info 字段和上下文持久化。
func ResolveOriginTask(c *gin.Context, info *relaycommon.RelayInfo) *dto.TaskError {
	// 检测 remix action
	path := c.Request.URL.Path
	if strings.Contains(path, "/v1/videos/") && strings.HasSuffix(path, "/remix") {
		info.Action = constant.TaskActionRemix
	}

	// 提取 remix 任务的 video_id
	if info.Action == constant.TaskActionRemix {
		videoID := c.Param("video_id")
		if strings.TrimSpace(videoID) == "" {
			return service.TaskErrorWrapperLocal(fmt.Errorf("video_id is required"), "invalid_request", http.StatusBadRequest)
		}
		info.OriginTaskID = videoID
	}

	if info.OriginTaskID == "" {
		return nil
	}

	// 查找原始任务
	originTask, exist, err := model.GetByTaskId(info.UserId, info.OriginTaskID)
	if err != nil {
		return service.TaskErrorWrapper(err, "get_origin_task_failed", http.StatusInternalServerError)
	}
	if !exist {
		return service.TaskErrorWrapperLocal(errors.New("task_origin_not_exist"), "task_not_exist", http.StatusBadRequest)
	}

	// 从原始任务推导模型名称
	if info.OriginModelName == "" {
		if originTask.Properties.OriginModelName != "" {
			info.OriginModelName = originTask.Properties.OriginModelName
		} else if originTask.Properties.UpstreamModelName != "" {
			info.OriginModelName = originTask.Properties.UpstreamModelName
		} else {
			var taskData map[string]interface{}
			_ = common.Unmarshal(originTask.Data, &taskData)
			if m, ok := taskData["model"].(string); ok && m != "" {
				info.OriginModelName = m
			}
		}
	}

	// 锁定到原始任务的渠道（重试时复用同一渠道，轮换 key）
	ch, err := model.GetChannelById(originTask.ChannelId, true)
	if err != nil {
		return service.TaskErrorWrapperLocal(err, "channel_not_found", http.StatusBadRequest)
	}
	if ch.Status != common.ChannelStatusEnabled {
		return service.TaskErrorWrapperLocal(errors.New("the channel of the origin task is disabled"), "task_channel_disable", http.StatusBadRequest)
	}
	info.LockedChannel = ch

	if originTask.ChannelId != info.ChannelId {
		key, _, newAPIError := ch.GetNextEnabledKey()
		if newAPIError != nil {
			return service.TaskErrorWrapper(newAPIError, "channel_no_available_key", newAPIError.StatusCode)
		}
		common.SetContextKey(c, constant.ContextKeyChannelKey, key)
		common.SetContextKey(c, constant.ContextKeyChannelType, ch.Type)
		common.SetContextKey(c, constant.ContextKeyChannelBaseUrl, ch.GetBaseURL())
		common.SetContextKey(c, constant.ContextKeyChannelId, originTask.ChannelId)

		info.ChannelBaseUrl = ch.GetBaseURL()
		info.ChannelId = originTask.ChannelId
		info.ChannelType = ch.Type
		info.ApiKey = key
	}

	// 提取 remix 参数（时长、分辨率 → OtherRatios）
	if info.Action == constant.TaskActionRemix {
		if originTask.PrivateData.BillingContext != nil {
			// 新的 remix 逻辑：直接从原始任务的 BillingContext 中提取 OtherRatios（如果存在）
			for s, f := range originTask.PrivateData.BillingContext.OtherRatios {
				info.PriceData.AddOtherRatio(s, f)
			}
		} else {
			// 旧的 remix 逻辑：直接从 task data 解析 seconds 和 size（如果存在）
			var taskData map[string]interface{}
			_ = common.Unmarshal(originTask.Data, &taskData)
			secondsStr, _ := taskData["seconds"].(string)
			seconds, _ := strconv.Atoi(secondsStr)
			if seconds <= 0 {
				seconds = 4
			}
			sizeStr, _ := taskData["size"].(string)
			if info.PriceData.OtherRatios == nil {
				info.PriceData.OtherRatios = map[string]float64{}
			}
			info.PriceData.OtherRatios["seconds"] = float64(seconds)
			info.PriceData.OtherRatios["size"] = 1
			if sizeStr == "1792x1024" || sizeStr == "1024x1792" {
				info.PriceData.OtherRatios["size"] = 1.666667
			}
		}
	}

	return nil
}

// RelayTaskSubmit 完成 task 提交的全部流程（每次尝试调用一次）：
// 刷新渠道元数据 → 确定 platform/adaptor → 验证请求 →
// 估算计费(EstimateBilling) → 计算价格 → 预扣费（仅首次）→
// 构建/发送/解析上游请求 → 提交后计费调整(AdjustBillingOnSubmit)。
// 控制器负责 defer Refund 和成功后 Settle。
func RelayTaskSubmit(c *gin.Context, info *relaycommon.RelayInfo) (*TaskSubmitResult, *dto.TaskError) {
	info.InitChannelMeta(c)
	restoreRequestBody, taskErr := applyTaskParamOverride(c, info)
	if taskErr != nil {
		return nil, taskErr
	}
	if restoreRequestBody != nil {
		defer restoreRequestBody()
	}

	// 1. 确定 platform → 创建适配器 → 验证请求
	platform := constant.TaskPlatform(c.GetString("platform"))
	if platform == "" {
		platform = GetTaskPlatform(c)
	}
	adaptor := GetTaskAdaptor(platform)
	if adaptor == nil {
		return nil, service.TaskErrorWrapperLocal(fmt.Errorf("invalid api platform: %s", platform), "invalid_api_platform", http.StatusBadRequest)
	}
	adaptor.Init(info)
	if taskErr := adaptor.ValidateRequestAndSetAction(c, info); taskErr != nil {
		return nil, taskErr
	}

	// 2. 确定模型名称
	modelName := info.OriginModelName
	if modelName == "" {
		modelName = service.CoverTaskActionToModelName(platform, info.Action)
	}

	// 2.5 应用渠道的模型映射（与同步任务对齐）
	info.OriginModelName = modelName
	info.UpstreamModelName = modelName
	if err := helper.ModelMappedHelper(c, info, nil); err != nil {
		return nil, service.TaskErrorWrapperLocal(err, "model_mapping_failed", http.StatusBadRequest)
	}

	// 3. 预生成公开 task ID（仅首次）
	if info.PublicTaskID == "" {
		info.PublicTaskID = model.GenerateTaskID()
	}

	// 4. 价格计算：基础模型价格
	info.OriginModelName = modelName
	priceData, err := helper.ModelPriceHelperPerCall(c, info)
	if err != nil {
		return nil, service.TaskErrorWrapper(err, "model_price_error", http.StatusBadRequest)
	}
	info.PriceData = priceData
	if apiErr := service.EnforceMoziaQuotaPolicy(info.UserId, info.OriginModelName); apiErr != nil {
		return nil, service.TaskErrorFromAPIError(apiErr)
	}

	// 5. 计费估算：显式 task_billing 配置优先；未配置时保持每个
	//    adaptor 的既有 EstimateBilling 语义。两者均使用已经过渠道参数
	//    覆盖处理的请求体。
	taskBillingEvaluation, taskBillingConfig, hasTaskBillingConfig, err := helper.TaskBillingEvaluation(c, modelName)
	if err != nil {
		return nil, service.TaskErrorWrapperLocal(err, "invalid_task_billing", http.StatusBadRequest)
	}
	if hasTaskBillingConfig {
		if !info.PriceData.UsePrice {
			return nil, service.TaskErrorWrapperLocal(
				fmt.Errorf("task billing for model %s requires a ModelPrice", modelName),
				"invalid_task_billing",
				http.StatusBadRequest,
			)
		}
		info.PriceData.TaskBillingMode = taskBillingConfig.Mode
		info.PriceData.TaskBillingVersion = taskBillingConfig.Version
		for k, v := range taskBillingEvaluation.Ratios {
			info.PriceData.AddOtherRatio(k, v)
		}
		info.PriceData.TaskBillingSurcharge = taskBillingEvaluation.Surcharge
	} else if estimatedRatios := adaptor.EstimateBilling(c, info); len(estimatedRatios) > 0 {
		for k, v := range estimatedRatios {
			info.PriceData.AddOtherRatio(k, v)
		}
	}
	if !hasTaskBillingConfig && !info.PriceData.UsePrice && relaycommon.TaskRequestHasReferenceVideo(c) {
		if ratio, ok := ratio_setting.GetVideoInputRatio(modelName); ok {
			info.PriceData.AddOtherRatio("video_input", ratio)
		}
	}

	// 6. 将 OtherRatios 应用到基础额度
	if hasTaskBillingConfig {
		info.PriceData.Quota = configuredTaskBillingQuota(info.PriceData)
		if info.PriceData.Quota > 0 {
			info.PriceData.FreeModel = false
		}
	} else if !common.StringsContains(constant.TaskPricePatches, modelName) {
		for _, ra := range info.PriceData.OtherRatios {
			if ra != 1.0 {
				info.PriceData.Quota = int(float64(info.PriceData.Quota) * ra)
			}
		}
	}

	// 7. 预扣费（仅首次 — 重试时 info.Billing 已存在，跳过）
	if info.Billing == nil && !info.PriceData.FreeModel {
		info.ForcePreConsume = true
		if apiErr := service.PreConsumeBilling(c, info.PriceData.Quota, info); apiErr != nil {
			return nil, service.TaskErrorFromAPIError(apiErr)
		}
	}

	// 8. 构建请求体
	requestBody, err := adaptor.BuildRequestBody(c, info)
	if err != nil {
		return nil, service.TaskErrorWrapper(err, "build_request_failed", http.StatusInternalServerError)
	}

	// 9. 发送请求
	resp, err := adaptor.DoRequest(c, info, requestBody)
	if err != nil {
		return nil, service.TaskErrorWrapper(err, "do_request_failed", http.StatusInternalServerError)
	}
	if resp != nil && resp.StatusCode != http.StatusOK {
		responseBody, _ := io.ReadAll(resp.Body)
		return nil, service.TaskErrorWrapper(fmt.Errorf("%s", string(responseBody)), "fail_to_fetch_task", resp.StatusCode)
	}

	// 10. 返回 OtherRatios 给下游（header 必须在 DoResponse 写 body 之前设置）
	otherRatios := info.PriceData.OtherRatios
	if otherRatios == nil {
		otherRatios = map[string]float64{}
	}
	ratiosJSON, _ := common.Marshal(otherRatios)
	c.Header("X-New-Api-Other-Ratios", string(ratiosJSON))

	// 11. 解析响应
	upstreamTaskID, taskData, taskErr := adaptor.DoResponse(c, resp, info)
	if taskErr != nil {
		return nil, taskErr
	}

	// 11. 提交后计费调整：让适配器根据上游实际返回调整 OtherRatios
	finalQuota := info.PriceData.Quota
	if !hasTaskBillingConfig {
		if adjustedRatios := adaptor.AdjustBillingOnSubmit(info, taskData); len(adjustedRatios) > 0 {
			// 基于调整后的 ratios 重新计算 quota
			finalQuota = recalcQuotaFromRatios(info, adjustedRatios)
			info.PriceData.OtherRatios = adjustedRatios
			info.PriceData.Quota = finalQuota
		}
	}

	return &TaskSubmitResult{
		UpstreamTaskID: upstreamTaskID,
		TaskData:       taskData,
		Platform:       platform,
		Quota:          finalQuota,
	}, nil
}

// applyTaskParamOverride makes the selected channel's effective JSON body
// available to task validation, billing estimation, and request conversion.
// The controller retries with the original body, so each channel gets a clean
// input instead of inheriting a previous channel's overrides.
func applyTaskParamOverride(c *gin.Context, info *relaycommon.RelayInfo) (func(), *dto.TaskError) {
	if info == nil || len(info.ParamOverride) == 0 ||
		!strings.HasPrefix(c.GetHeader("Content-Type"), "application/json") {
		return nil, nil
	}

	originalStorage, err := common.GetBodyStorage(c)
	if err != nil {
		statusCode := http.StatusBadRequest
		if common.IsRequestBodyTooLargeError(err) || errors.Is(err, common.ErrRequestBodyTooLarge) {
			statusCode = http.StatusRequestEntityTooLarge
		}
		return nil, service.TaskErrorWrapperLocal(err, "read_request_body_failed", statusCode)
	}
	originalJSON, err := originalStorage.Bytes()
	if err != nil {
		return nil, service.TaskErrorWrapperLocal(err, "read_request_body_failed", http.StatusBadRequest)
	}

	overriddenJSON, err := relaycommon.ApplyParamOverrideWithRelayInfo(originalJSON, info)
	if err != nil {
		apiErr := newAPIErrorFromParamOverride(err)
		taskErr := service.TaskErrorFromAPIError(apiErr)
		taskErr.LocalError = types.IsSkipRetryError(apiErr)
		return nil, taskErr
	}
	if bytes.Equal(originalJSON, overriddenJSON) {
		return nil, nil
	}

	effectiveStorage, err := common.CreateBodyStorage(overriddenJSON)
	if err != nil {
		return nil, service.TaskErrorWrapperLocal(err, "create_request_body_failed", http.StatusInternalServerError)
	}
	originalContentLength := c.Request.ContentLength
	c.Set(common.KeyBodyStorage, effectiveStorage)
	c.Request.Body = io.NopCloser(effectiveStorage)
	c.Request.ContentLength = effectiveStorage.Size()

	restore := func() {
		_ = effectiveStorage.Close()
		_, _ = originalStorage.Seek(0, io.SeekStart)
		c.Set(common.KeyBodyStorage, originalStorage)
		c.Request.Body = io.NopCloser(originalStorage)
		c.Request.ContentLength = originalContentLength
	}
	return restore, nil
}

// configuredTaskBillingQuota applies additive prices before the normal group
// ratio: (ModelPrice × ratios + additive price) × QuotaPerUnit × GroupRatio.
func configuredTaskBillingQuota(priceData types.PriceData) int {
	price := priceData.ModelPrice
	for _, ratio := range priceData.OtherRatios {
		price *= ratio
	}
	if priceData.TaskBillingSurcharge != nil {
		price += priceData.TaskBillingSurcharge.Price
	}
	return int(price * common.QuotaPerUnit * priceData.GroupRatioInfo.GroupRatio)
}

// recalcQuotaFromRatios 根据 adjustedRatios 重新计算 quota。
// 公式: baseQuota × ∏(ratio) — 其中 baseQuota 是不含 OtherRatios 的基础额度。
func recalcQuotaFromRatios(info *relaycommon.RelayInfo, ratios map[string]float64) int {
	// 从 PriceData 获取不含 OtherRatios 的基础价格
	baseQuota := info.PriceData.Quota
	// 先除掉原有的 OtherRatios 恢复基础额度
	for _, ra := range info.PriceData.OtherRatios {
		if ra != 1.0 && ra > 0 {
			baseQuota = int(float64(baseQuota) / ra)
		}
	}
	// 应用新的 ratios
	result := float64(baseQuota)
	for _, ra := range ratios {
		if ra != 1.0 {
			result *= ra
		}
	}
	return int(result)
}

var fetchRespBuilders = map[int]func(c *gin.Context) (respBody []byte, taskResp *dto.TaskError){
	relayconstant.RelayModeSunoFetchByID:  sunoFetchByIDRespBodyBuilder,
	relayconstant.RelayModeSunoFetch:      sunoFetchRespBodyBuilder,
	relayconstant.RelayModeVideoFetchByID: videoFetchByIDRespBodyBuilder,
}

func RelayTaskFetch(c *gin.Context, relayMode int) (taskResp *dto.TaskError) {
	respBuilder, ok := fetchRespBuilders[relayMode]
	if !ok {
		taskResp = service.TaskErrorWrapperLocal(errors.New("invalid_relay_mode"), "invalid_relay_mode", http.StatusBadRequest)
	}

	respBody, taskErr := respBuilder(c)
	if taskErr != nil {
		return taskErr
	}
	if len(respBody) == 0 {
		respBody = []byte("{\"code\":\"success\",\"data\":null}")
	}

	c.Writer.Header().Set("Content-Type", "application/json")
	_, err := io.Copy(c.Writer, bytes.NewBuffer(respBody))
	if err != nil {
		taskResp = service.TaskErrorWrapper(err, "copy_response_body_failed", http.StatusInternalServerError)
		return
	}
	return
}

func sunoFetchRespBodyBuilder(c *gin.Context) (respBody []byte, taskResp *dto.TaskError) {
	userId := c.GetInt("id")
	var condition = struct {
		IDs    []any  `json:"ids"`
		Action string `json:"action"`
	}{}
	err := c.BindJSON(&condition)
	if err != nil {
		taskResp = service.TaskErrorWrapper(err, "invalid_request", http.StatusBadRequest)
		return
	}
	var tasks []any
	if len(condition.IDs) > 0 {
		taskModels, err := model.GetByTaskIds(userId, condition.IDs)
		if err != nil {
			taskResp = service.TaskErrorWrapper(err, "get_tasks_failed", http.StatusInternalServerError)
			return
		}
		for _, task := range taskModels {
			tasks = append(tasks, TaskModel2Dto(task))
		}
	} else {
		tasks = make([]any, 0)
	}
	respBody, err = common.Marshal(dto.TaskResponse[[]any]{
		Code: "success",
		Data: tasks,
	})
	return
}

func sunoFetchByIDRespBodyBuilder(c *gin.Context) (respBody []byte, taskResp *dto.TaskError) {
	taskId := c.Param("id")
	userId := c.GetInt("id")

	originTask, exist, err := model.GetByTaskId(userId, taskId)
	if err != nil {
		taskResp = service.TaskErrorWrapper(err, "get_task_failed", http.StatusInternalServerError)
		return
	}
	if !exist {
		taskResp = service.TaskErrorWrapperLocal(errors.New("task_not_exist"), "task_not_exist", http.StatusBadRequest)
		return
	}

	respBody, err = common.Marshal(dto.TaskResponse[any]{
		Code: "success",
		Data: TaskModel2Dto(originTask),
	})
	return
}

func videoFetchByIDRespBodyBuilder(c *gin.Context) (respBody []byte, taskResp *dto.TaskError) {
	taskId := c.Param("task_id")
	if taskId == "" {
		taskId = c.GetString("task_id")
	}
	userId := c.GetInt("id")

	originTask, exist, err := model.GetByTaskId(userId, taskId)
	if err != nil {
		taskResp = service.TaskErrorWrapper(err, "get_task_failed", http.StatusInternalServerError)
		return
	}
	if !exist {
		taskResp = service.TaskErrorWrapperLocal(errors.New("task_not_exist"), "task_not_exist", http.StatusBadRequest)
		return
	}

	isOpenAIVideoAPI := strings.HasPrefix(c.Request.RequestURI, "/v1/videos/")

	// Gemini/Vertex 支持实时查询：用户 fetch 时直接从上游拉取最新状态
	if realtimeResp := tryRealtimeFetch(originTask, isOpenAIVideoAPI); len(realtimeResp) > 0 {
		respBody = realtimeResp
		return
	}

	// OpenAI Video API 格式: 走各 adaptor 的 ConvertToOpenAIVideo
	if isOpenAIVideoAPI {
		adaptor := GetTaskAdaptor(originTask.Platform)
		if adaptor == nil {
			taskResp = service.TaskErrorWrapperLocal(fmt.Errorf("invalid channel id: %d", originTask.ChannelId), "invalid_channel_id", http.StatusBadRequest)
			return
		}
		if converter, ok := adaptor.(channel.OpenAIVideoConverter); ok {
			openAIVideoData, err := converter.ConvertToOpenAIVideo(originTask)
			if err != nil {
				taskResp = service.TaskErrorWrapper(err, "convert_to_openai_video_failed", http.StatusInternalServerError)
				return
			}
			respBody = openAIVideoData
			return
		}
		taskResp = service.TaskErrorWrapperLocal(fmt.Errorf("not_implemented:%s", originTask.Platform), "not_implemented", http.StatusNotImplemented)
		return
	}

	respBody, err = publicVideoTaskResponseBody(originTask, originTask.Data)
	if err != nil {
		taskResp = service.TaskErrorWrapper(err, "marshal_response_failed", http.StatusInternalServerError)
	}
	return
}

// tryRealtimeFetch 尝试从上游实时拉取 Gemini/Vertex 任务状态。
// 仅当渠道类型为 Gemini 或 Vertex 时触发；其他渠道或出错时返回 nil。
// 当非 OpenAI Video API 时，使用最新上游响应构建统一的公开视频任务响应。
func tryRealtimeFetch(task *model.Task, isOpenAIVideoAPI bool) []byte {
	channelModel, err := model.GetChannelById(task.ChannelId, true)
	if err != nil {
		return nil
	}
	if channelModel.Type != constant.ChannelTypeVertexAi && channelModel.Type != constant.ChannelTypeGemini {
		return nil
	}

	baseURL := constant.ChannelBaseURLs[channelModel.Type]
	if channelModel.GetBaseURL() != "" {
		baseURL = channelModel.GetBaseURL()
	}
	proxy := channelModel.GetSetting().Proxy
	adaptor := GetTaskAdaptor(constant.TaskPlatform(strconv.Itoa(channelModel.Type)))
	if adaptor == nil {
		return nil
	}

	resp, err := adaptor.FetchTask(baseURL, channelModel.Key, map[string]any{
		"task_id": task.GetUpstreamTaskID(),
		"action":  task.Action,
	}, proxy)
	if err != nil || resp == nil {
		return nil
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}

	ti, err := adaptor.ParseTaskResult(body)
	if err != nil || ti == nil {
		return nil
	}

	snap := task.Snapshot()

	applyRealtimeTaskInfo(task, ti)

	// 将上游最新结果更新到 task
	if strings.HasPrefix(ti.Url, "data:") {
		// data: URI — kept in Data, not ResultURL
	} else if ti.Url != "" {
		task.PrivateData.ResultURL = ti.Url
	} else if task.Status == model.TaskStatusSuccess {
		// No URL from adaptor — construct proxy URL using public task ID
		task.PrivateData.ResultURL = taskcommon.BuildProxyURL(task.TaskID)
	}

	if !snap.Equal(task.Snapshot()) {
		_, _ = task.UpdateWithStatus(snap.Status)
	}

	// OpenAI Video API 由调用者的 ConvertToOpenAIVideo 分支处理
	if isOpenAIVideoAPI {
		return nil
	}

	respBody, _ := publicVideoTaskResponseBody(task, body)
	return respBody
}

func applyRealtimeTaskInfo(task *model.Task, ti *relaycommon.TaskInfo) {
	if ti.Status != "" {
		task.Status = model.TaskStatus(ti.Status)
	}
	if ti.Progress != "" {
		task.Progress = ti.Progress
	}
	if ti.Reason != "" || model.TaskStatus(ti.Status) == model.TaskStatusFailure {
		task.FailReason = ti.Reason
	}
}

func normalizePublicVideoTaskStatus(status model.TaskStatus) string {
	switch strings.ToUpper(strings.TrimSpace(string(status))) {
	case string(model.TaskStatusNotStart), string(model.TaskStatusSubmitted), string(model.TaskStatusQueued):
		return "queued"
	case string(model.TaskStatusInProgress):
		return "running"
	case string(model.TaskStatusSuccess):
		return "succeeded"
	case string(model.TaskStatusFailure):
		return "failed"
	case "CANCELLED", "CANCELED":
		return "cancelled"
	case "EXPIRED":
		return "expired"
	default:
		return "unknown"
	}
}

func publicVideoTaskProgress(status string, progress string) int {
	switch status {
	case "succeeded", "failed", "cancelled", "expired":
		return 100
	}
	progress = strings.TrimSpace(strings.TrimSuffix(progress, "%"))
	if progress == "" {
		return 0
	}
	value, err := strconv.Atoi(progress)
	if err != nil {
		return 0
	}
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}

func publicVideoTaskModel(task *model.Task) string {
	if task.Properties.OriginModelName != "" {
		return task.Properties.OriginModelName
	}
	if task.PrivateData.BillingContext != nil && task.PrivateData.BillingContext.OriginModelName != "" {
		return task.PrivateData.BillingContext.OriginModelName
	}
	if task.Properties.UpstreamModelName != "" {
		return task.Properties.UpstreamModelName
	}
	return ""
}

func publicVideoTaskMetadata(data []byte, resp *dto.PublicVideoTaskResponse) {
	if len(data) == 0 {
		return
	}
	var raw map[string]any
	if err := common.Unmarshal(data, &raw); err != nil {
		return
	}

	candidates := make([]map[string]any, 0, 5)
	if taskData, ok := raw["task"].(map[string]any); ok {
		if result, ok := taskData["result"].(map[string]any); ok {
			candidates = append(candidates, result)
		}
		candidates = append(candidates, taskData)
	}
	if result, ok := raw["result"].(map[string]any); ok {
		candidates = append(candidates, result)
	}
	if output, ok := raw["output"].(map[string]any); ok {
		candidates = append(candidates, output)
	}
	candidates = append(candidates, raw)

	for _, candidate := range candidates {
		if resp.Resolution == "" {
			resp.Resolution = publicVideoTaskString(candidate["resolution"])
		}
		if resp.Ratio == "" {
			resp.Ratio = publicVideoTaskString(candidate["ratio"])
			if resp.Ratio == "" {
				resp.Ratio = publicVideoTaskString(candidate["aspect_ratio"])
			}
		}
		if resp.Duration == nil {
			if duration, ok := publicVideoTaskDuration(candidate["duration"]); ok {
				resp.Duration = &duration
			} else if duration, ok := publicVideoTaskDuration(candidate["seconds"]); ok {
				resp.Duration = &duration
			}
		}
	}
}

func publicVideoTaskString(value any) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func publicVideoTaskDuration(value any) (float64, bool) {
	switch value := value.(type) {
	case float64:
		return value, value > 0
	case string:
		duration, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		return duration, err == nil && duration > 0
	default:
		return 0, false
	}
}

func publicVideoTaskResponse(task *model.Task, metadataData []byte) *dto.PublicVideoTaskResponse {
	status := normalizePublicVideoTaskStatus(task.Status)
	resp := &dto.PublicVideoTaskResponse{
		ID:        task.TaskID,
		TaskID:    task.TaskID,
		Object:    "video",
		Model:     publicVideoTaskModel(task),
		Status:    status,
		Progress:  publicVideoTaskProgress(status, task.Progress),
		CreatedAt: task.CreatedAt,
		UpdatedAt: task.UpdatedAt,
	}
	if status == "succeeded" && task.GetResultURL() != "" {
		now := time.Now()
		resp.Content = &dto.PublicVideoTaskContent{
			URL:       taskcommon.BuildSignedVideoGenerationURLAt(now, task.UserId, task.TaskID, taskcommon.TaskVideoFilename(task)),
			ExpiresAt: now.Add(taskcommon.SignedVideoURLTTL).Unix(),
		}
	}
	if status == "failed" {
		resp.Error = &dto.PublicVideoTaskFailure{
			Code:    "task_failed",
			Message: task.FailReason,
		}
	}
	publicVideoTaskMetadata(metadataData, resp)
	return resp
}

func publicVideoTaskResponseBody(task *model.Task, metadataData []byte) ([]byte, error) {
	return common.MarshalNoEscapeHTML(publicVideoTaskResponse(task, metadataData))
}

func TaskModel2Dto(task *model.Task) *dto.TaskDto {
	return &dto.TaskDto{
		ID:         task.ID,
		CreatedAt:  task.CreatedAt,
		UpdatedAt:  task.UpdatedAt,
		TaskID:     task.TaskID,
		Platform:   string(task.Platform),
		UserId:     task.UserId,
		Group:      task.Group,
		ChannelId:  task.ChannelId,
		Quota:      task.Quota,
		Action:     task.Action,
		Status:     string(task.Status),
		FailReason: task.FailReason,
		ResultURL:  task.GetResultURL(),
		SubmitTime: task.SubmitTime,
		StartTime:  task.StartTime,
		FinishTime: task.FinishTime,
		Progress:   task.Progress,
		Properties: task.Properties,
		Username:   task.Username,
		Data:       task.Data,
	}
}
