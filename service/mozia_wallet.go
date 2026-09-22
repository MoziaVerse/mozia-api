package service

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

func EnforceMoziaQuotaPolicy(c *gin.Context, userId int, modelName string) *types.NewAPIError {
	if userId == 0 || modelName == "" {
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
