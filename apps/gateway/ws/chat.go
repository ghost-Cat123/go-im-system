package ws

import (
	"GeeRPC/xclient"
	"context"
	"encoding/json"
	"fmt"
	"github.com/gorilla/websocket"
	"go-im-system/apps/gateway/rpcclient"
	"go-im-system/apps/pkg/logger"
	"go-im-system/apps/pkg/proto/pb_msg"
	"log"
	"strconv"
)

func handleSingleChat(messageType int, senderId int64, msgData []byte) {
	var clientReq ClientRequest // 你的单聊专属结构体
	if err := json.Unmarshal(msgData, &clientReq); err != nil {
		logger.Log.Errorf("JSON解析失败: %v", err)
	}

	// 2. 构造 pb.SendMessageArgs
	sendMessageArgs := &pb_msg.SendMessageArgs{
		SenderId:   senderId,
		ReceiverId: clientReq.Receiver,
		Body:       clientReq.Message,
	}

	// 3. 调用你的 rpcclient.LogicClient.Call 发给 Logic 服！
	sendMessageReply := &pb_msg.SendMessageReply{}
	routingKey := strconv.FormatInt(senderId, 10)
	ctx := xclient.WithRoutingKey(context.Background(), routingKey)
	err := rpcclient.LogicRpcClient.Call(ctx, "LogicService.SendMessage", sendMessageArgs, sendMessageReply)
	if err != nil {
		log.Printf("用户 [%d] 的消息发送失败: %v", senderId, err)
	}

	// 4. 消息转发
	receiverStr := strconv.FormatInt(clientReq.Receiver, 10)
	if receiverConn, ok := GlobalCliMap.Get(receiverStr); ok {
		// 目标用户在线！组装一个下发给他的 JSON 消息
		pushMsg := fmt.Sprintf(`{"from": "%d", "content": "%s"}`, senderId, clientReq.Message)
		// 发送给目标用户的物理连接
		_ = receiverConn.WriteMessage(messageType, []byte(pushMsg))
		log.Printf("成功将消息从 [%d] 转发给 [%d]", senderId, clientReq.Receiver)
	} else {
		// 目标用户不在线
		log.Printf("用户 [%d] 不在线，消息已由 Logic 存入离线库", clientReq.Receiver)
	}
}

func handleAck(userId int64, msgData []byte) {
	// 1. 解析前端参数
	var ackReq struct {
		MsgId int64 `json:"msg_id"`
		// 需要通知发送者id
		SenderId int64 `json:"sender_id"`
	}
	if err := json.Unmarshal(msgData, &ackReq); err != nil {
		log.Println("ACK 参数解析失败", err)
		return
	}
	// 2. 调用Logic层RPC 标记为已读
	ackMessageArgs := &pb_msg.AckMessageArgs{
		MsgId: ackReq.MsgId,
	}
	ackMessageReply := &pb_msg.AckMessageReply{}

	err := rpcclient.LogicRpcClient.Call(context.Background(), "LogicService.AckMessage", ackMessageArgs, ackMessageReply)
	if err != nil {
		log.Printf("更新已读状态失败: %v", err)
		return
	}

	// 3. 通知发送者
	if senderConn, ok := GlobalCliMap.Get(strconv.FormatInt(ackReq.SenderId, 10)); ok {
		ackMsg := fmt.Sprintf(`{"chat_type": "ack", "read_receipt": %d, "msg_id": %d}`, userId, ackReq.MsgId)
		// 发送给目标用户的物理连接
		_ = senderConn.WriteMessage(websocket.TextMessage, []byte(ackMsg))
		log.Printf("ACK成功发送给 [%d]", ackReq.SenderId)
	}
}
