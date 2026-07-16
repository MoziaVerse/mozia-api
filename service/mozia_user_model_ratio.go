package service

import (
	"sync"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/mozia_setting"
)

var moziaUserModelRatioMutationMutex sync.Mutex

func UpsertMoziaUserModelRatio(rule mozia_setting.UserModelRatio) error {
	moziaUserModelRatioMutationMutex.Lock()
	defer moziaUserModelRatioMutationMutex.Unlock()

	value, err := mozia_setting.BuildUserModelRatioUpsertJSON(rule)
	if err != nil {
		return err
	}
	return model.UpdateOptionsBulk(map[string]string{
		mozia_setting.UserModelRatioOptionKey: value,
	})
}

func DeleteMoziaUserModelRatio(userId int, modelName string) error {
	moziaUserModelRatioMutationMutex.Lock()
	defer moziaUserModelRatioMutationMutex.Unlock()

	value, err := mozia_setting.BuildUserModelRatioDeleteJSON(userId, modelName)
	if err != nil {
		return err
	}
	return model.UpdateOptionsBulk(map[string]string{
		mozia_setting.UserModelRatioOptionKey: value,
	})
}
