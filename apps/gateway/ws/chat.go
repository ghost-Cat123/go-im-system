package ws

import (
	"GeeRPC/xclient"
	"context"
	"encoding/json"
	"fmt"
	"go-im-system/apps/gateway/rpcclient"
	"go-im-system/apps/pkg/proto/pb_msg"
	"log"
	"strconv"
)

func handleSingleChat(messageType int, senderId int64, msgData []byte) {
	var clientReq ClientRequest // 你的单聊专属结构体
	if err := json.Unmarshal(msgData, &clientReq); err != nil {
		log.Printf("JSON解析失败: %v", err)
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
