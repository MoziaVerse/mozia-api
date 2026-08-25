package service

import (
	"sync"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/mozia_setting"
)

var moziaUserThinkingRedirectMutationMutex sync.Mutex

func SetMoziaUserThinkingDisabledRedirect(userId int, enabled bool) error {
	moziaUserThinkingRedirectMutationMutex.Lock()
	defer moziaUserThinkingRedirectMutationMutex.Unlock()

	value, err := mozia_setting.BuildUserThinkingDisabledRedirectJSON(userId, enabled)
	if err != nil {
		return err
	}
	return model.UpdateOptionsBulk(map[string]string{
		mozia_setting.UserThinkingDisabledRedirectOptionKey: value,
	})
}
