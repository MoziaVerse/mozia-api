package service

import (
	"errors"
	"slices"
	"sync"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/mozia_setting"
)

var moziaUserModelRedirectMutationMutex sync.Mutex

func UpsertMoziaUserModelRedirect(rule mozia_setting.UserModelRedirect) error {
	moziaUserModelRedirectMutationMutex.Lock()
	defer moziaUserModelRedirectMutationMutex.Unlock()

	rule = mozia_setting.NormalizeUserModelRedirect(rule)
	if rule.TargetChannelId > 0 {
		channel, err := model.GetChannelById(rule.TargetChannelId, false)
		if err != nil {
			return errors.New("target channel does not exist")
		}
		if !slices.Contains(channel.GetModels(), rule.TargetModel) {
			return errors.New("target model is not configured on the selected channel")
		}
	}
	value, err := mozia_setting.BuildUserModelRedirectUpsertJSON(rule)
	if err != nil {
		return err
	}
	return model.UpdateOptionsBulk(map[string]string{
		mozia_setting.UserModelRedirectOptionKey: value,
	})
}

func DeleteMoziaUserModelRedirect(userId int, sourceModel string, ids ...string) error {
	moziaUserModelRedirectMutationMutex.Lock()
	defer moziaUserModelRedirectMutationMutex.Unlock()

	value, err := mozia_setting.BuildUserModelRedirectDeleteJSON(userId, sourceModel, ids...)
	if err != nil {
		return err
	}
	return model.UpdateOptionsBulk(map[string]string{
		mozia_setting.UserModelRedirectOptionKey: value,
	})
}
