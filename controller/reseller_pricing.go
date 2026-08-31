package controller

import (
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

type resellerPriceRuleRequest struct {
	Model            string          `json:"model"`
	Multiplier       *string         `json:"multiplier"`
	OfficialDiscount *string         `json:"official_discount"`
	CustomerId       *int            `json:"customer_id"`
	ExpectedVersion  *int            `json:"expected_version"`
	ResellerId       json.RawMessage `json:"reseller_id"`
}

type resellerPricingPreviewRequest struct {
	Model            string          `json:"model"`
	BaseQuota        json.RawMessage `json:"base_quota"`
	Multiplier       *string         `json:"multiplier"`
	OfficialDiscount *string         `json:"official_discount"`
	CustomerId       *int            `json:"customer_id"`
	ResellerId       json.RawMessage `json:"reseller_id"`
}

type resellerPlatformPreviewResponse struct {
	Model            string                         `json:"model"`
	BaseQuota        string                         `json:"base_quota"`
	Multiplier       string                         `json:"multiplier"`
	EffectiveQuota   string                         `json:"effective_quota"`
	Source           string                         `json:"source"`
	RuleId           int64                          `json:"rule_id,omitempty"`
	RuleVersion      int                            `json:"rule_version,omitempty"`
	OfficialDiscount *model.PricingOfficialDiscount `json:"official_discount,omitempty"`
}

type resellerManagementPreviewResponse struct {
	Model                     string                         `json:"model"`
	BaseQuota                 string                         `json:"base_quota"`
	CustomerId                *int                           `json:"customer_id"`
	RetailMultiplier          string                         `json:"retail_multiplier"`
	RetailQuota               string                         `json:"retail_quota"`
	RetailSource              string                         `json:"retail_source"`
	RetailRuleId              int64                          `json:"retail_rule_id,omitempty"`
	RetailRuleVersion         int                            `json:"retail_rule_version,omitempty"`
	WholesaleMultiplier       string                         `json:"wholesale_multiplier"`
	WholesaleQuota            string                         `json:"wholesale_quota"`
	WholesaleSource           string                         `json:"wholesale_source"`
	WholesaleRuleId           int64                          `json:"wholesale_rule_id,omitempty"`
	WholesaleRuleVersion      int                            `json:"wholesale_rule_version,omitempty"`
	RetailOfficialDiscount    *model.PricingOfficialDiscount `json:"retail_official_discount,omitempty"`
	WholesaleOfficialDiscount *model.PricingOfficialDiscount `json:"wholesale_official_discount,omitempty"`
}

func GetResellerPlatformPricing(c *gin.Context) {
	resellerId, ok := positivePathID(c)
	if !ok {
		return
	}
	if _, err := model.GetResellerAdminRecord(resellerId); err != nil {
		handleResellerPricingError(c, err)
		return
	}
	records, err := model.ListResellerPriceRules(resellerId, nil)
	if err != nil {
		handleResellerPricingError(c, err)
		return
	}
	wholesale := make([]model.ResellerPriceRuleRecord, 0, len(records))
	for _, record := range records {
		if record.Kind == model.ResellerPriceRuleKindWholesale {
			wholesale = append(wholesale, record)
		}
	}
	models := resellerPricingModels(wholesale)
	writeResellerAdminSuccess(c, http.StatusOK, gin.H{
		"models":             models,
		"official_discounts": resellerPricingOfficialDiscounts(models),
		"rules":              wholesale,
	})
}

func CreateResellerPlatformWholesalePrice(c *gin.Context) {
	resellerId, ok := positivePathID(c)
	if !ok {
		return
	}
	request, ok := resellerPriceRuleBody(c, false)
	if !ok {
		return
	}
	rule, err := createResellerPriceRule(resellerId, model.ResellerPriceRuleKindWholesale, request, "platform", nil)
	if err != nil {
		handleResellerPricingError(c, err)
		return
	}
	writeResellerAdminSuccess(c, http.StatusCreated, gin.H{"rule": rule.Record()})
}

func PreviewResellerPlatformPricing(c *gin.Context) {
	resellerId, ok := positivePathID(c)
	if !ok {
		return
	}
	if _, err := model.GetResellerAdminRecord(resellerId); err != nil {
		handleResellerPricingError(c, err)
		return
	}
	request, baseQuota, ok := resellerPricingPreviewBody(c, false)
	if !ok {
		return
	}
	effective, err := model.ResolveResellerWholesalePrice(resellerId, strings.TrimSpace(request.Model), common.GetTimestamp())
	if err != nil {
		handleResellerPricingError(c, err)
		return
	}
	if multiplier, override, parseErr := resellerPriceOverride(request.Model, request.Multiplier, request.OfficialDiscount); parseErr != nil {
		handleResellerPricingError(c, parseErr)
		return
	} else if override {
		effective.MultiplierPPM = multiplier
		if request.OfficialDiscount != nil {
			effective.Source = "official_discount"
		} else {
			effective.Source = "override"
		}
		effective.RuleId = 0
		effective.RuleVersion = 0
	}
	quota, err := model.ApplyResellerMultiplier(baseQuota, effective.MultiplierPPM)
	if err != nil {
		handleResellerPricingError(c, err)
		return
	}
	writeResellerAdminSuccess(c, http.StatusOK, resellerPlatformPreviewResponse{
		Model: strings.TrimSpace(request.Model), BaseQuota: strconv.FormatInt(baseQuota, 10),
		Multiplier: model.FormatResellerMultiplier(effective.MultiplierPPM), EffectiveQuota: strconv.FormatInt(quota, 10),
		Source: effective.Source, RuleId: effective.RuleId, RuleVersion: effective.RuleVersion,
		OfficialDiscount: model.GetPricingOfficialDiscount(strings.TrimSpace(request.Model), effective.MultiplierPPM),
	})
}

func GetResellerManagementPricing(c *gin.Context) {
	resellerContext, ok := resellerManagementContext(c)
	if !ok {
		return
	}
	if resellerContext.Role == model.ResellerRoleSubagent && !resellerContext.CanManagePricing {
		middleware.AbortResellerRequest(c, http.StatusForbidden, middleware.ResellerErrorForbidden, "reseller pricing forbidden")
		return
	}
	customerId, ok := optionalPositiveQueryID(c, "customer_id")
	if !ok {
		return
	}
	var records []model.ResellerPriceRuleRecord
	var err error
	if resellerContext.Role == model.ResellerRoleSubagent {
		records, err = model.ListResellerSubagentPriceRules(resellerContext.ResellerId, resellerContext.MemberId, customerId)
	} else {
		records, err = model.ListResellerPriceRules(resellerContext.ResellerId, customerId)
	}
	if err != nil {
		handleResellerPricingError(c, err)
		return
	}
	models := resellerPricingModels(records)
	writeResellerAdminSuccess(c, http.StatusOK, gin.H{
		"models":             models,
		"official_discounts": resellerPricingOfficialDiscounts(models),
		"rules":              records,
	})
}

func resellerPricingModels(records []model.ResellerPriceRuleRecord) []string {
	models := model.GetEnabledModels()
	for _, record := range records {
		models = append(models, record.Model)
	}

	unique := make(map[string]struct{}, len(models))
	filtered := make([]string, 0, len(models))
	for _, name := range models {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, seen := unique[name]; seen {
			continue
		}
		unique[name] = struct{}{}
		filtered = append(filtered, name)
	}
	sort.Strings(filtered)
	return filtered
}

func CreateResellerManagementRetailPrice(c *gin.Context) {
	resellerContext, ok := resellerManagementContext(c)
	if !ok {
		return
	}
	if !resellerManagementWriteAllowed(resellerContext.Role) && !(resellerContext.Role == model.ResellerRoleSubagent && resellerContext.CanManagePricing) {
		middleware.AbortResellerRequest(c, http.StatusForbidden, middleware.ResellerErrorForbidden, "reseller pricing write forbidden")
		return
	}
	request, ok := resellerPriceRuleBody(c, true)
	if !ok {
		return
	}
	var requiredSubagentMemberId *int
	if resellerContext.Role == model.ResellerRoleSubagent {
		if request.CustomerId == nil {
			middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "subagent pricing requires customer_id")
			return
		}
		requiredSubagentMemberId = &resellerContext.MemberId
	}
	rule, err := createResellerPriceRule(resellerContext.ResellerId, model.ResellerPriceRuleKindRetail, request, resellerContext.Subject, requiredSubagentMemberId)
	if err != nil {
		handleResellerPricingError(c, err)
		return
	}
	writeResellerAdminSuccess(c, http.StatusCreated, gin.H{"rule": rule.Record()})
}

func PreviewResellerManagementPricing(c *gin.Context) {
	resellerContext, ok := resellerManagementContext(c)
	if !ok {
		return
	}
	if resellerContext.Role == model.ResellerRoleSubagent && !resellerContext.CanManagePricing {
		middleware.AbortResellerRequest(c, http.StatusForbidden, middleware.ResellerErrorForbidden, "reseller pricing forbidden")
		return
	}
	request, baseQuota, ok := resellerPricingPreviewBody(c, true)
	if !ok {
		return
	}
	customerId := 0
	if request.CustomerId != nil {
		customerId = *request.CustomerId
		var err error
		if resellerContext.Role == model.ResellerRoleSubagent {
			_, err = model.GetResellerSubagentCustomerRecord(resellerContext.ResellerId, resellerContext.MemberId, customerId)
		} else {
			_, err = model.GetResellerCustomerRecord(resellerContext.ResellerId, customerId, false)
		}
		if err != nil {
			handleResellerPricingError(c, err)
			return
		}
	}
	modelName := strings.TrimSpace(request.Model)
	now := common.GetTimestamp()
	retail, err := model.ResolveResellerRetailPrice(resellerContext.ResellerId, customerId, modelName, now)
	if err != nil {
		handleResellerPricingError(c, err)
		return
	}
	wholesale, err := model.ResolveResellerWholesalePrice(resellerContext.ResellerId, modelName, now)
	if err != nil {
		handleResellerPricingError(c, err)
		return
	}
	if multiplier, override, parseErr := resellerPriceOverride(request.Model, request.Multiplier, request.OfficialDiscount); parseErr != nil {
		handleResellerPricingError(c, parseErr)
		return
	} else if override {
		retail.MultiplierPPM = multiplier
		retail.RuleId = 0
		retail.RuleVersion = 0
		if request.OfficialDiscount != nil {
			retail.Source = "official_discount"
		} else {
			retail.Source = "override"
		}
	}
	if retail.MultiplierPPM < wholesale.MultiplierPPM {
		handleResellerPricingError(c, model.ErrResellerPriceMarginConflict)
		return
	}
	retailQuota, err := model.ApplyResellerMultiplier(baseQuota, retail.MultiplierPPM)
	if err != nil {
		handleResellerPricingError(c, err)
		return
	}
	wholesaleQuota, err := model.ApplyResellerMultiplier(baseQuota, wholesale.MultiplierPPM)
	if err != nil {
		handleResellerPricingError(c, err)
		return
	}
	writeResellerAdminSuccess(c, http.StatusOK, resellerManagementPreviewResponse{
		Model: modelName, BaseQuota: strconv.FormatInt(baseQuota, 10), CustomerId: request.CustomerId,
		RetailMultiplier: model.FormatResellerMultiplier(retail.MultiplierPPM), RetailQuota: strconv.FormatInt(retailQuota, 10),
		RetailSource: retail.Source, RetailRuleId: retail.RuleId, RetailRuleVersion: retail.RuleVersion,
		WholesaleMultiplier: model.FormatResellerMultiplier(wholesale.MultiplierPPM), WholesaleQuota: strconv.FormatInt(wholesaleQuota, 10),
		WholesaleSource: wholesale.Source, WholesaleRuleId: wholesale.RuleId, WholesaleRuleVersion: wholesale.RuleVersion,
		RetailOfficialDiscount:    model.GetPricingOfficialDiscount(modelName, retail.MultiplierPPM),
		WholesaleOfficialDiscount: model.GetPricingOfficialDiscount(modelName, wholesale.MultiplierPPM),
	})
}

func resellerPriceRuleBody(c *gin.Context, allowCustomer bool) (resellerPriceRuleRequest, bool) {
	body, ok := resellerRequestBody(c, resellerManagementBodyLimit)
	if !ok {
		return resellerPriceRuleRequest{}, false
	}
	var request resellerPriceRuleRequest
	if common.Unmarshal(body, &request) != nil || len(request.ResellerId) != 0 || strings.TrimSpace(request.Model) == "" ||
		(!allowCustomer && request.CustomerId != nil) || request.CustomerId != nil && *request.CustomerId < 1 {
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid request")
		return resellerPriceRuleRequest{}, false
	}
	return request, true
}

func createResellerPriceRule(resellerId int, kind string, request resellerPriceRuleRequest, createdBy string, requiredSubagentMemberId *int) (*model.ResellerPriceRule, error) {
	multiplierPPM, override, err := resellerPriceOverride(request.Model, request.Multiplier, request.OfficialDiscount)
	if err != nil || !override {
		if err == nil {
			err = model.ErrInvalidResellerPriceRule
		}
		return nil, err
	}
	customerId := 0
	if request.CustomerId != nil {
		customerId = *request.CustomerId
	}
	return model.CreateResellerPriceRule(model.CreateResellerPriceRuleParams{
		ResellerId: resellerId, Kind: kind, ModelName: request.Model, CustomerId: customerId,
		MultiplierPPM: multiplierPPM, ExpectedVersion: request.ExpectedVersion,
		Enabled: true, EffectiveAt: common.GetTimestamp(), CreatedBy: createdBy,
		RequiredSubagentMemberId: requiredSubagentMemberId,
	})
}

func resellerPricingPreviewBody(c *gin.Context, allowCustomer bool) (resellerPricingPreviewRequest, int64, bool) {
	body, ok := resellerRequestBody(c, resellerManagementBodyLimit)
	if !ok {
		return resellerPricingPreviewRequest{}, 0, false
	}
	var request resellerPricingPreviewRequest
	if common.Unmarshal(body, &request) != nil || len(request.ResellerId) != 0 || strings.TrimSpace(request.Model) == "" ||
		len(request.BaseQuota) == 0 || (!allowCustomer && request.CustomerId != nil) ||
		request.CustomerId != nil && *request.CustomerId < 1 || request.Multiplier != nil && request.OfficialDiscount != nil {
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid request")
		return resellerPricingPreviewRequest{}, 0, false
	}
	baseQuota, err := parseResellerQuota(request.BaseQuota)
	if err != nil {
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid request")
		return resellerPricingPreviewRequest{}, 0, false
	}
	return request, baseQuota, true
}

func resellerPriceOverride(modelName string, multiplier *string, officialDiscount *string) (int64, bool, error) {
	if multiplier != nil && officialDiscount != nil {
		return 0, false, model.ErrInvalidResellerPriceRule
	}
	if officialDiscount != nil {
		value, err := model.ResellerMultiplierFromOfficialDiscount(strings.TrimSpace(modelName), *officialDiscount)
		return value, true, err
	}
	if multiplier != nil {
		value, err := model.ParseResellerMultiplier(*multiplier)
		return value, true, err
	}
	return 0, false, nil
}

func resellerPricingOfficialDiscounts(models []string) map[string]*model.PricingOfficialDiscount {
	references := make(map[string]*model.PricingOfficialDiscount)
	for _, modelName := range models {
		if reference := model.GetPricingOfficialDiscount(modelName, model.ResellerDefaultMultiplierPPM); reference != nil {
			references[modelName] = reference
		}
	}
	return references
}

func parseResellerQuota(raw json.RawMessage) (int64, error) {
	value := strings.TrimSpace(string(raw))
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		var decoded string
		if common.Unmarshal(raw, &decoded) != nil {
			return 0, model.ErrInvalidResellerPriceRule
		}
		value = decoded
	}
	quota, err := strconv.ParseInt(value, 10, 64)
	if err != nil || quota < 0 {
		return 0, model.ErrInvalidResellerPriceRule
	}
	return quota, nil
}

func optionalPositiveQueryID(c *gin.Context, name string) (*int, bool) {
	value, exists := c.GetQuery(name)
	if !exists {
		return nil, true
	}
	id, err := strconv.Atoi(value)
	if err != nil || id < 1 {
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid request")
		return nil, false
	}
	return &id, true
}

func handleResellerPricingError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, model.ErrInvalidResellerPriceRule), errors.Is(err, model.ErrResellerQuotaOverflow):
		middleware.AbortResellerRequest(c, http.StatusBadRequest, middleware.ResellerErrorInvalidRequest, "invalid pricing request")
	case errors.Is(err, model.ErrResellerPriceRuleVersionConflict):
		middleware.AbortResellerRequest(c, http.StatusConflict, middleware.ResellerErrorPricingVersion, "pricing version changed; refresh rules and retry")
	case errors.Is(err, model.ErrResellerPriceMarginConflict):
		middleware.AbortResellerRequest(c, http.StatusConflict, middleware.ResellerErrorPricingMargin, "customer retail multiplier must be greater than or equal to wholesale multiplier")
	case errors.Is(err, model.ErrResellerNotFound):
		middleware.AbortResellerRequest(c, http.StatusNotFound, middleware.ResellerErrorNotFound, "reseller not found")
	case errors.Is(err, model.ErrResellerCustomerNotFound):
		middleware.AbortResellerRequest(c, http.StatusNotFound, middleware.ResellerErrorNotFound, "reseller customer not found")
	default:
		logger.LogError(c.Request.Context(), "reseller pricing database error: "+err.Error())
		middleware.AbortResellerRequest(c, http.StatusInternalServerError, middleware.ResellerErrorInternal, "internal error")
	}
}
