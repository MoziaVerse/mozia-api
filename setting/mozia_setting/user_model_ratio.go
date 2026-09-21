package mozia_setting

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/types"
)

const UserModelRatioOptionKey = "mozia_setting.user_model_ratios"

const (
	UserRatioScopeModel   = "model"
	UserRatioScopeChannel = "channel"
)

// UserModelRatio 是「用户 × 模型(或渠道)」的计费倍率规则。
//
// 同一 (user, scope, target) 允许多条规则并存，以 Source 区分来源（模型月卡、
// 场景包、手工……）；计费时取所有未过期规则中的最低倍率。ExpiresAt 为 0 表示
// 永不过期；过期规则在读取时被忽略，并在下一次写入时被清理——网关自己就是
// 折扣有效期的事实源，不再依赖调用方按时撤销。
//
// 兼容：没有 Source 的旧规则键格式不变（`<user>:<model>`），行为完全一致。
type UserModelRatio struct {
	UserId    int     `json:"user_id"`
	Scope     string  `json:"scope"`
	Model     string  `json:"model,omitempty"`
	ChannelId int     `json:"channel_id,omitempty"`
	Ratio     float64 `json:"ratio"`
	// 规则来源标识（小写，如 model_card / scenario_pack）。空 = 手工/旧规则。
	Source string `json:"source,omitempty"`
	// 过期时间（unix 秒），0 = 永不过期。
	ExpiresAt int64 `json:"expires_at,omitempty"`
}

// Expired 报告规则在 now（unix 秒）是否已过期。
func (r UserModelRatio) Expired(now int64) bool {
	return r.ExpiresAt > 0 && r.ExpiresAt <= now
}

// 过期规则保留多久后从持久化里清掉（写入时 GC）。留一段时间便于审计/排障。
const expiredRuleRetention = 7 * 24 * time.Hour

type MoziaSetting struct {
	UserModelRatios    *types.RWMap[string, UserModelRatio] `json:"user_model_ratios"`
	UserModelRedirects *userModelRedirectRules              `json:"user_thinking_disabled_redirects"`
}

var userModelRatioMap = types.NewRWMap[string, UserModelRatio]()

var moziaSetting = MoziaSetting{
	UserModelRatios:    userModelRatioMap,
	UserModelRedirects: userModelRedirectMap,
}

func init() {
	config.GlobalConfig.Register("mozia_setting", &moziaSetting)
}

func userModelRatioKey(userId int, model string) string {
	return fmt.Sprintf("%d:%s", userId, model)
}

func userChannelRatioKey(userId int, channelId int) string {
	return fmt.Sprintf("channel:%d:%d", userId, channelId)
}

func NormalizeUserModelRatio(rule UserModelRatio) UserModelRatio {
	rule.Scope = strings.ToLower(strings.TrimSpace(rule.Scope))
	rule.Model = strings.TrimSpace(rule.Model)
	rule.Source = strings.ToLower(strings.TrimSpace(rule.Source))
	if rule.ExpiresAt < 0 {
		rule.ExpiresAt = 0
	}
	if rule.Scope == "" {
		// Rules persisted before scope support were all model rules.
		rule.Scope = UserRatioScopeModel
	}
	if rule.Scope == UserRatioScopeModel {
		rule.ChannelId = 0
	} else if rule.Scope == UserRatioScopeChannel {
		rule.Model = ""
	}
	return rule
}

func userRatioKey(rule UserModelRatio) string {
	var key string
	if rule.Scope == UserRatioScopeChannel {
		key = userChannelRatioKey(rule.UserId, rule.ChannelId)
	} else {
		// Keep the original key format so existing persisted model rules remain
		// addressable without a data migration.
		key = userModelRatioKey(rule.UserId, rule.Model)
	}
	// 带来源的规则在键上追加 `#<source>`，与无来源的旧规则并存互不覆盖。
	if rule.Source != "" {
		key += "#" + rule.Source
	}
	return key
}

func ValidateUserModelRatio(rule UserModelRatio) error {
	rule = NormalizeUserModelRatio(rule)
	if rule.UserId <= 0 {
		return errors.New("user_id must be greater than 0")
	}
	switch rule.Scope {
	case UserRatioScopeModel:
		if rule.Model == "" {
			return errors.New("model must not be empty for model scope")
		}
	case UserRatioScopeChannel:
		if rule.ChannelId <= 0 {
			return errors.New("channel_id must be greater than 0 for channel scope")
		}
	default:
		return errors.New("scope must be model or channel")
	}
	if rule.Ratio <= 0 || math.IsNaN(rule.Ratio) || math.IsInf(rule.Ratio, 0) {
		return errors.New("ratio must be a finite number greater than 0")
	}
	if strings.ContainsAny(rule.Source, "#: \t\n") {
		return errors.New("source must not contain '#', ':' or whitespace")
	}
	return nil
}

// GetUserModelRatio 取该用户在该模型上的生效倍率：模型规则优先于渠道规则；
// 同一目标多条来源并存时取未过期规则中的最低值。
//
// 实现是对规则表整体扫描：规则总量在两位数量级（生产 7 条），且 ReadAll 只是
// 一次 map 拷贝；option 同步走 RWMap.UnmarshalJSON 不经本包，因此不维护二级
// 索引，避免失步。
func GetUserModelRatio(userId int, model string, channelIds ...int) (float64, bool) {
	now := time.Now().Unix()
	channelId := 0
	if len(channelIds) > 0 {
		channelId = channelIds[0]
	}
	modelRatio, modelOk := 1.0, false
	channelRatio, channelOk := 1.0, false
	for _, raw := range userModelRatioMap.ReadAll() {
		rule := NormalizeUserModelRatio(raw)
		if rule.UserId != userId || rule.Expired(now) {
			continue
		}
		switch rule.Scope {
		case UserRatioScopeModel:
			if rule.Model != model {
				continue
			}
			if !modelOk || rule.Ratio < modelRatio {
				modelRatio, modelOk = rule.Ratio, true
			}
		case UserRatioScopeChannel:
			if channelId <= 0 || rule.ChannelId != channelId {
				continue
			}
			if !channelOk || rule.Ratio < channelRatio {
				channelRatio, channelOk = rule.Ratio, true
			}
		}
	}
	if modelOk {
		return modelRatio, true
	}
	if channelOk {
		return channelRatio, true
	}
	return 1, false
}

// GetUserModelRatios 列出全部**未过期**规则（过期规则已不参与计费，对调用方
// 等价于不存在；持久化里的残留由写入时 GC 清理）。
func GetUserModelRatios() []UserModelRatio {
	now := time.Now().Unix()
	all := userModelRatioMap.ReadAll()
	rules := make([]UserModelRatio, 0, len(all))
	for _, raw := range all {
		rule := NormalizeUserModelRatio(raw)
		if rule.Expired(now) {
			continue
		}
		rules = append(rules, rule)
	}
	sort.Slice(rules, func(i, j int) bool {
		if rules[i].UserId != rules[j].UserId {
			return rules[i].UserId < rules[j].UserId
		}
		if rules[i].Scope != rules[j].Scope {
			return rules[i].Scope < rules[j].Scope
		}
		if rules[i].Scope == UserRatioScopeChannel {
			if rules[i].ChannelId != rules[j].ChannelId {
				return rules[i].ChannelId < rules[j].ChannelId
			}
		} else if rules[i].Model != rules[j].Model {
			return rules[i].Model < rules[j].Model
		}
		return rules[i].Source < rules[j].Source
	})
	return rules
}

// gcExpiredRules 从持久化快照里移除过期超过保留期的规则。
func gcExpiredRules(all map[string]UserModelRatio) {
	cutoff := time.Now().Add(-expiredRuleRetention).Unix()
	for key, rule := range all {
		if rule.ExpiresAt > 0 && rule.ExpiresAt <= cutoff {
			delete(all, key)
		}
	}
}

func UserModelRatios2JSONString() string {
	return userModelRatioMap.MarshalJSONString()
}

func BuildUserModelRatioUpsertJSON(rule UserModelRatio) (string, error) {
	rule = NormalizeUserModelRatio(rule)
	if err := ValidateUserModelRatio(rule); err != nil {
		return "", err
	}
	all := userModelRatioMap.ReadAll()
	gcExpiredRules(all)
	all[userRatioKey(rule)] = rule
	data, err := common.Marshal(all)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func BuildUserModelRatioDeleteJSON(userId int, model string) (string, error) {
	return BuildUserRatioDeleteJSON(UserModelRatio{
		UserId: userId,
		Scope:  UserRatioScopeModel,
		Model:  model,
		Ratio:  1,
	})
}

func BuildUserRatioDeleteJSON(rule UserModelRatio) (string, error) {
	rule = NormalizeUserModelRatio(rule)
	if err := ValidateUserModelRatio(rule); err != nil {
		return "", err
	}
	all := userModelRatioMap.ReadAll()
	key := userRatioKey(rule)
	if _, ok := all[key]; !ok {
		return "", errors.New("user ratio not found")
	}
	delete(all, key)
	gcExpiredRules(all)
	data, err := common.Marshal(all)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func UpdateUserModelRatiosByJSONString(value string) error {
	return types.LoadFromJsonString(userModelRatioMap, value)
}
