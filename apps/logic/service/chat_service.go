package service

import (
	"context"
	"errors"
	"github.com/go-redis/redis/v8"
	"go-im-system/apps/logic/agent"
	"go-im-system/apps/logic/dao"
	"go-im-system/apps/logic/models"
	"go-im-system/apps/pkg/cache"
	"go-im-system/apps/pkg/proto/pb_msg"
	"log"
	"strconv"
)

// 处理gateway推过来的发消息请求

type LogicService struct{}

func (s *LogicService) SendMessage(args *pb_msg.SendMessageArgs, reply *pb_msg.SendMessageReply) error {
	log.Printf("[Logic 服] 收到业务请求: %d -> %d, 内容: %s", args.SenderId, args.ReceiverId, args.Body)

	// AI(用户id: -1)聊天逻辑
	if args.ReceiverId == -1 {
		// 将用户提问插入数据库
		message := models.NewMessages(args.SenderId, -1, args.Body, true)
		err := dao.InsertMessage(message)
		if err != nil {
			log.Printf("插入数据库失败: %v", err)
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
	gatewayAddr, err := cache.RedisClient.Get(ctx, redisKey).Result()
	if errors.Is(err, redis.Nil) {
		log.Printf("用户 [%d] 不在线，准备落库 MySQL", args.ReceiverId)
		message := models.NewMessages(args.SenderId, args.ReceiverId, args.Body, false)
		err := dao.InsertMessage(message)
		if err != nil {
			log.Printf("插入数据库失败: %v", err)
			return err
		}
		reply.MsgId = message.MsgId
	} else if err != nil {
		log.Printf("查询 Redis 出错: %v", err)
		return err
	} else {
		log.Printf("用户 [%d] 在线，连在网关 [%s] 上", args.ReceiverId, gatewayAddr)
		// MVP 阶段：既然你知道他在线，说明可以发。
		// （由于现在只有一台网关，其实 Gateway 自己去查 CliMap 就能发，Logic 这里打印日志即可）
		message := models.NewMessages(args.SenderId, args.ReceiverId, args.Body, false)
		err := dao.InsertMessage(message)
		if err != nil {
			log.Printf("插入数据库失败: %v", err)
			return err
		}
		reply.MsgId = message.MsgId
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
			MsgId:    msg.MsgId,
			SenderId: msg.SenderId,
			Content:  msg.Content,
		})
	}
	return nil
}

func (s *LogicService) ReadMessages(args *pb_msg.ReadMessagesArgs, reply *pb_msg.ReadMessagesReply) error {
	err := dao.MarkMessageAsRead(args.ReceiverId)
	if err != nil {
		reply.Success = false
		return err
	} else {
		reply.Success = true
		return nil
	}
}
