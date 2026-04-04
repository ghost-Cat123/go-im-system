package dao

import (
	"go-im-system/apps/logic/models"
	"go-im-system/apps/pkg/db"
)

func FindUserByName(userName string) (models.User, error) {
	var user models.User
	result := db.DB.Where("username = ? ", userName).Find(&user)
	return user, result.Error
}

func InsertUser(user *models.User) error {
	result := db.DB.Create(user)
	return result.Error
}
