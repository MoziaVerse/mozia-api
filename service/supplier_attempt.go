package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

// PrepareSupplierRequest is the final guard on the OpenAI adaptor, including
// direct channel tests, playground, affinity, specified channels and old routing.
type SupplierAdmissionError struct{ Cause error }

func (e *SupplierAdmissionError) Error() string { return e.Cause.Error() }
func (e *SupplierAdmissionError) Unwrap() error { return e.Cause }

func PrepareSupplierRequest(c *gin.Context, info *relaycommon.RelayInfo, body io.Reader) (prepared io.Reader, resultErr error) {
	defer func() {
		if resultErr != nil {
			resultErr = &SupplierAdmissionError{Cause: resultErr}
		}
	}()
	r, err := ReadSupplierRuntime(c.Request.Context())
	if err != nil {
		return nil, err
	}
	managedChannel := false
	for _, b := range r.Config.Bindings {
		if b.ChannelID == info.ChannelId {
			managedChannel = true
			break
		}
	}
	if !managedChannel {
		return body, nil
	}
	poolID := r.Bindings[fmt.Sprintf("%d:%s", info.ChannelId, info.OriginModelName)]
	if poolID == 0 || info.ChannelType != constant.ChannelTypeOpenAI || info.RelayMode != relayconstant.RelayModeChatCompletions || info.RelayFormat != types.RelayFormatOpenAI {
		return nil, errors.New("managed supplier channel only permits admitted OpenAI text chat requests")
	}
	if body == nil {
		return nil, errors.New("supplier request body missing")
	}
	data, err := io.ReadAll(io.LimitReader(body, 16*1024*1024+1))
	if err != nil {
		return nil, err
	}
	if len(data) > 16*1024*1024 {
		return nil, errors.New("supplier request exceeds 16 MiB admission limit")
	}
	var request dto.GeneralOpenAIRequest
	if err := common.Unmarshal(data, &request); err != nil {
		return nil, err
	}
	if request.Model != info.UpstreamModelName {
		return nil, errors.New("outbound model override is not an accepted supplier binding")
	}
	pool := r.Pools[strconv.FormatInt(poolID, 10)]
	for _, declared := range info.ChannelOtherSettings.SupplierDeclarations {
		if declared.ID != request.Model {
			continue
		}
		for _, accepted := range pool.Models {
			if accepted.Name != info.OriginModelName {
				continue
			}
			if (declared.Version != "" && declared.Version != accepted.Version) || (declared.ContextLength > 0 && declared.ContextLength < accepted.ContextTokens) || (declared.MaxOutputTokens > 0 && declared.MaxOutputTokens < accepted.MaxOutputTokens) {
				return nil, errors.New("supplier model declaration changed; confirm its accepted specification before dispatch")
			}
		}
	}
	tokens, err := SupplierCandidateTokens(&request, info, pool)
	if err != nil {
		return nil, err
	}
	s := SupplierRoutingState(c)
	if s == nil {
		if err := InitSupplierRouting(c, info); err != nil {
			return nil, err
		}
		s = SupplierRoutingState(c)
	}
	if s == nil {
		return nil, errors.New("supplier admission not initialized")
	}
	if s.Current == nil || s.Finished {
		ch, err := model.CacheGetChannel(info.ChannelId)
		if err != nil {
			return nil, err
		}
		// Probe admission must use the final converted request, too.
		original := info.Request
		info.Request = &request
		_, err = SelectSupplierChannel(c, info, ch)
		info.Request = original
		if err != nil {
			return nil, err
		}
	}
	if s.Current.ChannelID != info.ChannelId || s.Current.PoolID != poolID {
		return nil, errors.New("supplier binding changed before dispatch")
	}
	if tokens > s.ReservedTokens {
		return nil, errors.New("outbound overrides exceed the reserved supplier token budget")
	}
	if (request.Stream != nil && *request.Stream) || (request.MaxTokens == nil && request.MaxCompletionTokens == nil) {
		// Preserve unknown upstream fields and explicit zero values.
		var fields map[string]json.RawMessage
		if err := common.Unmarshal(data, &fields); err != nil {
			return nil, err
		}
		if request.Stream != nil && *request.Stream {
			fields["stream_options"] = json.RawMessage(`{"include_usage":true}`)
		}
		if request.MaxTokens == nil && request.MaxCompletionTokens == nil {
			for _, spec := range pool.Models {
				if spec.Name == info.OriginModelName {
					fields["max_tokens"] = json.RawMessage(strconv.FormatInt(spec.MaxOutputTokens, 10))
				}
			}
		}
		data, err = common.Marshal(fields)
		if err != nil {
			return nil, err
		}
	}
	s.Started = time.Now()
	s.Sent = true
	return bytes.NewReader(data), nil
}

// ObserveSupplierResponse reads the original upstream usage, before new-api's
// fallback estimation. Estimated usage is never settled as supplier-reported cost.
func ObserveSupplierResponse(c *gin.Context, info *relaycommon.RelayInfo, data []byte, stream bool) {
	s := SupplierRoutingState(c)
	if s == nil || s.Current == nil || s.Finished {
		return
	}
	var response struct {
		ID      string     `json:"id"`
		Usage   *dto.Usage `json:"usage"`
		Choices []struct {
			FinishReason *string `json:"finish_reason"`
			Delta        struct {
				Content   string            `json:"content"`
				Reasoning string            `json:"reasoning_content"`
				ToolCalls []json.RawMessage `json:"tool_calls"`
			} `json:"delta"`
		} `json:"choices"`
	}
	if err := common.Unmarshal(data, &response); err != nil {
		return
	}
	if response.ID != "" && len(response.ID) <= 191 {
		s.Current.UpstreamRequestID = response.ID
	}
	if response.Usage != nil && response.Usage.PromptTokens >= 0 && response.Usage.CompletionTokens >= 0 {
		s.Usage = response.Usage
	}
	for _, choice := range response.Choices {
		if choice.FinishReason != nil && *choice.FinishReason != "" {
			s.Complete = true
		}
		if stream && s.FirstContent.IsZero() && (choice.Delta.Content != "" || choice.Delta.Reasoning != "" || len(choice.Delta.ToolCalls) > 0) {
			s.FirstContent = time.Now()
		}
	}

}

func SupplierStreamError(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	s := SupplierRoutingState(c)
	if s == nil || s.Current == nil {
		return nil
	}
	if !s.Complete || (info.IsStream && (info.StreamStatus == nil || info.StreamStatus.HasErrors() || (info.StreamStatus.EndReason != relaycommon.StreamEndReasonDone && info.StreamStatus.EndReason != relaycommon.StreamEndReasonEOF))) {
		return types.NewErrorWithStatusCode(errors.New("supplier response did not complete"), types.ErrorCodeBadResponseBody, http.StatusBadGateway, types.ErrOptionWithSkipRetry())
	}
	return nil
}

func SupplierCost(priceJSON string, usage *dto.Usage) (string, error) {
	if usage == nil || usage.PromptTokens < 0 || usage.CompletionTokens < 0 {
		return "", errors.New("upstream usage unavailable")
	}
	var cost model.ChannelCostPricing
	if err := common.UnmarshalJsonStr(priceJSON, &cost); err != nil {
		return "", err
	}
	if cost.Mode == model.ChannelCostModePerRequest && cost.Config.BasePrice != nil {
		return decimal.NewFromFloat(*cost.Config.BasePrice).StringFixed(8), nil
	}
	if cost.Mode != model.ChannelCostModePerToken {
		return "", errors.New("supplier cost mode needs reconciliation")
	}
	input, ok := cost.Config.Items["input"]
	if !ok {
		return "", errors.New("supplier input price missing")
	}
	output, ok := cost.Config.Items["output"]
	if !ok {
		return "", errors.New("supplier output price missing")
	}
	// ponytail: settle the accepted text-token categories; add other contracts only after their usage is verified.
	for name := range cost.Config.Items {
		if name != "input" && name != "output" && name != "cache_read" {
			return "", errors.New("supplier price category needs reconciliation")
		}
	}
	inputTokens := int64(usage.PromptTokens)
	amount := decimal.Zero
	if cachedPrice, ok := cost.Config.Items["cache_read"]; ok {
		cached := int64(usage.PromptTokensDetails.CachedTokens)
		if cached < 0 || cached > inputTokens {
			return "", errors.New("invalid upstream cache usage")
		}
		inputTokens -= cached
		amount = decimal.NewFromInt(cached).Mul(decimal.NewFromFloat(cachedPrice))
	}
	amount = amount.Add(decimal.NewFromInt(inputTokens).Mul(decimal.NewFromFloat(input))).Add(decimal.NewFromInt(int64(usage.CompletionTokens)).Mul(decimal.NewFromFloat(output)))
	return amount.Div(decimal.NewFromInt(1000000)).StringFixed(8), nil
}

func FinishSupplierAttempt(c *gin.Context, info *relaycommon.RelayInfo, apiErr *types.NewAPIError, cancelled bool) {
	s := SupplierRoutingState(c)
	if s == nil || s.Current == nil || s.Finished {
		return
	}
	s.Finished = true
	a := s.Current
	a.Status = "success"
	class := "success"
	release := true
	if cancelled || !s.Sent {
		a.Status = "cancelled"
		class = "excluded"
		cancelled = true
	}
	if apiErr != nil && !cancelled {
		a.StatusCode = apiErr.StatusCode
		a.Status = "failed"
		class = "failure"
		if apiErr.StatusCode == 429 {
			class = "overload"
		}
		if apiErr.StatusCode >= 400 && apiErr.StatusCode < 500 && apiErr.StatusCode != 429 {
			class = "excluded"
		}
		if apiErr.GetErrorCode() == types.ErrorCodeDoRequestFailed || (info.IsStream && !s.Complete && apiErr.StatusCode >= 500) {
			a.Status = "unknown"
			release = false
		}
	}
	if !cancelled && s.Sent && !s.Complete && (apiErr == nil || apiErr.StatusCode >= 500) {
		a.Status = "unknown"
		class = "failure"
		release = false
	}
	if c.Request.Context().Err() != nil && !cancelled {
		class = "excluded"
		a.Status = "unknown"
		release = false
	}
	a.OutcomeClass = class
	a.LatencyMs = time.Since(s.Started).Milliseconds()
	a.FinishedAt = time.Now().Unix()
	if !s.FirstContent.IsZero() {
		a.TTFTMs = s.FirstContent.Sub(s.Started).Milliseconds()
	}
	tokens := int64(-1)
	if s.Usage != nil {
		a.InputTokens = int64(s.Usage.PromptTokens)
		a.OutputTokens = int64(s.Usage.CompletionTokens)
		tokens = a.InputTokens + a.OutputTokens
		data, _ := common.Marshal(s.Usage)
		a.UsageJSON = string(data)
		if amount, err := SupplierCost(a.PriceJSON, s.Usage); err == nil && s.Complete && a.Status != "unknown" {
			a.Cost = amount
			a.CostStatus = "calculated"
		}
	}
	if cancelled {
		a.Cost = "0.00000000"
		a.CostStatus = "not_sent"
	}
	ctx, stop := context.WithTimeout(context.Background(), 3*time.Second)
	defer stop()
	input, _ := common.Marshal(map[string]any{"id": s.ReservationID, "cancel": cancelled, "release": release, "tokens": tokens, "class": class, "ttft": a.TTFTMs})
	if err := supplierAdmission.Run(ctx, common.RDB, []string{supplierRuntimeKey}, "finish", string(input)).Err(); err != nil {
		common.SysError("supplier capacity completion failed: " + err.Error())
	}
	if a.ID != 0 {
		if err := model.DB.WithContext(ctx).Model(&model.SupplierAttempt{}).Where("id = ? AND status = ?", a.ID, "pending").Updates(map[string]any{"outcome_class": a.OutcomeClass, "status": a.Status, "finished_at": a.FinishedAt, "status_code": a.StatusCode, "latency_ms": a.LatencyMs, "ttft_ms": a.TTFTMs, "input_tokens": a.InputTokens, "output_tokens": a.OutputTokens, "usage_json": a.UsageJSON, "cost": a.Cost, "cost_status": a.CostStatus, "upstream_request_id": a.UpstreamRequestID}).Error; err != nil {
			common.SysError("supplier attempt completion pending reconciliation: " + err.Error())
		}
	}
	s.Excluded[a.ChannelID] = true
	if class == "failure" || class == "overload" {
		s.ExcludedPools[a.PoolID] = true
	}
	if class == "failure" {
		s.ExcludedDomains[s.Runtime.Pools[strconv.FormatInt(a.PoolID, 10)].FailureDomain] = true
	}
}
