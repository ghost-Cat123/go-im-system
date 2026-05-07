package engine

import (
	"context"
	"errors"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"go-im-system/apps/agent/memory"
	"go-im-system/apps/pkg/logger"
	"io"
	"strconv"
	"strings"
	"time"
)

// PrepareAgentContext 准备AI消息
func PrepareAgentContext(ctx context.Context, senderId int64, message string) (*adk.Runner, *memory.Session, []*schema.Message, error) {
	// 用于后续工具调用、中间件等操作
	sessionID := "agent_session:" + strconv.FormatInt(senderId, 10)
	logger.Log.Infof("[AI] 开始处理用户[%d]问题: %s", senderId, strings.TrimSpace(message))
	// 1. 调用Eino 返回channel (存储的AI回应)
	runner, err := GetAgentGraphRunner(ctx)
	if err != nil {
		logger.Log.Errorf("[AI] 初始化 Runner 失败: %v", err)
		return nil, nil, nil, err
	}
	// 创建store
	store := memory.NewRedisStore(time.Hour * 24)
	// 生成sessionID
	// 创建或回复session
	session := store.GetOrCreate(sessionID)
	// 将用户输入 转换为schema
	userMsg := schema.UserMessage(strings.TrimSpace(message))
	if err := session.Append(ctx, userMsg); err != nil {
		return nil, nil, nil, err
	}
	// 获取历史消息
	history, err := session.GetMessages(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	return runner, session, history, nil
}

func GetAssistantFromEvents(events *adk.AsyncIterator[*adk.AgentEvent]) (<-chan string, error) {
	// 实时接收字符串 避免主循环return接收者不接收导致永久阻塞
	// 缓冲区用于写入剩余帧后正常关闭
	outChan := make(chan string, 64)
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
				logger.Log.Errorf("AI 事件流解析报错: %v", event.Err)
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
						logger.Log.Errorf("接收流式帧报错: %v", err)
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
