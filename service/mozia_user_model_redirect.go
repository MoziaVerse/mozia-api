package service

import (
	"sync"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/mozia_setting"
)

var moziaUserThinkingRedirectMutationMutex sync.Mutex

func UpsertMoziaUserThinkingDisabledRedirect(rule mozia_setting.UserThinkingDisabledRedirect) error {
	moziaUserThinkingRedirectMutationMutex.Lock()
	defer moziaUserThinkingRedirectMutationMutex.Unlock()

	value, err := mozia_setting.BuildUserThinkingDisabledRedirectUpsertJSON(rule)
	if err != nil {
		return err
	}
	return model.UpdateOptionsBulk(map[string]string{
		mozia_setting.UserThinkingDisabledRedirectOptionKey: value,
	})
}

func DeleteMoziaUserThinkingDisabledRedirect(userId int, sourceModel string) error {
	moziaUserThinkingRedirectMutationMutex.Lock()
	defer moziaUserThinkingRedirectMutationMutex.Unlock()

	value, err := mozia_setting.BuildUserThinkingDisabledRedirectDeleteJSON(userId, sourceModel)
	if err != nil {
		return err
	}
	return model.UpdateOptionsBulk(map[string]string{
		mozia_setting.UserThinkingDisabledRedirectOptionKey: value,
	})
}
