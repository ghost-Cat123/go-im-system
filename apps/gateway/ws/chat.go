package ws

import (
	"GeeRPC/xclient"
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"go-im-system/apps/pkg/config"
	"go-im-system/apps/pkg/mq"
	"net/http"
	"net/url"
	"strconv"
	"strings"

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

	bgCtx := context.Background()
	var msgID, seqID int64
	var convID string

	// 可靠性管理器：生成会话ID、消息ID、会话内序号
	if DefaultReliability != nil {
		convID = singleChatConvID(senderId, clientReq.Receiver)
		seq, err := DefaultReliability.GetNextSeqID(bgCtx, convID, senderId)
		if err != nil {
			logger.Log.Errorf("分配会话序号失败: %v", err)
			return
		}
		seqID = seq
		msgID = DefaultReliability.GenerateMsgID(bgCtx, convID, seqID)
	}

	// 上行：网关不调RPC不查DB，生成MsgID后直推MQ
	payload := mq.UploadPayload{
		MsgID:      msgID,
		SeqID:      seqID,
		ConvID:     convID,
		SenderID:   senderId,
		ReceiverID: clientReq.Receiver,
		Content:    clientReq.Message,
	}
	body, _ := json.Marshal(payload)

	// 上行失败直接打日志 等待客户端重试
	if pubErr := mq.PublishUpload(bgCtx, "upload.all", body); pubErr != nil {
		logger.Log.Errorf("[Gateway] 上行Publish失败，客户端将触发重试: %v", pubErr)
		return
	}

	// 给自己的ws发送server_ack
	if senderClient, ok := GlobalCliMap.Get(strconv.FormatInt(senderId, 10)); ok {
		ackMsg := fmt.Sprintf(`{"chat_type":"server_ack","msg_id":%d,"seq_id":%d}`, msgID, seqID)
		senderClient.SendMessage([]byte(ackMsg))
	}
	logger.Log.Infof("[Gateway] 上行Publish成功: sender=%d receiver=%d msgID=%d",
		senderId, clientReq.Receiver, msgID)
}

func handlerAIChat(senderId int64, msgData []byte) {
	var clientReq ClientRequest
	if err := json.Unmarshal(msgData, &clientReq); err != nil {
		logger.Log.Errorf("JSON解析失败: %v", err)
		return
	}
	bgCtx := context.Background()

	// 生成AI对话专属MsgID
	convID := fmt.Sprintf("ai_%d", senderId)
	var msgID, seqID int64

	// 可靠性管理器：生成会话ID、消息ID、会话内序号
	if DefaultReliability != nil {
		seq, err := DefaultReliability.GetNextSeqID(bgCtx, convID, senderId)
		if err != nil {
			logger.Log.Errorf("分配会话序号失败: %v", err)
			return
		}
		seqID = seq
		msgID = DefaultReliability.GenerateMsgID(bgCtx, convID, seqID)
	}

	// 包装用户提问走MQ上行 Logic中异步落库
	payload := mq.UploadPayload{
		MsgID:      msgID,
		SeqID:      seqID,
		ConvID:     convID,
		SenderID:   senderId,
		ReceiverID: -1,
		Content:    clientReq.Message,
	}
	body, _ := json.Marshal(payload)
	if pubErr := mq.PublishUpload(bgCtx, "upload.all", body); pubErr != nil {
		logger.Log.Errorf("[Gateway] AI上行Publish失败: %v", pubErr)
		return
	}
	// 服务端ACK
	if senderClient, ok := GlobalCliMap.Get(strconv.FormatInt(senderId, 10)); ok {
		ackMsg := fmt.Sprintf(`{"chat_type":"server_ack","msg_id":%d,"seq_id":%d}`, msgID, seqID)
		senderClient.SendMessage([]byte(ackMsg))
	}

	// SSE直连Agent，按chunk推送给网关
	agentURL := fmt.Sprintf("%s/agent/chat/sse?user_id=%d&message=%s",
		config.GlobalConfig.Server.AgentAddr, senderId, url.QueryEscape(clientReq.Message))
	resp, err := http.Get(agentURL)
	if err != nil {
		logger.Log.Errorf("[Gateway] 连接 Agent SSE 失败: %v", err)
		return
	}
	defer resp.Body.Close()

	senderClient, ok := GlobalCliMap.Get(strconv.FormatInt(senderId, 10))
	if !ok {
		// 离线直接返回 AI消息已落库兜底
		return
	}
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		// 结束符
		if line == "data: [DONE]" {
			senderClient.SendMessage([]byte(`{"chat_type":"ai_end","from":"-1"}`))
			return
		}
		// 消息
		if strings.HasPrefix(line, "data: ") {
			// 去除前缀
			chunk := strings.TrimPrefix(line, "data: ")
			msg := fmt.Sprintf(`{"chat_type":"ai_chunk","from":"-1","content":"%s"}`, chunk)
			// 发送消息
			senderClient.SendMessage([]byte(msg))
		}
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

	if senderClient, ok := GlobalCliMap.Get(strconv.FormatInt(ackReq.SenderId, 10)); ok {
		ackMsg := fmt.Sprintf(`{"chat_type": "ack", "read_receipt": %d, "msg_id": %d}`, userId, ackReq.MsgId)
		senderClient.SendMessage([]byte(ackMsg))
		log.Printf("ACK成功发送给 [%d]", ackReq.SenderId)
	}
}
