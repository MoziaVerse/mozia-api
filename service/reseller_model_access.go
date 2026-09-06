package service

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/types"
)

func EnforceResellerModelAccess(userId int, modelName string) *types.NewAPIError {
	policy, err := model.GetUserResellerModelAccess(userId)
	if err != nil {
		return types.NewError(errors.New("model authorization unavailable"), types.ErrorCodeQueryDataError, types.ErrOptionWithStatusCode(http.StatusServiceUnavailable), types.ErrOptionWithSkipRetry())
	}
	if !policy.Allows(modelName) {
		return types.NewError(errors.New("model not authorized by reseller"), types.ErrorCode("reseller_model_forbidden"), types.ErrOptionWithStatusCode(http.StatusForbidden), types.ErrOptionWithSkipRetry())
	}
	return nil
}
