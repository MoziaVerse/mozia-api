package mozia_setting

import (
	"errors"
	"sort"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/types"
)

const (
	UserThinkingDisabledRedirectOptionKey = "mozia_setting.user_thinking_disabled_redirects"
	ThinkingDisabledSourceModel           = "moonshotai/kimi-k3"
	ThinkingDisabledTargetModel           = "moonshotai/kimi-k2.6"
)

type userThinkingDisabledRedirectUsers struct {
	*types.RWMap[int, bool]
}

func (users *userThinkingDisabledRedirectUsers) UnmarshalJSON(data []byte) error {
	current := make(map[int]bool)
	if err := common.Unmarshal(data, &current); err == nil {
		users.Clear()
		users.AddAll(current)
		return nil
	}

	legacy := make(map[string]struct {
		UserId  int  `json:"user_id"`
		Enabled bool `json:"enabled"`
	})
	if err := common.Unmarshal(data, &legacy); err != nil {
		return err
	}
	users.Clear()
	for _, rule := range legacy {
		if rule.UserId > 0 && rule.Enabled {
			users.Set(rule.UserId, true)
		}
	}
	return nil
}

var userThinkingDisabledRedirectMap = &userThinkingDisabledRedirectUsers{
	RWMap: types.NewRWMap[int, bool](),
}

func IsUserThinkingDisabledRedirectEnabled(userId int) bool {
	enabled, ok := userThinkingDisabledRedirectMap.Get(userId)
	return ok && enabled
}

func GetUserThinkingDisabledRedirectUserIds() []int {
	all := userThinkingDisabledRedirectMap.ReadAll()
	userIds := make([]int, 0, len(all))
	for userId, enabled := range all {
		if enabled {
			userIds = append(userIds, userId)
		}
	}
	sort.Ints(userIds)
	return userIds
}

func BuildUserThinkingDisabledRedirectJSON(userId int, enabled bool) (string, error) {
	if userId <= 0 {
		return "", errors.New("user_id must be greater than 0")
	}
	all := userThinkingDisabledRedirectMap.ReadAll()
	if enabled {
		all[userId] = true
	} else {
		if _, ok := all[userId]; !ok {
			return "", errors.New("user thinking redirect not found")
		}
		delete(all, userId)
	}
	data, err := common.Marshal(all)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func UpdateUserThinkingDisabledRedirectsByJSONString(value string) error {
	return common.Unmarshal([]byte(value), userThinkingDisabledRedirectMap)
}
