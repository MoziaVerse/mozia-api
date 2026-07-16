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

type UserModelRatio struct {
	UserId int     `json:"user_id"`
	Model  string  `json:"model"`
	Ratio  float64 `json:"ratio"`
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

func ValidateUserModelRatio(rule UserModelRatio) error {
	if rule.UserId <= 0 {
		return errors.New("user_id must be greater than 0")
	}
	if strings.TrimSpace(rule.Model) == "" {
		return errors.New("model must not be empty")
	}
	if rule.Ratio <= 0 || math.IsNaN(rule.Ratio) || math.IsInf(rule.Ratio, 0) {
		return errors.New("ratio must be a finite number greater than 0")
	}
	return nil
}

func GetUserModelRatio(userId int, model string) (float64, bool) {
	rule, ok := userModelRatioMap.Get(userModelRatioKey(userId, model))
	if !ok {
		return 1, false
	}
	return rule.Ratio, true
}

func GetUserModelRatios() []UserModelRatio {
	all := userModelRatioMap.ReadAll()
	rules := make([]UserModelRatio, 0, len(all))
	for _, rule := range all {
		rules = append(rules, rule)
	}
	sort.Slice(rules, func(i, j int) bool {
		if rules[i].UserId != rules[j].UserId {
			return rules[i].UserId < rules[j].UserId
		}
		return rules[i].Model < rules[j].Model
	})
	return rules
}

func UserModelRatios2JSONString() string {
	return userModelRatioMap.MarshalJSONString()
}

func BuildUserModelRatioUpsertJSON(rule UserModelRatio) (string, error) {
	rule.Model = strings.TrimSpace(rule.Model)
	if err := ValidateUserModelRatio(rule); err != nil {
		return "", err
	}
	all := userModelRatioMap.ReadAll()
	all[userModelRatioKey(rule.UserId, rule.Model)] = rule
	data, err := common.Marshal(all)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func BuildUserModelRatioDeleteJSON(userId int, model string) (string, error) {
	model = strings.TrimSpace(model)
	if userId <= 0 {
		return "", errors.New("user_id must be greater than 0")
	}
	if model == "" {
		return "", errors.New("model must not be empty")
	}
	all := userModelRatioMap.ReadAll()
	key := userModelRatioKey(userId, model)
	if _, ok := all[key]; !ok {
		return "", errors.New("user model ratio not found")
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
