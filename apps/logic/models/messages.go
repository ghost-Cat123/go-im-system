package models

import "time"

type Messages struct {
	MsgId      int64     `gorm:"primaryKey;autoIncrement;column:msg_id"` // 自增主键
	SenderId   int64     `gorm:"index;column:sender_id"`                 // 改为 int64
	ReceiverId int64     `gorm:"index;column:receiver_id"`               // 改为 int64
	Content    string    `gorm:"type:text;column:content"`
	IsRead     bool      `gorm:"default:false;column:is_read"`      // 默认未读
	CreateTime time.Time `gorm:"autoCreateTime;column:create_time"` // 自动填入当前时间
}

func NewMessages(senderId int64, receiverId int64, content string, isRead bool) *Messages {
	return &Messages{SenderId: senderId, ReceiverId: receiverId, Content: content, IsRead: isRead}
}
