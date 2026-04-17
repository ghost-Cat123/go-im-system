package dao

import (
	"errors"
	"go-im-system/apps/logic/models"
	"go-im-system/apps/pkg/db"
)

func FindUserByName(userName string) (models.User, error) {
	var user models.User
	result := db.GetDB().Where("username = ? ", userName).Find(&user)

	// 检查是否查到了记录
	if result.Error != nil {
		return user, result.Error
	}

	// 检查是否有记录被找到
	if result.RowsAffected == 0 {
		return user, errors.New("用户不存在")
	}

	return user, nil
}

func InsertUser(user *models.User) error {
	result := db.GetDB().Create(user)
	return result.Error
}
