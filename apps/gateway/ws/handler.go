package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"go-im-system/apps/gateway/rpcclient"
	"go-im-system/apps/pkg/cache"
	"go-im-system/apps/pkg/proto/pb_msg"
	"go-im-system/apps/pkg/utils"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

// 全局WebSocket升级器
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// GlobalCliMap CliMap 全局连接管理器
var GlobalCliMap = NewCliMap()

// ClientRequest 定义前端发来的 JSON 格式结构体
type ClientRequest struct {
	Receiver int64  `json:"receiver"`
	Message  string `json:"message"`
}

// WSHandler 处理websocket请求
func WSHandler(c *gin.Context) {
	// 1. 根据token获取用户信息
	token := c.Query("token")
	if token == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未携带 Token"})
		return
	}
	userID, err := utils.ParseToken(token)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "鉴权失败，请重新登录"})
		return
	}
	userIDStr := strconv.FormatInt(userID, 10)

	// 2. 获取该用户的websocket长连接
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("WebSocket 升级失败: %v", err)
		return
	}
	log.Printf("用户 [%s] 成功建立websocket连接", userIDStr)

	// 3. 将conn连接放入全局ConnectionManager Map中维护不同用户的连接
	GlobalCliMap.Set(userIDStr, conn)

	// 4. 将连接信息放入redis路由表
	ctx := context.Background()
	redisKey := "route:user:" + userIDStr
	gatewayAddr := "127.0.0.1:8080"
	err = cache.RedisClient.Set(ctx, redisKey, gatewayAddr, time.Hour*24).Err()
	if err != nil {
		log.Printf("Redis 路由写入失败: %v", err)
	}

	// 5. 拉取所有发送给该用户的未读信息
	err = syncMessage(userID, conn)
	if err != nil {
		log.Printf("消息同步失败: %v", err)
		return
	}

	// 6. 启动当前用户独立的Redis订阅流
	go subscribeAILoop(userIDStr, conn)

	// 6. 开启独立线程 处理该连接客户端消息
	go readLoop(userID, conn)
}

func syncMessage(userID int64, conn *websocket.Conn) error {
	syncArgs := &pb_msg.SyncUnreadArgs{ReceiverId: userID}
	syncReply := &pb_msg.SyncUnreadReply{}
	err := rpcclient.LogicRpcClient.Call(context.Background(), "LogicService.SyncUnread", syncArgs, syncReply)
	if err == nil && len(syncReply.Messages) > 0 {
		// 如果有未读消息，通过 conn 循环 WriteMessage 发给该用户！
		for _, msg := range syncReply.Messages {
			// 构造 JSON 发送
			unReadMsg := fmt.Sprintf(`{"SenderId": "%d", "content": "%s"}`, msg.SenderId, msg.Content)
			_ = conn.WriteMessage(websocket.TextMessage, []byte(unReadMsg))
		}
		// 发送完后，最好再调一个 RPC 告诉 Logic 服：这些消息已读了 (Update is_read = 1)
		readArgs := &pb_msg.ReadMessagesArgs{ReceiverId: userID}
		readReply := &pb_msg.ReadMessagesReply{}
		err = rpcclient.LogicRpcClient.Call(context.Background(), "LogicService.ReadMessages", readArgs, readReply)
		if err != nil || !readReply.Success {
			log.Printf("消息 [%d] 连接关闭失败", userID)
		} else {
			log.Printf("未读消息拉取成功")
		}
	}
	return err
}

func subscribeAILoop(userIDStr string, conn *websocket.Conn) {
	ctx := context.Background()
	// 频道名要和发布时一致
	pubSubChannel := "ai:chunk:user:" + userIDStr
	// 1. 开启订阅
	pubSub := cache.RedisClient.Subscribe(ctx, pubSubChannel)
	// 2. 协程退出 关闭订阅 释放Redis连接
	defer func() {
		_ = pubSub.Close()
		log.Printf("用户 [%s] 的 AI 订阅流已关闭", userIDStr)
	}()
	// 3. 阻塞监听频道消息
	for msg := range pubSub.Channel() {
		// 如果系统发出了关闭连接的信号 需要context通知 其他协程关闭
		if msg.Payload == "[DONE]" {
			// 给前端的
			endMsg := `{"chat_type": "ai_end", "from": "-1", "content": ""}`
			if err := conn.WriteMessage(websocket.TextMessage, []byte(endMsg)); err != nil {
				// 用户断开 销毁订阅
				break
			}
			continue
		}
		// 收到普通文字 Chunk，组装成 JSON 推给前端
		pushMsg := fmt.Sprintf(`{"chat_type": "ai_chunk", "from": "-1", "content": "%s"}`, msg.Payload)

		err := conn.WriteMessage(websocket.TextMessage, []byte(pushMsg))
		if err != nil {
			log.Printf("向用户 [%s] 推送 AI 流式数据失败: %v", userIDStr, err)
			// 写入失败 退出循环
			break
		}
	}
}

// 死循环读取客户端消息
func readLoop(userID int64, conn *websocket.Conn) {
	userIDStr := strconv.FormatInt(userID, 10)
	// 连接断开时的清理工作
	defer func() {
		log.Printf("用户 [%d] 断开连接", userID)
		// 1. 本地连接池移除该用户的连接
		GlobalCliMap.Delete(userIDStr)
		// 2. 从 Redis 路由表清理 (宣告全网该用户离线)
		ctx := context.Background()
		redisKey := "route:user:" + userIDStr
		cache.RedisClient.Del(ctx, redisKey)
		err := conn.Close()
		if err != nil {
			log.Printf("用户 [%d] 连接关闭失败", userID)
			return
		}
	}()

	for {
		messageType, msgData, err := conn.ReadMessage()
		if err != nil {
			log.Printf("读取消息失败或用户掉线: %v", err)
			break
		}
		// 解析基础包，只看 action 是什么
		var baseReq struct {
			ChatType string `json:"chat_type"`
		}
		if err := json.Unmarshal(msgData, &baseReq); err != nil {
			return
		}

		switch baseReq.ChatType {
		case "ping":
			// 收到前端是心跳信息
			ctx := context.Background()
			redisKey := "route:user:" + userIDStr
			// 重新设置30s过期时间
			cache.RedisClient.Expire(ctx, redisKey, time.Second*30)
			// 给前端发送响应
			_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"action":"pong"}`))
		case "single_chat":
			handleSingleChat(messageType, userID, msgData) // 调专门处理单聊的函数
		case "group_chat":
			// handleGroupChat(userID, msgData)  // 调群聊函数
		case "ack":
			// handleAck(userID, msgData)        // 调已读确认函数
		case "heartbeat":
			// 处理心跳
		default:
			log.Println("未知动作", baseReq.ChatType)
		}
		log.Printf("收到用户 [%d] 的消息: %s", userID, string(msgData))
	}
}
