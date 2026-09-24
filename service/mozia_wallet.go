package service

import (
	"errors"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"net/http"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

func EnforceMoziaQuotaPolicy(c *gin.Context, userId int, modelName string) *types.NewAPIError {
	if userId == 0 || modelName == "" {
		return nil
	}
	// 模型配额策略约束的是「钱包里哪种来源的额度能用这个模型」。自带资金的 key 不走钱包，
	// 钱包为 0 是它的常态；它能用哪些模型由 key 自身的 model_limits 决定，这里直接放行。
	if common.GetContextKeyBool(c, constant.ContextKeyTokenSelfFunded) {
		return nil
	}
	err := model.CheckMoziaQuotaPolicyAccess(userId, modelName)
	if err == nil {
		return nil
	}
	statusCode := http.StatusInternalServerError
	errorCode := types.ErrorCodeQueryDataError
	if errors.Is(err, model.ErrMoziaWalletSourceForbidden) || errors.Is(err, model.ErrMoziaWalletInsufficient) {
		statusCode = http.StatusForbidden
		errorCode = types.ErrorCodeInsufficientUserQuota
	}
	apiErr := types.NewErrorWithStatusCode(
		fmt.Errorf("当前额度类型不支持调用模型 %s: %w", modelName, err),
		errorCode,
		statusCode,
		types.ErrOptionWithSkipRetry(),
	)
	RecordQuotaErrorLog(c, userId, modelName, apiErr)
	return apiErr
}
