package dao

import (
	"go-im-system/apps/agent/models"
	"go-im-system/apps/pkg/db"
	"time"
)

func InsertMessage(message *models.Messages) error {
	err := db.GetDB().Create(message).Error
	return err
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
