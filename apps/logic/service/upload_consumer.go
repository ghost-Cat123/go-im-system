package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go-im-system/apps/logic/dao"
	"go-im-system/apps/logic/models"
	"go-im-system/apps/pkg/cache"
	"go-im-system/apps/pkg/logger"
	"go-im-system/apps/pkg/mq"
	"go-im-system/apps/pkg/utils"
	"strconv"

	"github.com/go-redis/redis/v8"
	amqp "github.com/rabbitmq/amqp091-go"
)

const uploadWorkers = 50

// StartUploadConsumer 启动上行消息消费者（Gateway → MQ → Logic）。
// Logic 从 message.upload 消费，校验落库后发布下行。
// 启动多个 goroutine 并发消费同一个 MQ Channel，RabbitMQ 自动在 worker 间分发消息。
func StartUploadConsumer() {
	msgs, err := mq.ConsumeUploadQueue("logic.upload.queue", "upload.all")
	if err != nil {
		logger.Log.Fatalf("上行消费者启动失败: %v", err)
	}
	logger.Log.Infof("✅ 上行消费者已启动，Queue: logic.upload.queue, Workers: %d", uploadWorkers)

	for i := 0; i < uploadWorkers; i++ {
		go consumeUploadLoop(msgs)
	}
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

	// 这里判断消息类型
	switch payload.ChatType {
	case mq.ChatTypeSingle:
		if err := handleSingleChat(&payload); err != nil {
			_ = d.Nack(false, true)
			return
		}
	case mq.ChatTypeGroup:
		if err := handleGroupChat(&payload); err != nil {
			_ = d.Nack(false, true)
			return
		}
	case mq.ChatTypeAI:
		if err := handleAIChat(&payload); err != nil {
			_ = d.Nack(false, true)
			return
		}
	}

	_ = d.Ack(false)
}

// handleSingleChat 普通单聊消息处理：落库 → 查Redis路由 → 在线则下行推送
func handleSingleChat(payload *mq.UploadPayload) error {
	ctx := context.Background()

	// 先落库
	message := models.NewMessages(payload.SenderID, payload.ReceiverID, payload.GroupID, payload.Content, false)
	message.MsgId = payload.MsgID
	message.SeqId = payload.SeqID

	if err := dao.InsertMessage(message); err != nil {
		return fmt.Errorf("[Upload] 插入数据库失败: %v", err)
	}

	// 查 Redis 路由表，判断接收方是否在线
	redisKey := "route:user:" + strconv.FormatInt(payload.ReceiverID, 10)
	gatewayAddr, err := cache.GetCache().Get(ctx, redisKey).Result()
	if errors.Is(err, redis.Nil) {
		logger.Log.Infof("[Upload] 用户 [%d] 离线，消息已落库", payload.ReceiverID)
		return nil
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
		ChatType:   payload.ChatType,
	}
	body, _ := json.Marshal(downPayload)
	if pubErr := mq.PublishDown(ctx, gatewayAddr, body); pubErr != nil {
		logger.Log.Warn("[Upload] 下行Publish失败(已落库，不影响): %v", pubErr)
		return nil
	} else {
		logger.Log.Debug("[Upload] 下行Publish成功: receiver=%d msgID=%d gateway=%s",
			payload.ReceiverID, payload.MsgID, gatewayAddr)
		return nil
	}
}

// 消息写扩散
func handleGroupChat(payload *mq.UploadPayload) error {
	ctx := context.Background()

	// 1. 查询所有群成员
	members, err := dao.GetGroupMembers(payload.GroupID)
	if err != nil {
		return fmt.Errorf("[Upload] 查询群成员失败: %v", err)
	}
	// 对每个成员执行落库+下行推送
	for _, memberID := range members {
		if memberID == payload.SenderID {
			continue
		}

		// 每个成员生成独立 MsgID（write-diffusion: 群聊消息落库为 N 条独立记录）
		msg := models.NewMessages(payload.SenderID, memberID, payload.GroupID, payload.Content, false)
		msg.MsgId = utils.GetSnowflake().Generate()
		msg.SeqId = payload.SeqID
		if err := dao.InsertMessage(msg); err != nil {
			logger.Log.Errorf("[Upload][Group] 群成员 %d 落库失败: %v", memberID, err)
			continue
		}

		// 判断群成员是否在线
		redisKey := "route:user:" + strconv.FormatInt(memberID, 10)
		gatewayAddr, err := cache.GetCache().Get(ctx, redisKey).Result()
		if errors.Is(err, redis.Nil) {
			// 目标用户离线 等待上线拉取离线消息
			continue
		}
		if err != nil {
			logger.Log.Errorf("[Upload][Group] 查询 Redis 出错: %v", err)
			continue
		}
		// 在线 发布下行payload到网关
		downPayload := mq.DownPayload{
			MsgID:      msg.MsgId,
			SeqID:      payload.SeqID,
			GroupID:    payload.GroupID,
			SenderID:   payload.SenderID,
			ReceiverID: memberID,
			Content:    payload.Content,
			ChatType:   payload.ChatType,
		}

		body, _ := json.Marshal(downPayload)
		if pubErr := mq.PublishDown(ctx, gatewayAddr, body); pubErr != nil {
			logger.Log.Warn("[Upload][Group] 群成员 %d 下行 Publish 失败: %v", memberID, pubErr)
			// 下行失败不影响 离线消息兜底
		} else {
			logger.Log.Debug("[Upload][Group] 群成员下行Publish成功: groupID=%d receiver=%d msgID=%d gateway=%s",
				payload.GroupID, payload.ReceiverID, payload.MsgID, gatewayAddr)
		}
	}
	return nil
}

// handleAIChat AI 消息处理：落库 → 异步调用 Eino Agent
func handleAIChat(payload *mq.UploadPayload) error {
	message := models.NewMessages(payload.SenderID, payload.ReceiverID, payload.GroupID, payload.Content, true)
	message.MsgId = payload.MsgID
	message.SeqId = payload.SeqID

	if err := dao.InsertMessage(message); err != nil {
		return fmt.Errorf("[Upload] AI消息落库失败: %v", err)
	}
	return nil
}
