package ws

import (
	"GeeRPC/xclient"
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"go-im-system/apps/gateway/rpcclient"
	"go-im-system/apps/pkg/logger"
	"go-im-system/apps/pkg/proto/pb_msg"
	"log"
)

// NotifyDeliveredRPC 通知 Logic：对端 WS 已写入，send_status 0→1
func NotifyDeliveredRPC(ctx context.Context, senderID, msgID int64) {
	args := &pb_msg.NotifyDeliveredArgs{MsgId: msgID}
	reply := &pb_msg.NotifyDeliveredReply{}
	rk := xclient.WithRoutingKey(ctx, strconv.FormatInt(senderID, 10))
	if err := rpcclient.LogicRpcClient.Call(rk, "LogicService.NotifyDelivered", args, reply); err != nil {
		logger.Log.Errorf("NotifyDelivered RPC 失败: %v", err)
	}
}

// singleChatConvID 单聊会话 ID（有序用户对），用于会话内 Seq 的 Redis 键空间隔离 保证键的唯一性
func singleChatConvID(a, b int64) string {
	if a > b {
		return fmt.Sprintf("%d_%d", b, a)
	}
	return fmt.Sprintf("%d_%d", a, b)
}

func marshalChatPush(msgID, seqID, from int64, content string) ([]byte, error) {
	return json.Marshal(map[string]interface{}{
		"chat_type": "chat_push",
		"msg_id":    msgID,
		"seq_id":    seqID,
		"from":      from,
		"content":   content,
	})
}

func handleSingleChat(senderId int64, msgData []byte) {
	var clientReq ClientRequest
	if err := json.Unmarshal(msgData, &clientReq); err != nil {
		logger.Log.Errorf("JSON解析失败: %v", err)
		return
	}
	logger.Log.Infof("[Gateway] 收到 single_chat: sender=%d receiver=%d body=%s", senderId, clientReq.Receiver, clientReq.Message)

	bgCtx := context.Background()
	var msgID, seqID int64
	var convID string

	if DefaultReliability != nil {
		convID = singleChatConvID(senderId, clientReq.Receiver)
		// 获取会话内单调序号（Redis INCR，保证同会话内消息有序）
		seq, err := DefaultReliability.GetNextSeqID(bgCtx, convID, senderId)
		if err != nil {
			logger.Log.Errorf("分配会话序号失败: %v", err)
			return
		}
		seqID = seq
		msgID = DefaultReliability.GenerateMsgID(bgCtx, convID, seqID)
	}

	sendMessageArgs := &pb_msg.SendMessageArgs{
		SenderId:   senderId,
		ReceiverId: clientReq.Receiver,
		Body:       clientReq.Message,
		MsgId:      msgID,
		SeqId:      seqID,
	}

	sendMessageReply := &pb_msg.SendMessageReply{}
	routingKey := strconv.FormatInt(senderId, 10)
	ctx := xclient.WithRoutingKey(context.Background(), routingKey)
	err := rpcclient.LogicRpcClient.Call(ctx, "LogicService.SendMessage", sendMessageArgs, sendMessageReply)
	if err != nil {
		log.Printf("用户 [%d] 的消息发送失败: %v", senderId, err)
		return
	}

	// Logic 落库成功并已 Publish 到 RabbitMQ
	// MQ 消费者（mq_consumer.go）将负责把消息推送给接收方的 WebSocket 连接
	// 若接收方离线，消息已落库，上线时 SyncUnread 会自动拉取
	finalMsgID := sendMessageReply.MsgId
	if finalMsgID == 0 {
		finalMsgID = msgID
	}
	logger.Log.Infof("[Gateway] RPC 成功，消息已委托 MQ 分发: sender=%d receiver=%d msgID=%d",
		senderId, clientReq.Receiver, finalMsgID)
}

func handleAck(userId int64, msgData []byte) {
	var ackReq struct {
		MsgId    int64 `json:"msg_id"`
		SenderId int64 `json:"sender_id"`
	}
	if err := json.Unmarshal(msgData, &ackReq); err != nil {
		log.Println("ACK 参数解析失败", err)
		return
	}
	ackMessageArgs := &pb_msg.AckMessageArgs{
		MsgId: ackReq.MsgId,
	}
	ackMessageReply := &pb_msg.AckMessageReply{}

	routingKey := strconv.FormatInt(userId, 10)
	ctx := xclient.WithRoutingKey(context.Background(), routingKey)
	err := rpcclient.LogicRpcClient.Call(ctx, "LogicService.AckMessage", ackMessageArgs, ackMessageReply)
	if err != nil {
		log.Printf("更新已读状态失败: %v", err)
		return
	}

	if senderClient, ok := GlobalCliMap.Get(strconv.FormatInt(ackReq.SenderId, 10)); ok {
		ackMsg := fmt.Sprintf(`{"chat_type": "ack", "read_receipt": %d, "msg_id": %d}`, userId, ackReq.MsgId)
		senderClient.SendMessage([]byte(ackMsg))
		log.Printf("ACK成功发送给 [%d]", ackReq.SenderId)
	}
}
