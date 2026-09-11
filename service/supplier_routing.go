package service

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"hash/fnv"
	"sort"
	"strconv"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/go-redis/redis/v8"
)

const supplierRuntimeKey = "supplier-routing:config"
const supplierStateKey = "supplier_routing_state"

//go:embed supplier_admission.lua
var supplierAdmissionLua string
var supplierAdmission = redis.NewScript(supplierAdmissionLua)

type SupplierRuntime struct {
	Config    model.SupplierRoutingConfig   `json:"config"`
	Pools     map[string]model.SupplierPool `json:"pools"`
	Bindings  map[string]int64              `json:"bindings"`
	Suppliers map[string]bool               `json:"suppliers"`
	Blocked   bool                          `json:"blocked"`
}

type SupplierCandidate struct {
	Expires    int64 `json:"expires"`
	ChannelID  int   `json:"channel_id"`
	PoolID     int64 `json:"pool_id"`
	SupplierID int64 `json:"supplier_id"`
	Priority   int64 `json:"priority"`
	Weight     int64 `json:"weight"`
	Tokens     int64 `json:"tokens"`
}

type SupplierRouteState struct {
	Cancel          context.CancelFunc
	Runtime         *SupplierRuntime
	Rule            model.SupplierRoutingRule
	Group           string
	RequestID       string
	AttemptNumber   int
	Excluded        map[int]bool
	ExcludedPools   map[int64]bool
	ExcludedDomains map[string]bool
	Current         *model.SupplierAttempt
	ReservationID   string
	ReservedTokens  int64
	Started         time.Time
	FirstContent    time.Time
	Usage           *dto.Usage
	Sent            bool
	Complete        bool
	Finished        bool
	Managed         bool
	Shadow          bool
	ShadowRecorded  bool
}

func DefaultSupplierHealth() model.SupplierHealthPolicy {
	return model.SupplierHealthPolicy{WindowSeconds: 60, MinSamples: 10, FailurePercent: 20, MaxTTFTMs: 5000, CooldownSeconds: 30, TrialPercent: 10}
}

func BuildSupplierRuntime(cfg *model.SupplierRoutingConfig) *SupplierRuntime {
	r := &SupplierRuntime{Config: *cfg, Pools: map[string]model.SupplierPool{}, Bindings: map[string]int64{}, Suppliers: map[string]bool{}}
	for _, p := range cfg.Pools {
		r.Pools[strconv.FormatInt(p.ID, 10)] = p
	}
	for _, b := range cfg.Bindings {
		r.Bindings[fmt.Sprintf("%d:%s", b.ChannelID, b.Model)] = b.PoolID
	}
	for _, s := range cfg.Suppliers {
		r.Suppliers[strconv.FormatInt(s.ID, 10)] = s.Enabled
	}
	return r
}

// Redis is read at each outgoing call as well as selection. This deliberately
// prevents a stale channel cache or a legacy selector from bypassing admission.
func ReadSupplierRuntime(ctx context.Context) (*SupplierRuntime, error) {
	if !common.RedisEnabled || common.RDB == nil {
		common.OptionMapRWMutex.RLock()
		configured := common.OptionMap[model.SupplierRoutingOptionKey] != ""
		common.OptionMapRWMutex.RUnlock()
		if configured {
			return nil, errors.New("supplier resource admission requires shared Redis")
		}
		return BuildSupplierRuntime(&model.SupplierRoutingConfig{}), nil
	}
	raw, err := common.RDB.Get(ctx, supplierRuntimeKey).Result()
	if errors.Is(err, redis.Nil) {
		cfg, dbErr := model.ReadSupplierRoutingConfig()
		if dbErr != nil {
			return nil, dbErr
		}
		if cfg.Revision != 0 {
			return nil, errors.New("supplier state missing: restore configuration after reconciling in-flight capacity")
		}
		r := BuildSupplierRuntime(cfg)
		data, _ := common.Marshal(r)
		created, err := common.RDB.SetNX(ctx, supplierRuntimeKey, string(data), 0).Result()
		if err != nil {
			return nil, err
		}
		if created {
			return r, nil
		}
		// A publisher won the race; do not return the stale empty configuration.
		raw, err = common.RDB.Get(ctx, supplierRuntimeKey).Result()
	}
	if err != nil {
		return nil, err
	}
	var r SupplierRuntime
	if err := common.UnmarshalJsonStr(raw, &r); err != nil {
		return nil, err
	}
	if r.Blocked {
		return nil, errors.New("supplier routing publication in progress")
	}
	return &r, nil
}

func MatchSupplierRule(cfg *model.SupplierRoutingConfig, userID int, group, modelName string) *model.SupplierRoutingRule {
	var selected *model.SupplierRoutingRule
	best := -1
	for i := range cfg.Rules {
		r := &cfg.Rules[i]
		if r.Model != modelName || (r.UserID != 0 && r.UserID != userID) || (r.Group != "" && r.Group != group) {
			continue
		}
		rank := 0
		if r.Group != "" {
			rank = 1
		}
		if r.UserID != 0 {
			rank += 2
		}
		if rank > best {
			selected = r
			best = rank
		}
	}
	return selected
}

func SupplierRoutingState(c *gin.Context) *SupplierRouteState {
	v, ok := c.Get(supplierStateKey)
	if !ok {
		return nil
	}
	s, _ := v.(*SupplierRouteState)
	return s
}

func InitSupplierRouting(c *gin.Context, info *relaycommon.RelayInfo) error {
	r, err := ReadSupplierRuntime(c.Request.Context())
	if err != nil {
		return err
	}
	pool := r.Bindings[fmt.Sprintf("%d:%s", c.GetInt("channel_id"), info.OriginModelName)]
	rule := MatchSupplierRule(&r.Config, info.UserId, info.UsingGroup, info.OriginModelName)
	if pool == 0 && (rule == nil || !r.Config.Enabled) {
		return nil
	}
	requestID := c.GetString(common.RequestIdKey)
	if requestID == "" {
		requestID = common.NewRequestId()
	}
	s := &SupplierRouteState{Runtime: r, Group: info.UsingGroup, RequestID: requestID, Excluded: map[int]bool{}, ExcludedPools: map[int64]bool{}, ExcludedDomains: map[string]bool{}, Managed: r.Config.Enabled && rule != nil}
	s.Rule = model.SupplierRoutingRule{ID: "legacy", Mode: "capacity", MaxAttempts: common.RetryTimes + 1, TimeoutSeconds: 120, Health: DefaultSupplierHealth()}
	if rule != nil {
		s.Rule = *rule
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(strconv.Itoa(info.UserId)))
	s.Shadow = s.Managed && (r.Config.Shadow || int(h.Sum32()%100) >= r.Config.CanaryPercent)
	info.SupplierPriceFrozen = s.Managed && !s.Shadow
	ctx, cancel := context.WithTimeout(c.Request.Context(), time.Duration(s.Rule.TimeoutSeconds)*time.Second)
	s.Cancel = cancel
	c.Request = c.Request.WithContext(ctx)
	c.Set(supplierStateKey, s)
	return nil
}

func SupplierCandidateTokens(request *dto.GeneralOpenAIRequest, info *relaycommon.RelayInfo, pool model.SupplierPool) (int64, error) {
	if len(request.Audio) > 0 || request.WebSearchOptions != nil || len(request.SearchParameters) > 0 {
		return 0, errors.New("supplier admission currently supports text generation without hosted search")
	}
	if len(request.Modalities) > 0 {
		var modalities []string
		if err := common.Unmarshal(request.Modalities, &modalities); err != nil {
			return 0, err
		}
		for _, modality := range modalities {
			if modality != "text" {
				return 0, errors.New("supplier admission currently supports text output only")
			}
		}
	}
	var spec *model.SupplierModelSpec
	for i := range pool.Models {
		if pool.Models[i].Name == info.OriginModelName {
			spec = &pool.Models[i]
			break
		}
	}
	if spec == nil {
		return 0, errors.New("model has no accepted supplier specification")
	}
	if (len(request.Tools) > 0 || len(request.Functions) > 0) && !spec.Tools {
		return 0, errors.New("supplier does not support tool calls")
	}
	if request.ResponseFormat != nil && request.ResponseFormat.Type != "text" && !spec.JSON {
		return 0, errors.New("supplier does not support structured output")
	}
	if request.N != nil && *request.N != 1 {
		return 0, errors.New("supplier admission currently requires n=1")
	}
	for _, m := range request.Messages {
		if !m.IsStringContent() {
			for _, part := range m.ParseContent() {
				if part.Type != dto.ContentTypeText {
					return 0, errors.New("supplier admission currently supports text inputs only")
				}
			}
		}
	}
	output := spec.MaxOutputTokens
	if request.MaxTokens != nil {
		output = int64(*request.MaxTokens)
	}
	if request.MaxCompletionTokens != nil {
		output = int64(*request.MaxCompletionTokens)
	}
	if output <= 0 || output > spec.MaxOutputTokens {
		return 0, errors.New("requested output exceeds accepted supplier specification")
	}
	// Count independently of the customer billing token-count feature flag.
	meta := request.GetTokenCountMeta()
	prompt := int64(CountTextToken(meta.CombineText, info.OriginModelName) + meta.ToolsCount*8 + meta.MessagesCount*3 + meta.NameCount*3 + 3)
	if int64(info.GetEstimatePromptTokens()) > prompt {
		prompt = int64(info.GetEstimatePromptTokens())
	}
	prompt = (prompt*pool.InputSafetyPercent + 99) / 100
	if prompt+output > spec.ContextTokens {
		return 0, errors.New("request exceeds supplier context capacity")
	}
	return prompt + output, nil
}

func SelectSupplierChannel(c *gin.Context, info *relaycommon.RelayInfo, locked *model.Channel) (*model.Channel, error) {
	s := SupplierRoutingState(c)
	if s == nil {
		return locked, nil
	}
	request, ok := info.Request.(*dto.GeneralOpenAIRequest)
	if !ok {
		return nil, errors.New("supplier routing currently supports OpenAI chat requests")
	}
	var channels []*model.Channel
	if locked != nil {
		channels = []*model.Channel{locked}
	} else {
		var err error
		channels, err = model.GetSatisfiedChannelCandidates(s.Group, info.OriginModelName, c.Request.URL.Path)
		if err != nil {
			return nil, err
		}
	}
	weights := make(map[int64]int64)
	for _, t := range s.Rule.Targets {
		weights[t.SupplierID] = t.Weight
	}
	candidates := make([]SupplierCandidate, 0, len(channels))
	byID := make(map[int]*model.Channel)
	for _, ch := range channels {
		if s.Excluded[ch.Id] || (ch.Status != common.ChannelStatusEnabled && !(info.IsChannelTest && ch.Status == common.ChannelStatusAutoDisabled)) {
			continue
		}
		poolID := s.Runtime.Bindings[fmt.Sprintf("%d:%s", ch.Id, info.OriginModelName)]
		if poolID == 0 {
			continue
		}
		pool := s.Runtime.Pools[strconv.FormatInt(poolID, 10)]
		if s.ExcludedPools[poolID] || s.ExcludedDomains[pool.FailureDomain] {
			continue
		}
		weight := weights[pool.SupplierID]
		if s.Managed && (!s.Shadow || locked == nil) && weight == 0 {
			continue
		}
		if weight == 0 {
			weight = 1
		}
		tokens, err := SupplierCandidateTokens(request, info, pool)
		if err != nil {
			continue
		}
		candidates = append(candidates, SupplierCandidate{ChannelID: ch.Id, PoolID: poolID, SupplierID: pool.SupplierID, Priority: ch.GetPriority(), Weight: weight, Tokens: tokens})
		byID[ch.Id] = ch
	}
	if len(candidates) == 0 {
		return nil, errors.New("no eligible supplier capacity for this request")
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].ChannelID < candidates[j].ChannelID })
	s.ReservationID = fmt.Sprintf("%s:%d", s.RequestID, s.AttemptNumber+1)
	shareScope := fmt.Sprintf("%q:%q:%q", s.Rule.ID, info.OriginModelName, s.Group)
	schedule := fmt.Sprintf("%s:%d", shareScope, s.Runtime.Config.Revision)
	if s.AttemptNumber > 0 || info.IsChannelTest {
		schedule += ":auxiliary"
	}
	input := map[string]any{"id": s.ReservationID, "candidates": candidates, "model": info.OriginModelName, "group": s.Group, "health": s.Rule.Health, "mode": s.Rule.Mode, "schedule": schedule, "share_scope": shareScope, "timeout_seconds": s.Rule.TimeoutSeconds, "max_supplier_percent": s.Rule.MaxSupplierPercent, "first": s.AttemptNumber == 0 && info.RetryIndex == 0 && !info.IsChannelTest}
	data, err := common.Marshal(input)
	if err != nil {
		return nil, err
	}
	op := "acquire"
	if s.Shadow && locked == nil {
		op = "preview"
	}
	raw, err := supplierAdmission.Run(c.Request.Context(), common.RDB, []string{supplierRuntimeKey}, op, string(data)).Text()
	if err != nil && op == "acquire" {
		// Resolve ambiguous Redis replies using the same idempotency key.
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		raw, err = common.RDB.Get(ctx, "supplier-routing:attempt:"+s.ReservationID).Result()
	}
	if err != nil {
		return nil, err
	}
	if raw == "" {
		return nil, errors.New("supplier capacity exhausted or cooling down")
	}
	var selected SupplierCandidate
	if err := common.UnmarshalJsonStr(raw, &selected); err != nil {
		return nil, err
	}
	if op == "preview" {
		s.ShadowRecorded = true
		observation := model.SupplierAttempt{RequestID: s.RequestID, Attempt: 0, SupplierID: selected.SupplierID, PoolID: selected.PoolID, ChannelID: selected.ChannelID, UserID: info.UserId, Model: info.OriginModelName, GroupName: s.Group, Revision: s.Runtime.Config.Revision, Reason: "shadow:" + s.Rule.Mode, Kind: "shadow", Status: "observed", CreatedAt: time.Now().Unix(), CostStatus: "not_applicable"}
		if err := model.DB.Create(&observation).Error; err != nil {
			return nil, err
		}
		return byID[selected.ChannelID], nil
	}
	s.AttemptNumber++
	s.ReservedTokens = selected.Tokens
	s.Started = time.Now()
	s.FirstContent = time.Time{}
	s.Usage = nil
	s.Sent = false
	s.Complete = false
	s.Finished = false
	reason := s.Rule.Mode
	if s.Shadow {
		reason = "shadow:legacy"
	}
	if !s.Managed {
		reason = "legacy"
	}
	fallback := false
	for _, candidate := range candidates {
		if candidate.Priority > selected.Priority {
			fallback = true
		}
	}
	if fallback {
		reason += ":priority_fallback"
	}
	kind := "first"
	if s.AttemptNumber > 1 || info.RetryIndex > 0 {
		kind = "retry"
	}
	if info.IsChannelTest {
		kind = "probe"
	}
	s.Current = &model.SupplierAttempt{PriorityFallback: fallback, CapacityUntil: selected.Expires, RequestID: s.RequestID, Attempt: s.AttemptNumber, SupplierID: selected.SupplierID, PoolID: selected.PoolID, ChannelID: selected.ChannelID, UserID: info.UserId, Model: info.OriginModelName, GroupName: s.Group, Revision: s.Runtime.Config.Revision, Reason: reason, Kind: kind, Status: "pending", CreatedAt: time.Now().Unix(), CostStatus: "pending"}
	var cost model.ChannelCostPricing
	if err := model.DB.Where("channel_id = ? AND model_name = ?", selected.ChannelID, info.OriginModelName).First(&cost).Error; err == nil {
		if err := common.UnmarshalJsonStr(cost.ConfigJson, &cost.Config); err == nil {
			snapshot, _ := common.Marshal(cost)
			s.Current.PriceJSON = string(snapshot)
			s.Current.Currency = cost.Currency
		}
	}
	if err := model.DB.Create(s.Current).Error; err != nil {
		FinishSupplierAttempt(c, info, nil, true)
		return nil, err
	}
	return byID[selected.ChannelID], nil
}

func SupplierRoutingError(err error) *types.NewAPIError {
	return types.NewErrorWithStatusCode(err, types.ErrorCodeGetChannelFailed, 503, types.ErrOptionWithSkipRetry())
}
