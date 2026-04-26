package service

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/go-redis/redis/v8"
	"go-im-system/apps/logic/agent"
	"go-im-system/apps/logic/dao"
	"go-im-system/apps/logic/models"
	"go-im-system/apps/pkg/cache"
	"go-im-system/apps/pkg/logger"
	"go-im-system/apps/pkg/mq"
	"go-im-system/apps/pkg/proto/pb_msg"
	"strconv"
)

// 处理gateway推过来的发消息请求

type LogicService struct{}

func (s *LogicService) SendMessage(args *pb_msg.SendMessageArgs, reply *pb_msg.SendMessageReply) error {
	logger.Log.Infof("[Logic 服] 收到业务请求: %d -> %d, 内容: %s", args.SenderId, args.ReceiverId, args.Body)

	// AI(用户id: -1)聊天逻辑
	if args.ReceiverId == -1 {
		// 将用户提问插入数据库
		message := models.NewMessages(args.SenderId, -1, args.Body, true)
		if args.MsgId != 0 {
			message.MsgId = args.MsgId
			message.SeqId = args.SeqId
		}
		err := dao.InsertMessage(message)
		if err != nil {
			logger.Log.Errorf("插入数据库失败: %v", err)
			return err
		}
		// 开启协程 处理AI对话逻辑
		go agent.HandlerAIMessage(args.SenderId, args.Body)
		reply.MsgId = -1
		return nil
	}

	// 普通聊天逻辑
	ctx := context.Background()
	redisKey := "route:user:" + strconv.FormatInt(args.ReceiverId, 10)
	gatewayAddr, err := cache.GetCache().Get(ctx, redisKey).Result()
	if errors.Is(err, redis.Nil) {
		logger.Log.Infof("用户 [%d] 不在线，准备落库 MySQL", args.ReceiverId)
		message := models.NewMessages(args.SenderId, args.ReceiverId, args.Body, false)
		if args.MsgId != 0 {
			message.MsgId = args.MsgId
			message.SeqId = args.SeqId
		}
		err := dao.InsertMessage(message)
		if err != nil {
			logger.Log.Errorf("插入数据库失败: %v", err)
			return err
		}
		reply.MsgId = message.MsgId
	} else if err != nil {
		logger.Log.Errorf("查询 Redis 出错: %v", err)
		return err
	} else {
		// 用户在线：落库并通过 RabbitMQ 通知目标网关实时推送
		logger.Log.Infof("用户 [%d] 在线，连在网关 [%s] 上", args.ReceiverId, gatewayAddr)
		message := models.NewMessages(args.SenderId, args.ReceiverId, args.Body, false)
		if args.MsgId != 0 {
			message.MsgId = args.MsgId
			message.SeqId = args.SeqId
		}
		err := dao.InsertMessage(message)
		if err != nil {
			logger.Log.Errorf("插入数据库失败: %v", err)
			return err
		}
		reply.MsgId = message.MsgId

		// 落库成功后将消息发布到 RabbitMQ
		// RoutingKey = gatewayAddr，Direct Exchange 将消息精准投递到目标网关的专属 Queue
		payload := &mq.PushPayload{
			MsgID:      message.MsgId,
			SeqID:      message.SeqId,
			SenderID:   message.SenderId,
			ReceiverID: message.ReceiverId,
			Content:    message.Content,
			ChatType:   "chat_push",
		}
		body, _ := json.Marshal(payload)
		if pubErr := mq.Publish(ctx, gatewayAddr, body); pubErr != nil {
			// Publish 失败不影响落库，接收方上线时会通过 SyncUnread 全量拉取兑底
			logger.Log.Errorf("Publish 到 RabbitMQ 失败(已落库，不影响): %v", pubErr)
		} else {
			logger.Log.Infof("[MQ] Publish 成功: receiver=%d msgID=%d gateway=%s",
				args.ReceiverId, message.MsgId, gatewayAddr)
		}
	}
	return nil
}

func (s *LogicService) SyncUnread(args *pb_msg.SyncUnreadArgs, reply *pb_msg.SyncUnreadReply) error {
	// 去 MySQL 查所有 ReceiverId == args.UserId 且 IsRead == false 的消息
	messages, err := dao.GetUnreadMessages(args.ReceiverId)
	if err != nil {
		return err
	}

	// 组装到 reply 中返回给 Gateway
	for _, msg := range messages {
		reply.Messages = append(reply.Messages, &pb_msg.MessageItem{
			MsgId:      msg.MsgId,
			SenderId:   msg.SenderId,
			Content:    msg.Content,
			SeqId:      msg.SeqId,
			SendStatus: int32(msg.SendStatus),
		})
	}
	return nil
}

// NotifyDelivered 网关 WS 已成功写入接收方连接，将 send_status 从 0 置为 1
func (s *LogicService) NotifyDelivered(args *pb_msg.NotifyDeliveredArgs, reply *pb_msg.NotifyDeliveredReply) error {
	if err := dao.MarkSendTransportDelivered(args.MsgId); err != nil {
		logger.Log.Errorf("NotifyDelivered 失败: %v", err)
		reply.Success = false
		return err
	}
	reply.Success = true
	return nil
}

func (s *LogicService) AckMessage(args *pb_msg.AckMessageArgs, reply *pb_msg.AckMessageReply) error {
	// 去 MySQL 将msgId == args.msg_id 的消息 变为已读
	err := dao.MarkMessageAsRead(args.MsgId)
	if err != nil {
		logger.Log.Errorf("修改据库失败: %v", err)
		reply.Success = false
		return err
	}
	reply.Success = true
	return nil
}

func (s *LogicService) ReadMessages(args *pb_msg.ReadMessagesArgs, reply *pb_msg.ReadMessagesReply) error {
	err := dao.MarkMessagesAsRead(args.ReceiverId)
	if err != nil {
		logger.Log.Errorf("修改据库失败: %v", err)
		reply.Success = false
		return err
	} else {
		reply.Success = true
		return nil
	}
}
