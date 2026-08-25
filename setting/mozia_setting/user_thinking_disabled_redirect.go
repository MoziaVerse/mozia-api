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

type UserThinkingDisabledRedirect struct {
	UserId      int    `json:"user_id"`
	SourceModel string `json:"source_model"`
	TargetModel string `json:"target_model"`
	Enabled     bool   `json:"enabled"`
}

var userThinkingDisabledRedirectMap = types.NewRWMap[string, UserThinkingDisabledRedirect]()

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

func userThinkingDisabledRedirectKey(userId int, sourceModel string) string {
	return fmt.Sprintf("%d:%s", userId, strings.TrimSpace(sourceModel))
}

func GetUserThinkingDisabledRedirect(userId int, sourceModel string) (UserThinkingDisabledRedirect, bool) {
	rule, ok := userThinkingDisabledRedirectMap.Get(userThinkingDisabledRedirectKey(userId, sourceModel))
	if !ok || !rule.Enabled {
		return UserThinkingDisabledRedirect{}, false
	}
	return NormalizeUserThinkingDisabledRedirect(rule), true
}

func GetUserThinkingDisabledRedirects() []UserThinkingDisabledRedirect {
	all := userThinkingDisabledRedirectMap.ReadAll()
	rules := make([]UserThinkingDisabledRedirect, 0, len(all))
	for _, rule := range all {
		rules = append(rules, NormalizeUserThinkingDisabledRedirect(rule))
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
	all[userThinkingDisabledRedirectKey(rule.UserId, rule.SourceModel)] = rule
	data, err := common.Marshal(all)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func BuildUserThinkingDisabledRedirectDeleteJSON(userId int, sourceModel string) (string, error) {
	key := userThinkingDisabledRedirectKey(userId, sourceModel)
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
	return types.LoadFromJsonString(userThinkingDisabledRedirectMap, value)
}
