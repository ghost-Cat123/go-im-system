package dao

import (
	"go-im-system/apps/logic/models"
	"go-im-system/apps/pkg/db"
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
		Updates(map[string]interface{}{
			"is_read":     true,
			"send_status": models.SendStatusSentConfirmed,
		})
	return result.Error
}

func MarkMessageAsRead(msgId int64) error {
	// 已读即视为「发送已确认」（单级业务确认，不单独做投递 ACK）
	result := db.GetDB().Model(&models.Messages{}).
		Where("msg_id = ? AND is_read = ?", msgId, false).
		Updates(map[string]interface{}{
			"is_read":     true,
			"send_status": models.SendStatusSentConfirmed,
		})
	return result.Error
}

// MarkSendTransportDelivered 网关推送成功：仅允许 0 -> 1
func MarkSendTransportDelivered(msgId int64) error {
	result := db.GetDB().Model(&models.Messages{}).
		Where("msg_id = ? AND send_status = ?", msgId, models.SendStatusUnsent).
		Update("send_status", models.SendStatusSentUnconfirmed)
	return result.Error
}
