package model

import (
	"errors"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

type UserSSO struct {
	Id     int    `json:"id" gorm:"primaryKey;autoIncrement"`
	SSOSub string `json:"sso_sub" gorm:"type:varchar(255);uniqueIndex;not null"`
	UserId int    `json:"user_id" gorm:"index;not null"`
}

func GetUserSSOBySub(ssoSub string) (*UserSSO, error) {
	if ssoSub == "" {
		return nil, errors.New("sso_sub is empty")
	}
	var userSSO UserSSO
	err := DB.Where("sso_sub = ?", ssoSub).First(&userSSO).Error
	if err != nil {
		return nil, err
	}
	return &userSSO, nil
}

func CreateUserSSO(ssoSub string, userId int) error {
	userSSO := UserSSO{
		SSOSub: ssoSub,
		UserId: userId,
	}
	return DB.Create(&userSSO).Error
}

func createSSOUser(ssoSub string) (*User, error) {
	username := "mozia_" + ssoSub
	if len(username) > UserNameMaxLength {
		username = username[:UserNameMaxLength]
	}

	password := common.GetRandomString(16)
	user := &User{
		Username:    username,
		Password:    password,
		DisplayName: username,
		Role:        common.RoleCommonUser,
		Status:      common.UserStatusEnabled,
		Quota:       common.QuotaForNewUser,
	}

	randI := common.GetRandomInt(4)
	key, err := common.GenerateRandomKey(29 + randI)
	if err != nil {
		common.SysLog("failed to generate access token: " + err.Error())
		return nil, err
	}
	user.SetAccessToken(key)

	err = user.Insert(0)
	if err != nil {
		return nil, err
	}

	return user, nil
}

func createDefaultTokenForUser(userId int) error {
	key, err := common.GenerateKey()
	if err != nil {
		return err
	}

	token := &Token{
		UserId:         userId,
		Name:           "default",
		Key:            key,
		CreatedTime:    common.GetTimestamp(),
		AccessedTime:   common.GetTimestamp(),
		ExpiredTime:    -1,
		UnlimitedQuota: true,
		Group:          "auto",
	}

	return token.Insert()
}

func GetOrCreateSSOUser(ssoSub string) (*UserSSO, error) {
	userSSO, err := GetUserSSOBySub(ssoSub)
	if err == nil {
		return userSSO, nil
	}

	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	user, err := createSSOUser(ssoSub)
	if err != nil {
		return nil, err
	}

	err = createDefaultTokenForUser(user.Id)
	if err != nil {
		common.SysError("failed to create default token for SSO user: " + err.Error())
	}

	userSSO = &UserSSO{
		SSOSub: ssoSub,
		UserId: user.Id,
	}
	err = CreateUserSSO(ssoSub, user.Id)
	if err != nil {
		return nil, err
	}

	return userSSO, nil
}
