package service

import (
	"sync"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/mozia_setting"
)

var moziaUserThinkingRedirectMutationMutex sync.Mutex

func UpsertMoziaUserThinkingDisabledRedirect(userId int) error {
	moziaUserThinkingRedirectMutationMutex.Lock()
	defer moziaUserThinkingRedirectMutationMutex.Unlock()

	value, err := mozia_setting.BuildUserThinkingDisabledRedirectUpsertJSON(userId)
	if err != nil {
		return err
	}
	return model.UpdateOptionsBulk(map[string]string{
		mozia_setting.UserThinkingDisabledRedirectOptionKey: value,
	})
}

func DeleteMoziaUserThinkingDisabledRedirect(userId int) error {
	moziaUserThinkingRedirectMutationMutex.Lock()
	defer moziaUserThinkingRedirectMutationMutex.Unlock()

	value, err := mozia_setting.BuildUserThinkingDisabledRedirectDeleteJSON(userId)
	if err != nil {
		return err
	}
	return model.UpdateOptionsBulk(map[string]string{
		mozia_setting.UserThinkingDisabledRedirectOptionKey: value,
	})
}
