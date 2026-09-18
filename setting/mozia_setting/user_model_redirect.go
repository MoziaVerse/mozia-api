package mozia_setting

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
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

type RouteCondition struct {
	Operator string          `json:"operator"`
	Path     string          `json:"path,omitempty"`
	Value    json.RawMessage `json:"value,omitempty"`
}

type UserModelRedirect struct {
	ID                   string           `json:"id"`
	AllUsers             bool             `json:"all_users"`
	Disabled             bool             `json:"disabled"`
	Priority             int              `json:"priority"`
	Endpoint             string           `json:"endpoint,omitempty"`
	Conditions           []RouteCondition `json:"conditions,omitempty"`
	TargetChannelId      int              `json:"target_channel_id"`
	UserId               int              `json:"user_id"`
	SourceModel          string           `json:"source_model"`
	TargetModel          string           `json:"target_model"`
	OnlyThinkingDisabled bool             `json:"only_thinking_disabled"`
	Seamless             bool             `json:"seamless"`
}

type persistedUserModelRedirect struct {
	UserModelRedirect
	OnlyThinkingDisabled *bool `json:"only_thinking_disabled"`
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
	rule.Endpoint = strings.TrimSpace(rule.Endpoint)
	if rule.ID == "" {
		rule.ID = redirectKey(rule.UserId, rule.SourceModel)
	}
	return rule
}

func ValidateUserModelRedirect(rule UserModelRedirect) error {
	rule = NormalizeUserModelRedirect(rule)
	if rule.UserId < 0 || (rule.UserId == 0 && !rule.AllUsers) || (rule.UserId != 0 && rule.AllUsers) {
		return errors.New("select either all users or one valid user")
	}
	if rule.SourceModel == "" {
		return errors.New("source_model must not be empty")
	}
	if rule.TargetModel == "" {
		return errors.New("target_model must not be empty")
	}
	if rule.SourceModel == rule.TargetModel && rule.TargetChannelId == 0 {
		return errors.New("select a different target model or a specific channel")
	}
	if rule.TargetChannelId < 0 || len(rule.ID) > 512 || strings.TrimSpace(rule.ID) != rule.ID ||
		len(rule.SourceModel) > 191 || len(rule.TargetModel) > 191 || rule.Priority < 0 || rule.Priority > 10000 {
		return errors.New("invalid route ID, model, channel or priority")
	}
	if rule.Endpoint != "" && !slices.Contains([]string{"/v1/chat/completions", "/v1/messages", "/v1/responses", "/pg/chat/completions"}, rule.Endpoint) {
		return errors.New("unsupported conditional routing endpoint")
	}
	if len(rule.Conditions) > 8 {
		return errors.New("at most 8 conditions are allowed")
	}
	for _, condition := range rule.Conditions {
		if err := ValidateRouteCondition(condition); err != nil {
			return err
		}
	}
	return nil
}

func (rules *userModelRedirectRules) UnmarshalJSON(data []byte) error {
	persisted := make(map[string]persistedUserModelRedirect)
	if err := common.Unmarshal(data, &persisted); err == nil {
		if len(persisted) > 1000 {
			return errors.New("at most 1000 routing rules are allowed")
		}
		normalized := make(map[string]UserModelRedirect, len(persisted))
		for _, stored := range persisted {
			onlyThinkingDisabled := stored.ID == ""
			if stored.OnlyThinkingDisabled != nil {
				onlyThinkingDisabled = *stored.OnlyThinkingDisabled
			}
			rule := NormalizeUserModelRedirect(stored.UserModelRedirect)
			rule.OnlyThinkingDisabled = onlyThinkingDisabled
			if err := ValidateUserModelRedirect(rule); err != nil {
				return err
			}
			if _, exists := normalized[rule.ID]; exists {
				return errors.New("duplicate route ID")
			}
			normalized[rule.ID] = rule
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
		rule = NormalizeUserModelRedirect(rule)
		configured[rule.ID] = rule
	}
	rules.Clear()
	rules.AddAll(configured)
	return nil
}

var userModelRedirectMap = &userModelRedirectRules{
	RWMap: types.NewRWMap[string, UserModelRedirect](),
}

func GetUserModelRedirects() []UserModelRedirect {
	all := userModelRedirectMap.ReadAll()
	rules := make([]UserModelRedirect, 0, len(all))
	for _, rule := range all {
		rules = append(rules, rule)
	}
	sort.Slice(rules, func(i, j int) bool {
		if rules[i].Priority != rules[j].Priority {
			return rules[i].Priority > rules[j].Priority
		}
		if rules[i].AllUsers != rules[j].AllUsers {
			return !rules[i].AllUsers
		}
		if rules[i].UserId != rules[j].UserId {
			return rules[i].UserId < rules[j].UserId
		}
		if rules[i].SourceModel != rules[j].SourceModel {
			return rules[i].SourceModel < rules[j].SourceModel
		}
		return rules[i].ID < rules[j].ID
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
	if len(all) >= 1000 {
		if _, exists := all[rule.ID]; !exists {
			return "", errors.New("at most 1000 routing rules are allowed")
		}
	}
	all[rule.ID] = rule
	data, err := common.Marshal(all)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func BuildUserModelRedirectDeleteJSON(userId int, sourceModel string, ids ...string) (string, error) {
	key := redirectKey(userId, sourceModel)
	if len(ids) > 0 && ids[0] != "" {
		key = ids[0]
	}
	all := userModelRedirectMap.ReadAll()
	if rule, ok := all[key]; !ok || rule.UserId != userId || rule.SourceModel != sourceModel {
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
