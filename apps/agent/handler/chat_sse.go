package handler

import (
	"context"
	"fmt"
	"github.com/cloudwego/eino/schema"
	"github.com/gin-gonic/gin"
	"go-im-system/apps/agent/dao"
	"go-im-system/apps/agent/engine"
	"go-im-system/apps/agent/models"
	"go-im-system/apps/pkg/logger"
	"net/http"
	"strconv"
)

func ChatSSE(c *gin.Context) {
	// 提取请求参数
	senderId, _ := strconv.ParseInt(c.Query("user_id"), 10, 64)
	message := c.Query("message")

	// SSE标准响应头
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	// 检查接口实现
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}

	// 构造context
	ctx := context.WithValue(c.Request.Context(), "current_user_id", senderId)
	runner, session, history, err := engine.PrepareAgentContext(ctx, senderId, message)
	if err != nil {
		logger.Log.Errorf("[SSE] 准备上下文失败: %v", err)
		_, _ = fmt.Fprintf(c.Writer, "data: AI服务初始化失败，请稍后重试\n\n")
		_, _ = fmt.Fprintf(c.Writer, "data: [DONE]\n\n")
		flusher.Flush()
		return
	}

	// 处理AI流
	events := runner.Run(ctx, history)
	// 通过channel消费事件
	outStream, _ := engine.GetAssistantFromEvents(events)
	var fullText string
	for chunk := range outStream {
		fullText += chunk
		// 说明客户端断开，停止消费Eino events 节省Token
		if _, err := fmt.Fprintf(c.Writer, "data: %s\n\n", chunk); err != nil {
			logger.Log.Warnf("[SSE] 客户端断开，停止推送: user=%d", senderId)
			return
		}
		flusher.Flush()
	}
	if fullText == "" {
		_, _ = fmt.Fprintf(c.Writer, "data: [DONE]\n\n")
		flusher.Flush()
		return
	}
	// [DONE] 结束符
	_, _ = fmt.Fprintf(c.Writer, "data: [DONE]\n\n")
	flusher.Flush()

	// 异步落库AI消息， 更新Redis Session
	go func() {
		assistantMsg := schema.AssistantMessage(fullText, nil)
		if err := session.Append(context.Background(), assistantMsg); err != nil {
			logger.Log.Errorf("[SSE] session 更新失败: %v", err)
		}
		if err = dao.InsertMessage(models.NewMessages(-1, senderId, fullText, false)); err != nil {
			logger.Log.Errorf("AI消息落库失败: %v", err)
		}
		logger.Log.Infof("[SSE] AI 回复已落库: user=%d", senderId)
	}()
}
