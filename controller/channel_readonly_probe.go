package controller

import (
	"errors"
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
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

const maxProbeResponseBytes = 1 << 20

type probeError struct {
	Code       string
	Message    string
	StatusCode int
}

func (e *probeError) Error() string {
	if e == nil {
		return ""
	}
	return e.Code
}

func normalizeBaseURLIdentity(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return ""
	}
	scheme := strings.ToLower(parsed.Scheme)
	host := strings.ToLower(parsed.Host)
	if scheme == "" {
		return host
	}
	return scheme + "://" + host
}

func channelSupportsBalanceProbe(channel *model.Channel) bool {
	if channel == nil || channel.ChannelInfo.IsMultiKey {
		return false
	}
	switch channel.Type {
	case constant.ChannelTypeOpenAI,
		constant.ChannelTypeCustom,
		constant.ChannelTypeAIProxy,
		constant.ChannelTypeAPI2GPT,
		constant.ChannelTypeAIGC2D,
		constant.ChannelTypeSiliconFlow,
		constant.ChannelTypeDeepSeek,
		constant.ChannelTypeOpenRouter,
		constant.ChannelTypeMoonshot:
		return true
	default:
		return false
	}
}

func newProbeHTTPClient(channel *model.Channel) (*http.Client, error) {
	return service.NewProxyHttpClient(channel.GetSetting().Proxy)
}

func readOnlyProbeBody(client *http.Client, method, requestURL string, headers http.Header) ([]byte, error) {
	req, err := http.NewRequest(method, requestURL, nil)
	if err != nil {
		return nil, &probeError{Code: "invalid_request", Message: "Unable to create probe request"}
	}
	for k := range headers {
		req.Header.Add(k, headers.Get(k))
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, &probeError{Code: "request_failed", Message: "Upstream probe request failed"}
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxProbeResponseBytes))
	if err != nil {
		return nil, &probeError{Code: "read_failed", Message: "Upstream probe response could not be read"}
	}
	if resp.StatusCode != http.StatusOK {
		return nil, &probeError{Code: "upstream_http_status", Message: "Upstream probe request failed", StatusCode: resp.StatusCode}
	}
	return body, nil
}

func readOnlyProbeJSON(client *http.Client, method, requestURL string, headers http.Header, target any) error {
	body, err := readOnlyProbeBody(client, method, requestURL, headers)
	if err != nil {
		return err
	}
	if err := common.Unmarshal(body, target); err != nil {
		return &probeError{Code: "parse_failed", Message: "Upstream probe response could not be parsed"}
	}
	return nil
}

func probeOpenAIBalance(client *http.Client, channel *model.Channel) (float64, string, error) {
	baseURL := constant.ChannelBaseURLs[channel.Type]
	if channel.GetBaseURL() != "" {
		baseURL = channel.GetBaseURL()
	}

	var subscription OpenAISubscriptionResponse
	if err := readOnlyProbeJSON(client, http.MethodGet, fmt.Sprintf("%s/v1/dashboard/billing/subscription", baseURL), GetAuthHeader(channel.Key), &subscription); err != nil {
		return 0, "USD", err
	}
	now := time.Now()
	startDate := fmt.Sprintf("%s-01", now.Format("2006-01"))
	endDate := now.Format("2006-01-02")
	if !subscription.HasPaymentMethod {
		startDate = now.AddDate(0, 0, -100).Format("2006-01-02")
	}
	var usage OpenAIUsageResponse
	if err := readOnlyProbeJSON(client, http.MethodGet, fmt.Sprintf("%s/v1/dashboard/billing/usage?start_date=%s&end_date=%s", baseURL, startDate, endDate), GetAuthHeader(channel.Key), &usage); err != nil {
		return 0, "USD", err
	}
	return subscription.HardLimitUSD - usage.TotalUsage/100, "USD", nil
}

func probeAIProxyBalance(client *http.Client, channel *model.Channel) (float64, string, error) {
	headers := http.Header{}
	headers.Add("Api-Key", channel.Key)
	var response AIProxyUserOverviewResponse
	if err := readOnlyProbeJSON(client, http.MethodGet, "https://aiproxy.io/api/report/getUserOverview", headers, &response); err != nil {
		return 0, "points", err
	}
	if !response.Success {
		return 0, "points", &probeError{Code: "upstream_rejected", Message: "Upstream balance probe failed"}
	}
	return response.Data.TotalPoints, "points", nil
}

func probeAPI2GPTBalance(client *http.Client, channel *model.Channel) (float64, string, error) {
	var response API2GPTUsageResponse
	if err := readOnlyProbeJSON(client, http.MethodGet, "https://api.api2gpt.com/dashboard/billing/credit_grants", GetAuthHeader(channel.Key), &response); err != nil {
		return 0, "USD", err
	}
	return response.TotalRemaining, "USD", nil
}

func probeAIGC2DBalance(client *http.Client, channel *model.Channel) (float64, string, error) {
	var response APGC2DGPTUsageResponse
	if err := readOnlyProbeJSON(client, http.MethodGet, "https://api.aigc2d.com/dashboard/billing/credit_grants", GetAuthHeader(channel.Key), &response); err != nil {
		return 0, "USD", err
	}
	return response.TotalAvailable, "USD", nil
}

func probeSiliconFlowBalance(client *http.Client, channel *model.Channel) (float64, string, error) {
	var response SiliconFlowUsageResponse
	if err := readOnlyProbeJSON(client, http.MethodGet, "https://api.siliconflow.cn/v1/user/info", GetAuthHeader(channel.Key), &response); err != nil {
		return 0, "USD", err
	}
	if response.Code != 20000 {
		return 0, "USD", &probeError{Code: "upstream_rejected", Message: "Upstream balance probe failed"}
	}
	balance, err := strconv.ParseFloat(response.Data.TotalBalance, 64)
	if err != nil {
		return 0, "USD", &probeError{Code: "parse_failed", Message: "Upstream probe response could not be parsed"}
	}
	return balance, "USD", nil
}

func probeDeepSeekBalance(client *http.Client, channel *model.Channel) (float64, string, error) {
	var response DeepSeekUsageResponse
	if err := readOnlyProbeJSON(client, http.MethodGet, "https://api.deepseek.com/user/balance", GetAuthHeader(channel.Key), &response); err != nil {
		return 0, "CNY", err
	}
	for _, info := range response.BalanceInfos {
		if info.Currency != "CNY" {
			continue
		}
		balance, err := strconv.ParseFloat(info.TotalBalance, 64)
		if err != nil {
			return 0, "CNY", &probeError{Code: "parse_failed", Message: "Upstream probe response could not be parsed"}
		}
		return balance, "CNY", nil
	}
	return 0, "CNY", &probeError{Code: "upstream_rejected", Message: "Upstream balance probe failed"}
}

func probeOpenRouterBalance(client *http.Client, channel *model.Channel) (float64, string, error) {
	var response OpenRouterCreditResponse
	if err := readOnlyProbeJSON(client, http.MethodGet, "https://openrouter.ai/api/v1/credits", GetAuthHeader(channel.Key), &response); err != nil {
		return 0, "USD", err
	}
	return response.Data.TotalCredits - response.Data.TotalUsage, "USD", nil
}

func probeMoonshotBalance(client *http.Client, channel *model.Channel) (float64, string, error) {
	type moonshotBalanceData struct {
		AvailableBalance float64 `json:"available_balance"`
	}
	type moonshotBalanceResponse struct {
		Code   int                 `json:"code"`
		Data   moonshotBalanceData `json:"data"`
		Status bool                `json:"status"`
	}
	var response moonshotBalanceResponse
	if err := readOnlyProbeJSON(client, http.MethodGet, "https://api.moonshot.cn/v1/users/me/balance", GetAuthHeader(channel.Key), &response); err != nil {
		return 0, "USD", err
	}
	if !response.Status || response.Code != 0 {
		return 0, "USD", &probeError{Code: "upstream_rejected", Message: "Upstream balance probe failed"}
	}
	return decimal.NewFromFloat(response.Data.AvailableBalance).Div(decimal.NewFromFloat(operation_setting.Price)).InexactFloat64(), "USD", nil
}

func probeChannelBalanceReadOnly(channel *model.Channel) dto.ChannelBalanceProbeDTO {
	result := dto.ChannelBalanceProbeDTO{
		Status:           "unsupported",
		Supported:        false,
		CheckedAt:        time.Now().Unix(),
		ProviderIdentity: channelOwnerName(channel.Type),
	}
	if !channelSupportsBalanceProbe(channel) {
		result.SanitizedErrorCode = "unsupported_channel_type"
		result.SanitizedErrorMessage = "Balance probing is not supported for this channel"
		return result
	}

	client, err := newProbeHTTPClient(channel)
	if err != nil {
		result.Status = "failed"
		result.Supported = true
		result.SanitizedErrorCode = "client_init_failed"
		result.SanitizedErrorMessage = "Unable to initialize probe client"
		return result
	}

	var balance float64
	var unit string
	switch channel.Type {
	case constant.ChannelTypeOpenAI, constant.ChannelTypeCustom:
		balance, unit, err = probeOpenAIBalance(client, channel)
	case constant.ChannelTypeAIProxy:
		balance, unit, err = probeAIProxyBalance(client, channel)
	case constant.ChannelTypeAPI2GPT:
		balance, unit, err = probeAPI2GPTBalance(client, channel)
	case constant.ChannelTypeAIGC2D:
		balance, unit, err = probeAIGC2DBalance(client, channel)
	case constant.ChannelTypeSiliconFlow:
		balance, unit, err = probeSiliconFlowBalance(client, channel)
	case constant.ChannelTypeDeepSeek:
		balance, unit, err = probeDeepSeekBalance(client, channel)
	case constant.ChannelTypeOpenRouter:
		balance, unit, err = probeOpenRouterBalance(client, channel)
	case constant.ChannelTypeMoonshot:
		balance, unit, err = probeMoonshotBalance(client, channel)
	default:
		err = &probeError{Code: "unsupported_channel_type", Message: "Balance probing is not supported for this channel"}
	}
	result.Supported = true
	result.UnitOrCurrency = unit
	if err != nil {
		result.Status = "failed"
		var perr *probeError
		if errors.As(err, &perr) {
			result.SanitizedErrorCode = perr.Code
			result.SanitizedErrorMessage = perr.Message
		} else {
			result.SanitizedErrorCode = "probe_failed"
			result.SanitizedErrorMessage = "Balance probing failed"
		}
		return result
	}
	result.Status = "success"
	result.Balance = &balance
	return result
}

func GetChannelDiscovery(c *gin.Context) {
	groupFilter := model.NormalizeChannelGroupFilter(c.Query("group"))
	statusFilter := parseStatusFilter(c.Query("status"))
	typeFilter := -1
	if typeStr := c.Query("type"); typeStr != "" {
		if parsed, err := strconv.Atoi(typeStr); err == nil {
			typeFilter = parsed
		}
	}
	var channels []*model.Channel
	query := buildChannelListQuery(groupFilter, statusFilter, typeFilter).Order("id asc").Omit("key")
	if err := query.Find(&channels).Error; err != nil {
		common.ApiSuccess(c, []dto.ChannelDiscoveryDTO{})
		return
	}
	items := make([]dto.ChannelDiscoveryDTO, 0, len(channels))
	for _, channel := range channels {
		items = append(items, dto.ChannelDiscoveryDTO{
			ChannelID:        channel.Id,
			ChannelName:      channel.Name,
			ChannelType:      channel.Type,
			ProviderIdentity: channelOwnerName(channel.Type),
			BaseURLIdentity:  normalizeBaseURLIdentity(channel.GetBaseURL()),
			Enabled:          channel.Status == common.ChannelStatusEnabled,
			ProbeCapability:  channelSupportsBalanceProbe(channel),
		})
	}
	common.ApiSuccess(c, items)
}

func ProbeChannelBalance(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		common.ApiSuccess(c, dto.ChannelBalanceProbeDTO{
			Status:                "failed",
			Supported:             false,
			CheckedAt:             time.Now().Unix(),
			SanitizedErrorCode:    "invalid_channel_id",
			SanitizedErrorMessage: "Channel lookup failed",
		})
		return
	}
	channel, err := model.CacheGetChannel(id)
	if err != nil {
		common.ApiSuccess(c, dto.ChannelBalanceProbeDTO{
			Status:                "failed",
			Supported:             false,
			CheckedAt:             time.Now().Unix(),
			SanitizedErrorCode:    "channel_not_found",
			SanitizedErrorMessage: "Channel lookup failed",
		})
		return
	}
	common.ApiSuccess(c, probeChannelBalanceReadOnly(channel))
}
