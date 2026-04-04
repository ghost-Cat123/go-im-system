package models

import "time"

type ScheduledMessages struct {
	SchMsgId          int64     `gorm:"primaryKey;autoIncrement;column:msg_id"` // 自增主键
	CreatorId         int64     `gorm:"index;column:creator_id"`                // 改为 int64
	ReceiverId        int64     `gorm:"index;column:receiver_id"`               // 改为 int64
	Content           string    `gorm:"type:text;column:content"`
	ScheduledSendTime time.Time `gorm:"type:datetime;not null;column:scheduled_send_time"` // 默认未读
	Status            int       `gorm:"type:tinyint;default:0;column:status"`
	ActualSendTime    time.Time `gorm:"type:datetime;default:null;column:actual_send_time"`
	FailReason        string    `gorm:"type:varchar(255);default:null;column:fail_reason"`
	CreateTime        time.Time `gorm:"autoCreateTime;column:create_time"` // 自动填入当前时间
	UpdateTime        time.Time `gorm:"autoCreateTime;column:update_time"`
}
