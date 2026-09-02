package mozia_setting

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
)

// Keep the existing option key so deployed rules continue to load.
const UserModelRedirectOptionKey = "mozia_setting.user_thinking_disabled_redirects"

// These values are only used to migrate the previous {user_id: true} format.
const (
	legacyThinkingDisabledSourceModel = "moonshotai/kimi-k3"
	legacyThinkingDisabledTargetModel = "moonshotai/kimi-k2.6"
)

type UserModelRedirect struct {
	UserId               int    `json:"user_id"`
	SourceModel          string `json:"source_model"`
	TargetModel          string `json:"target_model"`
	OnlyThinkingDisabled bool   `json:"only_thinking_disabled"`
	Seamless             bool   `json:"seamless"`
}

type persistedUserModelRedirect struct {
	UserId               int    `json:"user_id"`
	SourceModel          string `json:"source_model"`
	TargetModel          string `json:"target_model"`
	OnlyThinkingDisabled *bool  `json:"only_thinking_disabled"`
	Seamless             bool   `json:"seamless"`
}

type userModelRedirectRules struct {
	*types.RWMap[string, UserModelRedirect]
}

func redirectKey(userId int, sourceModel string) string {
	return fmt.Sprintf("%d:%s", userId, strings.TrimSpace(sourceModel))
}

func NormalizeUserModelRedirect(rule UserModelRedirect) UserModelRedirect {
	rule.SourceModel = strings.TrimSpace(rule.SourceModel)
	rule.TargetModel = strings.TrimSpace(rule.TargetModel)
	return rule
}

func ValidateUserModelRedirect(rule UserModelRedirect) error {
	rule = NormalizeUserModelRedirect(rule)
	if rule.UserId <= 0 {
		return errors.New("user_id must be greater than 0")
	}
	if rule.SourceModel == "" {
		return errors.New("source_model must not be empty")
	}
	if rule.TargetModel == "" {
		return errors.New("target_model must not be empty")
	}
	if rule.SourceModel == rule.TargetModel {
		return errors.New("source_model and target_model must be different")
	}
	return nil
}

func (rules *userModelRedirectRules) UnmarshalJSON(data []byte) error {
	persisted := make(map[string]persistedUserModelRedirect)
	if err := common.Unmarshal(data, &persisted); err == nil {
		normalized := make(map[string]UserModelRedirect, len(persisted))
		for _, stored := range persisted {
			onlyThinkingDisabled := true
			if stored.OnlyThinkingDisabled != nil {
				onlyThinkingDisabled = *stored.OnlyThinkingDisabled
			}
			rule := NormalizeUserModelRedirect(UserModelRedirect{
				UserId:               stored.UserId,
				SourceModel:          stored.SourceModel,
				TargetModel:          stored.TargetModel,
				OnlyThinkingDisabled: onlyThinkingDisabled,
				Seamless:             stored.Seamless,
			})
			if err := ValidateUserModelRedirect(rule); err != nil {
				return err
			}
			normalized[redirectKey(rule.UserId, rule.SourceModel)] = rule
		}
		rules.Clear()
		rules.AddAll(normalized)
		return nil
	}

	legacy := make(map[int]bool)
	if err := common.Unmarshal(data, &legacy); err != nil {
		return err
	}
	configured := make(map[string]UserModelRedirect, len(legacy))
	for userId, enabled := range legacy {
		if userId <= 0 || !enabled {
			continue
		}
		rule := UserModelRedirect{
			UserId:               userId,
			SourceModel:          legacyThinkingDisabledSourceModel,
			TargetModel:          legacyThinkingDisabledTargetModel,
			OnlyThinkingDisabled: true,
		}
		configured[redirectKey(userId, rule.SourceModel)] = rule
	}
	rules.Clear()
	rules.AddAll(configured)
	return nil
}

var userModelRedirectMap = &userModelRedirectRules{
	RWMap: types.NewRWMap[string, UserModelRedirect](),
}

func GetUserModelRedirect(userId int, sourceModel string) (UserModelRedirect, bool) {
	rule, ok := userModelRedirectMap.Get(redirectKey(userId, sourceModel))
	return rule, ok
}

func GetUserModelRedirects() []UserModelRedirect {
	all := userModelRedirectMap.ReadAll()
	rules := make([]UserModelRedirect, 0, len(all))
	for _, rule := range all {
		rules = append(rules, rule)
	}
	sort.Slice(rules, func(i, j int) bool {
		if rules[i].UserId != rules[j].UserId {
			return rules[i].UserId < rules[j].UserId
		}
		return rules[i].SourceModel < rules[j].SourceModel
	})
	return rules
}

func UserModelRedirects2JSONString() string {
	return userModelRedirectMap.MarshalJSONString()
}

func BuildUserModelRedirectUpsertJSON(rule UserModelRedirect) (string, error) {
	rule = NormalizeUserModelRedirect(rule)
	if err := ValidateUserModelRedirect(rule); err != nil {
		return "", err
	}
	all := userModelRedirectMap.ReadAll()
	all[redirectKey(rule.UserId, rule.SourceModel)] = rule
	data, err := common.Marshal(all)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func BuildUserModelRedirectDeleteJSON(userId int, sourceModel string) (string, error) {
	key := redirectKey(userId, sourceModel)
	all := userModelRedirectMap.ReadAll()
	if _, ok := all[key]; !ok {
		return "", errors.New("user model redirect not found")
	}
	delete(all, key)
	data, err := common.Marshal(all)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func UpdateUserModelRedirectsByJSONString(value string) error {
	return common.Unmarshal([]byte(value), userModelRedirectMap)
}
