package mozia_setting

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
	"github.com/QuantumNous/new-api/types"
)

const UserModelRatioOptionKey = "mozia_setting.user_model_ratios"

const (
	UserRatioScopeModel   = "model"
	UserRatioScopeChannel = "channel"
)

type UserModelRatio struct {
	UserId    int     `json:"user_id"`
	Scope     string  `json:"scope"`
	Model     string  `json:"model,omitempty"`
	ChannelId int     `json:"channel_id,omitempty"`
	Ratio     float64 `json:"ratio"`
}

type MoziaSetting struct {
	UserModelRatios *types.RWMap[string, UserModelRatio] `json:"user_model_ratios"`
}

var userModelRatioMap = types.NewRWMap[string, UserModelRatio]()

var moziaSetting = MoziaSetting{
	UserModelRatios: userModelRatioMap,
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
	if rule.Scope == UserRatioScopeChannel {
		return userChannelRatioKey(rule.UserId, rule.ChannelId)
	}
	// Keep the original key format so existing persisted model rules remain
	// addressable without a data migration.
	return userModelRatioKey(rule.UserId, rule.Model)
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
	return nil
}

func GetUserModelRatio(userId int, model string, channelIds ...int) (float64, bool) {
	rule, ok := userModelRatioMap.Get(userModelRatioKey(userId, model))
	if ok {
		return rule.Ratio, true
	}
	if len(channelIds) > 0 && channelIds[0] > 0 {
		rule, ok = userModelRatioMap.Get(userChannelRatioKey(userId, channelIds[0]))
		if ok {
			return rule.Ratio, true
		}
	}
	return 1, false
}

func GetUserModelRatios() []UserModelRatio {
	all := userModelRatioMap.ReadAll()
	rules := make([]UserModelRatio, 0, len(all))
	for _, rule := range all {
		rules = append(rules, NormalizeUserModelRatio(rule))
	}
	sort.Slice(rules, func(i, j int) bool {
		if rules[i].UserId != rules[j].UserId {
			return rules[i].UserId < rules[j].UserId
		}
		if rules[i].Scope != rules[j].Scope {
			return rules[i].Scope < rules[j].Scope
		}
		if rules[i].Scope == UserRatioScopeChannel {
			return rules[i].ChannelId < rules[j].ChannelId
		}
		return rules[i].Model < rules[j].Model
	})
	return rules
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
	data, err := common.Marshal(all)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func UpdateUserModelRatiosByJSONString(value string) error {
	return types.LoadFromJsonString(userModelRatioMap, value)
}
