package models

import (
	"go-im-system/apps/pkg/utils"
	"time"
)

// 发送状态（实习项目单级确认：不区分「投递 ACK」与「已读 ACK」等多级）
const (
	SendStatusUnsent          int8 = 0 // 未发送：已落库，对端尚未经本网关 WS 成功下发
	SendStatusSentUnconfirmed int8 = 1 // 发送未确认：已写入对端 WebSocket，接收方尚未确认
	SendStatusSentConfirmed   int8 = 2 // 发送已确认：接收方已 ack（与 is_read 一并更新）
)

type Messages struct {
	// MsgId 全局唯一（雪花），非自增；由网关或 Logic 在插入前赋值
	MsgId      int64     `gorm:"primaryKey;column:msg_id"`
	SeqId      int64     `gorm:"index;column:seq_id"` // 会话内序号，不同会话可重复
	SenderId   int64     `gorm:"index;column:sender_id"`
	ReceiverId int64     `gorm:"index;column:receiver_id"`
	Content    string    `gorm:"type:text;column:content"`
	IsRead     bool      `gorm:"default:false;column:is_read"`
	SendStatus int8      `gorm:"column:send_status;default:0"` // 见 SendStatus* 常量
	CreateTime time.Time `gorm:"autoCreateTime;column:create_time"`
}

// NewMessages 创建一条待插入消息（分配全局 MsgId，SeqId 默认 0；单聊由网关覆盖 SeqId）
func NewMessages(senderId int64, receiverId int64, content string, isRead bool) *Messages {
	return &Messages{
		MsgId:      utils.GetSnowflake().Generate(),
		SeqId:      0,
		SenderId:   senderId,
		ReceiverId: receiverId,
		Content:    content,
		IsRead:     isRead,
		SendStatus: SendStatusUnsent,
	}
}
