package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
)

// LogTaskConsumption 记录任务消费日志和统计信息（仅记录，不涉及实际扣费）。
// 实际扣费已由 BillingSession（PreConsumeBilling + SettleBilling）完成。
func LogTaskConsumption(c *gin.Context, info *relaycommon.RelayInfo) {
	tokenName := c.GetString("token_name")
	logContent := fmt.Sprintf("操作 %s", info.Action)
	// 支持任务仅按次计费
	if common.StringsContains(constant.TaskPricePatches, info.OriginModelName) {
		logContent = fmt.Sprintf("%s，按次计费", logContent)
	} else {
		if len(info.PriceData.OtherRatios) > 0 {
			var contents []string
			for key, ra := range info.PriceData.OtherRatios {
				if 1.0 != ra {
					contents = append(contents, fmt.Sprintf("%s: %.2f", key, ra))
				}
			}
			if len(contents) > 0 {
				logContent = fmt.Sprintf("%s, 计算参数：%s", logContent, strings.Join(contents, ", "))
			}
		}
	}
	other := make(map[string]interface{})
	other["is_task"] = true
	other["request_path"] = c.Request.URL.Path
	other["model_price"] = info.PriceData.ModelPrice
	if info.PriceData.ModelRatio > 0 {
		other["model_ratio"] = info.PriceData.ModelRatio
	}
	other["group_ratio"] = info.PriceData.GroupRatioInfo.GroupRatio
	if info.PriceData.GroupRatioInfo.HasSpecialRatio {
		other["user_group_ratio"] = info.PriceData.GroupRatioInfo.GroupSpecialRatio
	}
	if info.PriceData.GroupRatioInfo.HasUserModelRatio {
		other["base_group_ratio"] = info.PriceData.GroupRatioInfo.BaseGroupRatio
		other["user_model_ratio"] = info.PriceData.GroupRatioInfo.UserModelRatio
	}
	if info.IsModelMapped {
		other["is_model_mapped"] = true
		other["upstream_model_name"] = info.UpstreamModelName
	}
	customerQuota, customerQuotaErr := CustomerQuotaForBase(info, info.PriceData.Quota)
	if customerQuotaErr != nil {
		logger.LogError(c, "error calculating reseller customer quota: "+customerQuotaErr.Error())
		customerQuota = info.PriceData.Quota
	}
	model.RecordConsumeLog(c, info.UserId, model.RecordConsumeLogParams{
		ChannelId: info.ChannelId,
		ModelName: info.OriginModelName,
		TokenName: tokenName,
		Quota:     customerQuota,
		Content:   logContent,
		TokenId:   info.TokenId,
		Group:     info.UsingGroup,
		Other:     other,
	})
	model.UpdateUserUsedQuotaAndRequestCount(info.UserId, customerQuota)
	model.UpdateChannelUsedQuota(info.ChannelId, info.PriceData.Quota)
}

// ---------------------------------------------------------------------------
// 异步任务计费辅助函数
// ---------------------------------------------------------------------------

// resolveTokenKey 通过 TokenId 运行时获取令牌 Key（用于 Redis 缓存操作）。
// 如果令牌已被删除或查询失败，返回空字符串。
func resolveTokenKey(ctx context.Context, tokenId int, taskID string) string {
	token, err := model.GetTokenById(tokenId)
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("获取令牌 key 失败 (tokenId=%d, task=%s): %s", tokenId, taskID, err.Error()))
		return ""
	}
	return token.Key
}

// taskIsSubscription 判断任务是否通过订阅计费。
func taskIsSubscription(task *model.Task) bool {
	return task.PrivateData.BillingSource == BillingSourceSubscription && task.PrivateData.SubscriptionId > 0
}

// taskAdjustFunding 调整任务的资金来源（钱包或订阅），delta > 0 表示扣费，delta < 0 表示退还。
func taskAdjustFunding(task *model.Task, delta int) error {
	if taskIsSubscription(task) {
		return model.PostConsumeUserSubscriptionDelta(task.PrivateData.SubscriptionId, int64(delta))
	}
	if task.PrivateData.WalletReservationRequestId != "" {
		actualQuota := task.Quota + delta
		if actualQuota <= 0 {
			return model.RefundMoziaWalletReservation(task.PrivateData.WalletReservationRequestId, task.UserId)
		}
		return model.SettleMoziaWalletReservation(task.PrivateData.WalletReservationRequestId, task.UserId, taskModelName(task), actualQuota)
	}
	if delta > 0 {
		return model.DecreaseUserQuota(task.UserId, delta, false)
	}
	return model.IncreaseUserQuota(task.UserId, -delta, false)
}

// taskAdjustTokenQuota 调整任务的令牌额度，delta > 0 表示扣费，delta < 0 表示退还。
// 需要通过 resolveTokenKey 运行时获取 key（不从 PrivateData 中读取）。
func taskAdjustTokenQuota(ctx context.Context, task *model.Task, delta int) {
	if task.PrivateData.TokenId <= 0 || delta == 0 {
		return
	}
	tokenKey := resolveTokenKey(ctx, task.PrivateData.TokenId, task.TaskID)
	if tokenKey == "" {
		return
	}
	var err error
	if delta > 0 {
		err = model.DecreaseTokenQuota(task.PrivateData.TokenId, tokenKey, delta)
	} else {
		err = model.IncreaseTokenQuota(task.PrivateData.TokenId, tokenKey, -delta)
	}
	if err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("调整令牌额度失败 (delta=%d, task=%s): %s", delta, task.TaskID, err.Error()))
	}
}

// taskBillingOther 从 task 的 BillingContext 构建日志 Other 字段。
func taskBillingOther(task *model.Task) map[string]interface{} {
	other := make(map[string]interface{})
	if bc := task.PrivateData.BillingContext; bc != nil {
		other["model_price"] = bc.ModelPrice
		if bc.ModelRatio > 0 {
			other["model_ratio"] = bc.ModelRatio
		}
		other["group_ratio"] = bc.GroupRatio
		if bc.HasUserModelRatio {
			other["base_group_ratio"] = bc.BaseGroupRatio
			other["user_model_ratio"] = bc.UserModelRatio
		}
		if len(bc.OtherRatios) > 0 {
			for k, v := range bc.OtherRatios {
				other[k] = v
			}
		}
	}
	props := task.Properties
	if props.UpstreamModelName != "" && props.UpstreamModelName != props.OriginModelName {
		other["is_model_mapped"] = true
		other["upstream_model_name"] = props.UpstreamModelName
	}
	return other
}

// taskModelName 从 BillingContext 或 Properties 中获取模型名称。
func taskModelName(task *model.Task) string {
	if bc := task.PrivateData.BillingContext; bc != nil && bc.OriginModelName != "" {
		return bc.OriginModelName
	}
	if task.Properties.OriginModelName == "" && len(task.Data) > 0 {
		var data struct {
			Model string `json:"model"`
		}
		if common.Unmarshal(task.Data, &data) == nil && data.Model != "" {
			return data.Model
		}
	}
	return task.Properties.OriginModelName
}

func taskResellerSettlement(task *model.Task) (*model.ResellerRequestSettlement, bool, error) {
	if task == nil || task.PrivateData.ResellerSettlementRequestId == "" {
		return nil, false, nil
	}
	settlement, err := model.GetResellerRequestSettlement(task.PrivateData.ResellerSettlementRequestId)
	if err != nil {
		return nil, true, err
	}
	return settlement, true, nil
}

// RefundTaskQuota 统一的任务失败退款逻辑。
// 当异步任务失败时，将预扣的 quota 退还给用户（支持钱包和订阅），并退还令牌额度。
func RefundTaskQuota(ctx context.Context, task *model.Task, reason string) {
	if task != nil && task.PrivateData.BillingSettlementPending {
		logger.LogWarn(ctx, fmt.Sprintf("任务 %s 的初始 reseller settlement 尚未完成，跳过自动退款并保留恢复状态", task.TaskID))
		return
	}
	quota := task.Quota
	settlement, resellerBilled, settlementErr := taskResellerSettlement(task)
	if settlementErr != nil {
		logger.LogWarn(ctx, fmt.Sprintf("读取 reseller settlement 失败 task %s: %s", task.TaskID, settlementErr.Error()))
		return
	}
	if resellerBilled {
		if settlement.Status == model.ResellerSettlementStatusRefunded {
			return
		}
		projected, err := quotaInt(settlement.ActualCustomerQuota)
		if err != nil {
			logger.LogWarn(ctx, fmt.Sprintf("reseller 退款额度溢出 task %s: %s", task.TaskID, err.Error()))
			return
		}
		quota = projected
		if err := model.RefundResellerSettlement(settlement.RequestId); err != nil {
			logger.LogWarn(ctx, fmt.Sprintf("锁定 reseller 退款状态失败 task %s: %s", task.TaskID, err.Error()))
			return
		}
	}
	if quota == 0 {
		return
	}

	// 1. 退还资金来源（钱包或订阅）
	billingTask := *task
	billingTask.Quota = quota
	if err := taskAdjustFunding(&billingTask, -quota); err != nil {
		if resellerBilled {
			_ = model.RestoreResellerSettlementAfterRefundFailure(settlement.RequestId)
		}
		logger.LogWarn(ctx, fmt.Sprintf("退还资金来源失败 task %s: %s", task.TaskID, err.Error()))
		return
	}

	// 2. 退还令牌额度
	taskAdjustTokenQuota(ctx, task, -quota)

	// 3. 记录日志
	other := taskBillingOther(task)
	other["task_id"] = task.TaskID
	other["reason"] = reason
	model.RecordTaskBillingLog(model.RecordTaskBillingLogParams{
		UserId:    task.UserId,
		LogType:   model.LogTypeRefund,
		Content:   "",
		ChannelId: task.ChannelId,
		ModelName: taskModelName(task),
		Quota:     quota,
		TokenId:   task.PrivateData.TokenId,
		Group:     task.Group,
		Other:     other,
	})
}

// RecalculateTaskQuota 通用的异步差额结算。
// actualQuota 是任务完成后的实际应扣额度，与预扣额度 (task.Quota) 做差额结算。
// reason 用于日志记录（例如 "token重算" 或 "adaptor调整"）。
func RecalculateTaskQuota(ctx context.Context, task *model.Task, actualQuota int, reason string) {
	recalculateTaskQuota(ctx, task, actualQuota, reason, "")
}

func recalculateTaskQuota(ctx context.Context, task *model.Task, actualQuota int, reason string, usageJSON string) {
	if task != nil && task.PrivateData.BillingSettlementPending {
		logger.LogWarn(ctx, fmt.Sprintf("任务 %s 的初始 reseller settlement 尚未完成，跳过差额结算并保留恢复状态", task.TaskID))
		return
	}
	if actualQuota <= 0 {
		return
	}
	preConsumedQuota := task.Quota
	quotaDelta := actualQuota - preConsumedQuota
	billingQuotaDelta := quotaDelta
	billingPreConsumedQuota := preConsumedQuota
	billingActualQuota := actualQuota
	billingTask := task
	settlement, resellerBilled, settlementErr := taskResellerSettlement(task)
	if settlementErr != nil {
		logger.LogError(ctx, fmt.Sprintf("读取 reseller settlement 失败 task %s: %s", task.TaskID, settlementErr.Error()))
		return
	}
	var resellerWholesaleActual int64
	if resellerBilled {
		if settlement.Status == model.ResellerSettlementStatusRefunded {
			return
		}
		customerActual, err := model.ApplyResellerMultiplier(int64(actualQuota), settlement.RetailMultiplierPPM)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("计算 reseller 客户实际额度失败 task %s: %s", task.TaskID, err.Error()))
			return
		}
		resellerWholesaleActual, err = model.ApplyResellerMultiplier(int64(actualQuota), settlement.WholesaleMultiplierPPM)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("计算 reseller 批发实际额度失败 task %s: %s", task.TaskID, err.Error()))
			return
		}
		billingPreConsumedQuota, err = quotaInt(settlement.ActualCustomerQuota)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("读取 reseller 客户额度失败 task %s: %s", task.TaskID, err.Error()))
			return
		}
		billingActualQuota, err = quotaInt(customerActual)
		if err != nil {
			logger.LogError(ctx, fmt.Sprintf("reseller 客户实际额度溢出 task %s: %s", task.TaskID, err.Error()))
			return
		}
		billingQuotaDelta = billingActualQuota - billingPreConsumedQuota
		copyTask := *task
		copyTask.Quota = billingPreConsumedQuota
		billingTask = &copyTask
	}

	if billingQuotaDelta == 0 {
		if resellerBilled && (settlement.ActualBaseQuota != int64(actualQuota) || settlement.ActualWholesaleQuota != resellerWholesaleActual) {
			if err := model.UpdateSettledResellerSettlementActual(settlement.RequestId, int64(actualQuota), int64(billingActualQuota), resellerWholesaleActual, usageJSON); err != nil {
				logger.LogError(ctx, fmt.Sprintf("更新 reseller task settlement 失败 task %s: %s", task.TaskID, err.Error()))
			}
		}
		logger.LogInfo(ctx, fmt.Sprintf("任务 %s 预扣费准确（%s，%s）",
			task.TaskID, logger.LogQuota(billingActualQuota), reason))
		task.Quota = actualQuota
		return
	}

	logger.LogInfo(ctx, fmt.Sprintf("任务 %s 差额结算：delta=%s（实际：%s，预扣：%s，%s）",
		task.TaskID,
		logger.LogQuota(billingQuotaDelta),
		logger.LogQuota(billingActualQuota),
		logger.LogQuota(billingPreConsumedQuota),
		reason,
	))

	// 调整资金来源
	if err := taskAdjustFunding(billingTask, billingQuotaDelta); err != nil {
		logger.LogError(ctx, fmt.Sprintf("差额结算资金调整失败 task %s: %s", task.TaskID, err.Error()))
		return
	}

	// 调整令牌额度
	taskAdjustTokenQuota(ctx, task, billingQuotaDelta)
	if resellerBilled {
		if err := model.UpdateSettledResellerSettlementActual(settlement.RequestId, int64(actualQuota), int64(billingActualQuota), resellerWholesaleActual, usageJSON); err != nil {
			rollbackTask := *billingTask
			rollbackTask.Quota = billingActualQuota
			if rollbackErr := taskAdjustFunding(&rollbackTask, -billingQuotaDelta); rollbackErr != nil {
				logger.LogError(ctx, fmt.Sprintf("回滚 reseller task 资金失败 task %s: %s", task.TaskID, rollbackErr.Error()))
			}
			taskAdjustTokenQuota(ctx, task, -billingQuotaDelta)
			logger.LogError(ctx, fmt.Sprintf("更新 reseller task settlement 失败 task %s: %s", task.TaskID, err.Error()))
			return
		}
	}

	task.Quota = actualQuota

	var logType int
	var logQuota int
	if billingQuotaDelta > 0 {
		logType = model.LogTypeConsume
		logQuota = billingQuotaDelta
		model.UpdateUserUsedQuotaAndRequestCount(task.UserId, billingQuotaDelta)
		model.UpdateChannelUsedQuota(task.ChannelId, quotaDelta)
	} else {
		logType = model.LogTypeRefund
		logQuota = -billingQuotaDelta
	}
	other := taskBillingOther(task)
	other["task_id"] = task.TaskID
	other["pre_consumed_quota"] = billingPreConsumedQuota
	other["actual_quota"] = billingActualQuota
	model.RecordTaskBillingLog(model.RecordTaskBillingLogParams{
		UserId:    task.UserId,
		LogType:   logType,
		Content:   reason,
		ChannelId: task.ChannelId,
		ModelName: taskModelName(task),
		Quota:     logQuota,
		TokenId:   task.PrivateData.TokenId,
		Group:     task.Group,
		Other:     other,
		NodeName:  task.PrivateData.NodeName,
	})
}

// RecalculateTaskQuotaByTokens 根据实际 token 消耗重新计费（异步差额结算）。
// 当任务成功且返回了 totalTokens 时，根据模型倍率和分组倍率重新计算实际扣费额度，
// 与预扣费的差额进行补扣或退还。支持钱包和订阅计费来源。
func RecalculateTaskQuotaByTokens(ctx context.Context, task *model.Task, totalTokens int) {
	if totalTokens <= 0 {
		return
	}

	modelName := taskModelName(task)

	var modelRatio float64
	if bc := task.PrivateData.BillingContext; bc != nil {
		modelRatio = bc.ModelRatio
	} else {
		var hasRatioSetting bool
		modelRatio, hasRatioSetting, _ = ratio_setting.GetModelRatio(modelName)
		if !hasRatioSetting {
			return
		}
	}
	if modelRatio <= 0 {
		return
	}

	// 获取用户和组的倍率信息
	group := task.Group
	if group == "" {
		user, err := model.GetUserById(task.UserId, false)
		if err == nil {
			group = user.Group
		}
	}
	if group == "" {
		return
	}

	var finalGroupRatio float64
	if bc := task.PrivateData.BillingContext; bc != nil {
		finalGroupRatio = bc.GroupRatio
	} else {
		groupRatio := ratio_setting.GetGroupRatio(group)
		userGroupRatio, hasUserGroupRatio := ratio_setting.GetGroupGroupRatio(group, group)
		if hasUserGroupRatio {
			finalGroupRatio = userGroupRatio
		} else {
			finalGroupRatio = groupRatio
		}
	}

	// 计算 OtherRatios 乘积（视频折扣、时长等）
	otherMultiplier := 1.0
	if bc := task.PrivateData.BillingContext; bc != nil {
		for _, r := range bc.OtherRatios {
			if r != 1.0 && r > 0 {
				otherMultiplier *= r
			}
		}
	}

	// 计算实际应扣费额度: totalTokens * modelRatio * groupRatio * otherMultiplier
	actualQuota := int(float64(totalTokens) * modelRatio * finalGroupRatio * otherMultiplier)

	reason := fmt.Sprintf("token重算：tokens=%d, modelRatio=%.2f, groupRatio=%.2f, otherMultiplier=%.4f", totalTokens, modelRatio, finalGroupRatio, otherMultiplier)
	usageJSON := ""
	if encoded, err := common.Marshal(map[string]any{
		"base_quota":   actualQuota,
		"kind":         "task_tokens",
		"total_tokens": totalTokens,
	}); err == nil {
		usageJSON = string(encoded)
	} else {
		logger.LogError(ctx, fmt.Sprintf("序列化 reseller task usage 失败 task %s: %s", task.TaskID, err.Error()))
	}
	recalculateTaskQuota(ctx, task, actualQuota, reason, usageJSON)
}
