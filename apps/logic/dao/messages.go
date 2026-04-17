package dao

import (
	"go-im-system/apps/logic/models"
	"go-im-system/apps/pkg/db"
	"time"
)

func InsertMessage(message *models.Messages) error {
	err := db.GetDB().Create(message).Error
	return err
}

func GetUnreadMessages(receiverId int64) ([]models.Messages, error) {
	var messages []models.Messages
	result := db.GetDB().Where("receiver_id = ? AND is_read = ?", receiverId, false).Find(&messages)
	return messages, result.Error
}

func MarkMessagesAsRead(receiverId int64) error {
	result := db.GetDB().Model(&models.Messages{}).
		Where("receiver_id = ? AND is_read = ?", receiverId, false).
		Update("is_read", true)
	return result.Error
}

func MarkMessageAsRead(msgId int64) error {
	result := db.GetDB().Model(&models.Messages{}).
		Where("msg_id = ? AND is_read = ?", msgId, false).
		Update("is_read", true)
	return result.Error
}

func GetHistoryBetweenUsers(uid1, uid2 int64, startTime, endTime time.Time, limit int) ([]models.Messages, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var messages []models.Messages
	// 构建 SQL
	sql := `
		SELECT *
		FROM messages 
		WHERE 
		    ((sender_id = ? AND receiver_id = ?) OR (sender_id = ? AND receiver_id = ?))
		    AND create_time >= ? 
		    AND create_time <= ?
		ORDER BY create_time DESC 
		LIMIT ?
	`
	result := db.GetDB().Raw(sql, uid1, uid2, uid2, uid1, startTime, endTime, limit).Scan(&messages)
	return messages, result.Error
}
