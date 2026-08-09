package service

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
)

func quotaInt(value int64) (int, error) {
	converted := int(value)
	if int64(converted) != value {
		return 0, model.ErrResellerQuotaOverflow
	}
	return converted, nil
}

// CustomerQuotaForBase applies the retail multiplier frozen at pre-consume.
// Ordinary users retain the exact historical base quota path.
func CustomerQuotaForBase(relayInfo *relaycommon.RelayInfo, baseQuota int) (int, error) {
	if baseQuota < 0 {
		return 0, model.ErrInvalidResellerPriceRule
	}
	if relayInfo == nil || relayInfo.ResellerBilling == nil {
		return baseQuota, nil
	}
	quota, err := model.ApplyResellerMultiplier(int64(baseQuota), relayInfo.ResellerBilling.RetailMultiplierPPM)
	if err != nil {
		return 0, err
	}
	return quotaInt(quota)
}

// CaptureResellerBillingUsage stores request usage only on the internal relay
// context. It is persisted with the settlement snapshot and is never included
// in customer-facing responses.
func CaptureResellerBillingUsage(relayInfo *relaycommon.RelayInfo, usage any) {
	if relayInfo == nil || relayInfo.ResellerBilling == nil || usage == nil {
		return
	}
	encoded, err := common.Marshal(usage)
	if err == nil {
		relayInfo.ResellerBilling.UsageJSON = string(encoded)
	}
}

func prepareResellerBilling(relayInfo *relaycommon.RelayInfo, estimatedBaseQuota int) (int, bool, *types.NewAPIError) {
	if relayInfo == nil {
		return 0, false, types.NewError(fmt.Errorf("relayInfo is nil"), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}
	if relayInfo.ResellerBilling != nil {
		customerQuota, err := CustomerQuotaForBase(relayInfo, estimatedBaseQuota)
		if err != nil {
			return 0, false, types.NewError(err, types.ErrorCodeModelPriceError, types.ErrOptionWithSkipRetry())
		}
		return customerQuota, false, nil
	}

	customer, err := model.ResolveResellerBillingCustomer(relayInfo.UserId)
	if err != nil {
		return 0, false, types.NewError(err, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry())
	}
	if customer == nil {
		return estimatedBaseQuota, false, nil
	}

	now := common.GetTimestamp()
	wholesale, err := model.ResolveResellerWholesalePrice(customer.ResellerId, relayInfo.OriginModelName, now)
	if err != nil {
		return 0, false, types.NewError(err, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry())
	}
	retail, err := model.ResolveResellerRetailPrice(customer.ResellerId, customer.CustomerId, relayInfo.OriginModelName, now)
	if err != nil {
		return 0, false, types.NewError(err, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry())
	}
	if retail.MultiplierPPM < wholesale.MultiplierPPM {
		return 0, false, types.NewError(model.ErrResellerPriceMarginConflict, types.ErrorCodeModelPriceError, types.ErrOptionWithSkipRetry())
	}
	estimatedCustomerQuota, err := model.ApplyResellerMultiplier(int64(estimatedBaseQuota), retail.MultiplierPPM)
	if err != nil {
		return 0, false, types.NewError(err, types.ErrorCodeModelPriceError, types.ErrOptionWithSkipRetry())
	}
	estimatedWholesaleQuota, err := model.ApplyResellerMultiplier(int64(estimatedBaseQuota), wholesale.MultiplierPPM)
	if err != nil {
		return 0, false, types.NewError(err, types.ErrorCodeModelPriceError, types.ErrOptionWithSkipRetry())
	}

	settlement, created, err := model.PrepareResellerRequestSettlement(&model.ResellerRequestSettlement{
		RequestId: relayInfo.RequestId, ResellerId: customer.ResellerId, CustomerId: customer.CustomerId,
		UserId: customer.UserId, ModelName: relayInfo.OriginModelName,
		WholesaleRuleId: wholesale.RuleId, WholesaleRuleVersion: wholesale.RuleVersion,
		WholesaleMultiplierPPM: wholesale.MultiplierPPM,
		RetailRuleId:           retail.RuleId, RetailRuleVersion: retail.RuleVersion,
		RetailMultiplierPPM: retail.MultiplierPPM,
		EstimatedBaseQuota:  int64(estimatedBaseQuota), EstimatedCustomerQuota: estimatedCustomerQuota,
		EstimatedWholesaleQuota: estimatedWholesaleQuota,
		UsageJSON:               "",
	})
	if err != nil {
		return 0, false, types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
	}
	if !created {
		return 0, false, types.NewError(model.ErrResellerSettlementConflict, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
	}
	relayInfo.ResellerBilling = &relaycommon.ResellerBillingContext{
		SettlementRequestId: settlement.RequestId, ResellerId: settlement.ResellerId,
		CustomerId: settlement.CustomerId, RetailMultiplierPPM: settlement.RetailMultiplierPPM,
		WholesaleMultiplierPPM: settlement.WholesaleMultiplierPPM,
	}
	customerQuota, err := quotaInt(settlement.EstimatedCustomerQuota)
	if err != nil {
		return 0, created, types.NewError(err, types.ErrorCodeModelPriceError, types.ErrOptionWithSkipRetry())
	}
	return customerQuota, created, nil
}

func resellerActualQuotas(relayInfo *relaycommon.RelayInfo, actualBaseQuota int) (int, int64, error) {
	customerQuota, err := CustomerQuotaForBase(relayInfo, actualBaseQuota)
	if err != nil {
		return 0, 0, err
	}
	wholesaleQuota, err := model.ApplyResellerMultiplier(int64(actualBaseQuota), relayInfo.ResellerBilling.WholesaleMultiplierPPM)
	if err != nil {
		return 0, 0, err
	}
	return customerQuota, wholesaleQuota, nil
}

func beginResellerBillingSettlement(relayInfo *relaycommon.RelayInfo, actualBaseQuota int) (int, error) {
	if relayInfo == nil || relayInfo.ResellerBilling == nil {
		return actualBaseQuota, nil
	}
	customerQuota, wholesaleQuota, err := resellerActualQuotas(relayInfo, actualBaseQuota)
	if err != nil {
		return 0, err
	}
	usageJSON := relayInfo.ResellerBilling.UsageJSON
	if usageJSON == "" {
		encoded, marshalErr := common.Marshal(map[string]any{"base_quota": actualBaseQuota, "kind": "per_call"})
		if marshalErr != nil {
			return 0, marshalErr
		}
		usageJSON = string(encoded)
	}
	if err := model.BeginResellerSettlement(
		relayInfo.ResellerBilling.SettlementRequestId,
		int64(actualBaseQuota), int64(customerQuota), wholesaleQuota, usageJSON,
	); err != nil {
		return 0, err
	}
	return customerQuota, nil
}

func completeResellerBillingSettlement(relayInfo *relaycommon.RelayInfo) error {
	if relayInfo == nil || relayInfo.ResellerBilling == nil {
		return nil
	}
	return model.CompleteResellerSettlement(relayInfo.ResellerBilling.SettlementRequestId)
}

func failNewResellerBillingSettlement(relayInfo *relaycommon.RelayInfo, created bool) {
	if !created || relayInfo == nil || relayInfo.ResellerBilling == nil {
		return
	}
	_ = model.MarkResellerSettlementFailed(relayInfo.ResellerBilling.SettlementRequestId)
}

func refundResellerSettlement(relayInfo *relaycommon.RelayInfo) error {
	if relayInfo == nil || relayInfo.ResellerBilling == nil {
		return nil
	}
	return model.RefundResellerSettlement(relayInfo.ResellerBilling.SettlementRequestId)
}
