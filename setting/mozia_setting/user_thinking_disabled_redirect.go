package mozia_setting

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
)

const UserThinkingDisabledRedirectOptionKey = "mozia_setting.user_thinking_disabled_redirects"

// These values are only used to migrate the previous {user_id: true} format.
const (
	legacyThinkingDisabledSourceModel = "moonshotai/kimi-k3"
	legacyThinkingDisabledTargetModel = "moonshotai/kimi-k2.6"
)

type UserThinkingDisabledRedirect struct {
	UserId      int    `json:"user_id"`
	SourceModel string `json:"source_model"`
	TargetModel string `json:"target_model"`
}

type persistedUserThinkingDisabledRedirect struct {
	UserId      int    `json:"user_id"`
	SourceModel string `json:"source_model"`
	TargetModel string `json:"target_model"`
	Enabled     *bool  `json:"enabled,omitempty"`
}

type userThinkingDisabledRedirectRules struct {
	*types.RWMap[string, UserThinkingDisabledRedirect]
}

func redirectKey(userId int, sourceModel string) string {
	return fmt.Sprintf("%d:%s", userId, strings.TrimSpace(sourceModel))
}

func NormalizeUserThinkingDisabledRedirect(rule UserThinkingDisabledRedirect) UserThinkingDisabledRedirect {
	rule.SourceModel = strings.TrimSpace(rule.SourceModel)
	rule.TargetModel = strings.TrimSpace(rule.TargetModel)
	return rule
}

func ValidateUserThinkingDisabledRedirect(rule UserThinkingDisabledRedirect) error {
	rule = NormalizeUserThinkingDisabledRedirect(rule)
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

func (rules *userThinkingDisabledRedirectRules) UnmarshalJSON(data []byte) error {
	persisted := make(map[string]persistedUserThinkingDisabledRedirect)
	if err := common.Unmarshal(data, &persisted); err == nil {
		normalized := make(map[string]UserThinkingDisabledRedirect, len(persisted))
		for _, stored := range persisted {
			if stored.Enabled != nil && !*stored.Enabled {
				continue
			}
			rule := UserThinkingDisabledRedirect{
				UserId:      stored.UserId,
				SourceModel: stored.SourceModel,
				TargetModel: stored.TargetModel,
			}
			rule = NormalizeUserThinkingDisabledRedirect(rule)
			if err := ValidateUserThinkingDisabledRedirect(rule); err != nil {
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
	configured := make(map[string]UserThinkingDisabledRedirect, len(legacy))
	for userId, enabled := range legacy {
		if userId <= 0 || !enabled {
			continue
		}
		rule := UserThinkingDisabledRedirect{
			UserId:      userId,
			SourceModel: legacyThinkingDisabledSourceModel,
			TargetModel: legacyThinkingDisabledTargetModel,
		}
		configured[redirectKey(userId, rule.SourceModel)] = rule
	}
	rules.Clear()
	rules.AddAll(configured)
	return nil
}

var userThinkingDisabledRedirectMap = &userThinkingDisabledRedirectRules{
	RWMap: types.NewRWMap[string, UserThinkingDisabledRedirect](),
}

func GetUserThinkingDisabledRedirect(userId int, sourceModel string) (UserThinkingDisabledRedirect, bool) {
	rule, ok := userThinkingDisabledRedirectMap.Get(redirectKey(userId, sourceModel))
	return rule, ok
}

func GetUserThinkingDisabledRedirects() []UserThinkingDisabledRedirect {
	all := userThinkingDisabledRedirectMap.ReadAll()
	rules := make([]UserThinkingDisabledRedirect, 0, len(all))
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

func UserThinkingDisabledRedirects2JSONString() string {
	return userThinkingDisabledRedirectMap.MarshalJSONString()
}

func BuildUserThinkingDisabledRedirectUpsertJSON(rule UserThinkingDisabledRedirect) (string, error) {
	rule = NormalizeUserThinkingDisabledRedirect(rule)
	if err := ValidateUserThinkingDisabledRedirect(rule); err != nil {
		return "", err
	}
	all := userThinkingDisabledRedirectMap.ReadAll()
	all[redirectKey(rule.UserId, rule.SourceModel)] = rule
	data, err := common.Marshal(all)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func BuildUserThinkingDisabledRedirectDeleteJSON(userId int, sourceModel string) (string, error) {
	key := redirectKey(userId, sourceModel)
	all := userThinkingDisabledRedirectMap.ReadAll()
	if _, ok := all[key]; !ok {
		return "", errors.New("user thinking redirect not found")
	}
	delete(all, key)
	data, err := common.Marshal(all)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func UpdateUserThinkingDisabledRedirectsByJSONString(value string) error {
	return common.Unmarshal([]byte(value), userThinkingDisabledRedirectMap)
}
