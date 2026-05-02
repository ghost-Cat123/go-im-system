package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/go-redis/redis/v8"
	amqp "github.com/rabbitmq/amqp091-go"
	"go-im-system/apps/logic/agent"
	"go-im-system/apps/logic/dao"
	"go-im-system/apps/logic/models"
	"go-im-system/apps/pkg/cache"
	"go-im-system/apps/pkg/logger"
	"go-im-system/apps/pkg/mq"
	"strconv"
)

// StartUploadConsumer 启动上行消息消费者（Gateway → MQ → Logic）。
// Logic 从 message.upload 消费，校验落库后发布下行。
func StartUploadConsumer() {
	// 声明共享队列 路由统一
	msgs, err := mq.ConsumeUploadQueue("logic.upload.queue", "upload.all")
	if err != nil {
		logger.Log.Fatalf("上行消费者启动失败: %v", err)
	}
	logger.Log.Infof("✅ 上行消费者已启动，Queue: logic.upload.queue")
	// Logic启动上行异步消费
	go consumeUploadLoop(msgs)
}

func consumeUploadLoop(msgs <-chan amqp.Delivery) {
	// 从channel中拿出消息
	for d := range msgs {
		handleUploadDelivery(d)
	}
	logger.Log.Warn("上行消费通道已关闭")
}

func handleUploadDelivery(d amqp.Delivery) {
	var payload mq.UploadPayload
	if err := json.Unmarshal(d.Body, &payload); err != nil {
		logger.Log.Errorf("[Upload] Payload 解析失败，丢弃消息: %v", err)
		// 发送NACK 不重试 不重入队 上游发送了脏数据
		_ = d.Nack(false, false)
		return
	}

	logger.Log.Infof("[Upload] 收到上行消息: %d -> %d, msgID=%d", payload.SenderID, payload.ReceiverID, payload.MsgID)

	// AI 和普通消息走统一入口，由 Logic 根据 receiver 分发
	if payload.ReceiverID == -1 {
		if err := handleAIChat(&payload); err != nil {
			// DB写入失败重入队
			_ = d.Nack(false, true)
		}
	} else {
		if err := handleSingleChat(&payload); err != nil {
			_ = d.Nack(false, true)
		}
	}

	// 手动 ACK：消息已成功处理
	_ = d.Ack(false)
}

// handleSingleChat 普通单聊消息处理：落库 → 查Redis路由 → 在线则下行推送
func handleSingleChat(payload *mq.UploadPayload) error {
	ctx := context.Background()

	// 先落库
	message := models.NewMessages(payload.SenderID, payload.ReceiverID, payload.Content, false)
	message.MsgId = payload.MsgID
	message.SeqId = payload.SeqID

	if err := dao.InsertMessage(message); err != nil {
		return fmt.Errorf("[Upload] 插入数据库失败: %v", err)
	}

	// 查 Redis 路由表，判断接收方是否在线
	redisKey := "route:user:" + strconv.FormatInt(payload.ReceiverID, 10)
	gatewayAddr, err := cache.GetCache().Get(ctx, redisKey).Result()
	if errors.Is(err, redis.Nil) {
		return fmt.Errorf("[Upload] 用户 [%d] 离线，消息已落库", payload.ReceiverID)

	}
	if err != nil {
		return fmt.Errorf("[Upload] 查询 Redis 出错: %v", err)
	}

	// 在线：发布下行到目标网关的专属 Queue
	logger.Log.Infof("[Upload] 用户 [%d] 在线，网关 [%s]，发布下行", payload.ReceiverID, gatewayAddr)
	downPayload := &mq.DownPayload{
		MsgID:      payload.MsgID,
		SeqID:      payload.SeqID,
		SenderID:   payload.SenderID,
		ReceiverID: payload.ReceiverID,
		Content:    payload.Content,
		ChatType:   "chat_push",
	}
	body, _ := json.Marshal(downPayload)
	if pubErr := mq.PublishDown(ctx, gatewayAddr, body); pubErr != nil {
		logger.Log.Errorf("[Upload] 下行Publish失败(已落库，不影响): %v", pubErr)
		return nil
	} else {
		logger.Log.Infof("[Upload] 下行Publish成功: receiver=%d msgID=%d gateway=%s",
			payload.ReceiverID, payload.MsgID, gatewayAddr)
		return nil
	}
}

// handleAIChat AI 消息处理：落库 → 异步调用 Eino Agent
func handleAIChat(payload *mq.UploadPayload) error {
	message := models.NewMessages(payload.SenderID, -1, payload.Content, true)
	message.MsgId = payload.MsgID
	message.SeqId = payload.SeqID

	if err := dao.InsertMessage(message); err != nil {
		return fmt.Errorf("[Upload] AI消息落库失败: %v", err)
	}

	// 异步处理 AI 对话（Eino Agent + Redis PubSub 流式推送）
	go agent.HandlerAIMessage(payload.SenderID, payload.Content)
	return nil
}
