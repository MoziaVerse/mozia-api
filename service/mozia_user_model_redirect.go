package service

import (
	"sync"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/mozia_setting"
)

var moziaUserModelRedirectMutationMutex sync.Mutex

func UpsertMoziaUserModelRedirect(rule mozia_setting.UserModelRedirect) error {
	moziaUserModelRedirectMutationMutex.Lock()
	defer moziaUserModelRedirectMutationMutex.Unlock()

	value, err := mozia_setting.BuildUserModelRedirectUpsertJSON(rule)
	if err != nil {
		return err
	}
	return model.UpdateOptionsBulk(map[string]string{
		mozia_setting.UserModelRedirectOptionKey: value,
	})
}

func DeleteMoziaUserModelRedirect(userId int, sourceModel string) error {
	moziaUserModelRedirectMutationMutex.Lock()
	defer moziaUserModelRedirectMutationMutex.Unlock()

	value, err := mozia_setting.BuildUserModelRedirectDeleteJSON(userId, sourceModel)
	if err != nil {
		return err
	}
	return model.UpdateOptionsBulk(map[string]string{
		mozia_setting.UserModelRedirectOptionKey: value,
	})
}
