package agent

import (
	"context"
	"errors"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"go-im-system/apps/logic/dao"
	"go-im-system/apps/logic/models"
	"go-im-system/apps/pkg/cache"
	"io"
	"log"
	"strconv"
	"strings"
	"time"
)

func HandlerAIMessage(senderId int64, content string) {
	ctx := context.Background()
	sessionID := "agent_session:" + strconv.FormatInt(senderId, 10)

	// 1. 调用Eino 返回channel (存储的AI回应)
	runner := GetAgentGraphRunner()
	// 将用户输入 转换为schema
	userMsg := schema.UserMessage(strings.TrimSpace(content))
	// 创建store
	store := NewRedisStore(time.Hour * 24)
	// 生成sessionID
	// 创建或回复session
	session := store.GetOrCreate(sessionID)
	// 存入session
	err := session.Append(ctx, userMsg)
	if err != nil {
		log.Printf("无法存入session: %v", err)
		return
	}
	// 构建[]*schema.Message列表
	history, err := session.GetMessages(ctx)
	// 执行
	events := runner.Run(ctx, history)
	// getAssistantFromEvents 返回一个channel
	outStream, _ := getAssistantFromEvents(events)
	var fullText string
	// 2. 循环读channel 并推入Redis
	for chunkText := range outStream {
		fullText += chunkText
		cache.RedisClient.Publish(ctx, "ai:chunk:user:"+strconv.FormatInt(senderId, 10), chunkText)
	}
	// 3. 发布结束符告诉前端 AI生成完毕
	cache.RedisClient.Publish(ctx, "ai:chunk:user:"+strconv.FormatInt(senderId, 10), "[DONE]")
	// 4. 存储AI消息到session
	assistantMsg := schema.AssistantMessage(fullText, nil)
	err = session.Append(ctx, assistantMsg)
	if err != nil {
		log.Printf("无法存入session: %v", err)
		return
	}

	// 4. 将完整消息存入mysql 实现不同设备读取 AI发给用户的消息
	_ = dao.InsertMessage(models.NewMessages(-1, senderId, fullText, false))
}

func getAssistantFromEvents(events *adk.AsyncIterator[*adk.AgentEvent]) (<-chan string, error) {
	// 无缓冲channel 实时接收字符串
	outChan := make(chan string)
	// 开启协程 处理events迭代器
	go func() {
		// 关闭通道
		defer close(outChan)
		for {
			event, ok := events.Next()
			if !ok {
				break
			}
			if event.Err != nil {
				log.Printf("AI 事件流解析报错: %v", event.Err)
			}
			// 消息为空
			if event.Output == nil || event.Output.MessageOutput == nil {
				continue
			}
			mv := event.Output.MessageOutput
			// 只处理AI消息
			if mv.Role != schema.Assistant {
				continue
			}
			// 处理开启Streaming 的情况
			if mv.IsStreaming {
				mv.MessageStream.SetAutomaticClose()
				for {
					frame, err := mv.MessageStream.Recv()
					// 当前流式消息处理完毕
					if errors.Is(err, io.EOF) {
						break
					}
					if err != nil {
						log.Printf("接收流式帧报错: %v", err)
						break
					}
					if frame != nil && frame.Content != "" {
						// 将消息存入通道
						outChan <- frame.Content
					}
				}
				continue
			}
			// 处理非流式的情况
			if mv.Message != nil && mv.Message.Content != "" {
				outChan <- mv.Message.Content
			}
		}
	}()
	return outChan, nil
}
