package ws

import (
	"GeeRPC/xclient"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/gorilla/websocket"
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

func handleSingleChat(messageType int, senderId int64, msgData []byte) {
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
		// 获取会话内序号
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
	logger.Log.Infof("[Gateway] SendMessage RPC 成功: sender=%d receiver=%d msgID=%d", senderId, clientReq.Receiver, sendMessageReply.MsgId)

	// 如果可靠性管理器为空，则直接推送消息
	if DefaultReliability == nil {
		receiverStr := strconv.FormatInt(clientReq.Receiver, 10)
		if receiverConn, ok := GlobalCliMap.Get(receiverStr); ok {
			pushMsg := fmt.Sprintf(`{"from": "%d", "content": "%s"}`, senderId, clientReq.Message)
			_ = receiverConn.WriteMessage(messageType, []byte(pushMsg))
		}
		return
	}

	// 使用 Logic 返回的 msg_id（与落库一致；网关已传 msg_id 时通常相同）
	finalMsgID := sendMessageReply.MsgId
	if finalMsgID == 0 {
		finalMsgID = msgID
	}
	// 组装待确认投递
	pending := &PendingMessage{
		MsgID:       finalMsgID,
		ConvID:      convID,
		SenderID:    senderId,
		ReceiverIDs: []int64{clientReq.Receiver},
		Content:     clientReq.Message,
		SeqID:       seqID,
		ChatType:    "single_chat",
		NextRetryAt: time.Now(),
	}
	// 加入待确认投递
	if err := DefaultReliability.AddPending(bgCtx, pending); err != nil {
		logger.Log.Errorf("加入 pending 失败: %v", err)
	}
	// 组装推送 JSON
	payload, err := marshalChatPush(finalMsgID, seqID, senderId, clientReq.Message)
	if err != nil {
		logger.Log.Errorf("组装推送 JSON 失败: %v", err)
		return
	}
	// 推送消息
	receiverStr := strconv.FormatInt(clientReq.Receiver, 10)
	if receiverConn, ok := GlobalCliMap.Get(receiverStr); ok {
		if err := receiverConn.WriteMessage(messageType, payload); err != nil {
			logger.Log.Errorf("首包推送失败，将依赖重试: %v", err)
			return
		}
		if err := DefaultReliability.Ack(bgCtx, finalMsgID); err != nil {
			logger.Log.Errorf("首包送达后 Ack 失败: %v", err)
		}
	} else {
		logger.Log.Infof("用户 [%d] 不在线，pending 已由 Redis 重试线程投递", clientReq.Receiver)
	}
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

	if senderConn, ok := GlobalCliMap.Get(strconv.FormatInt(ackReq.SenderId, 10)); ok {
		ackMsg := fmt.Sprintf(`{"chat_type": "ack", "read_receipt": %d, "msg_id": %d}`, userId, ackReq.MsgId)
		_ = senderConn.WriteMessage(websocket.TextMessage, []byte(ackMsg))
		log.Printf("ACK成功发送给 [%d]", ackReq.SenderId)
	}
}
