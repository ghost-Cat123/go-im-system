package agent

import (
	"context"
	"flag"
	"fmt"
	"github.com/cloudwego/eino-ext/components/model/deepseek"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/prebuilt/deep"
	"github.com/cloudwego/eino/callbacks"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"go-im-system/apps/logic/agent/callback"
	"go-im-system/apps/logic/agent/middleware"
	"go-im-system/apps/logic/agent/tools"
	"go-im-system/apps/pkg/config"
	"os"
	"strings"
)

func GetAgentGraphRunner() *adk.Runner {
	defaultAgent := config.GetDefaultAgent()
	// 系统指令 定义命令行参数 -instruction
	var instruction string
	flag.StringVar(&instruction, "instruction", "You are a helpful assistant.", "")
	flag.Parse()

	ctx := context.Background()
	// 配置ChatModel
	cm, err := deepseek.NewChatModel(ctx, &deepseek.ChatModelConfig{
		APIKey:  defaultAgent.APIKey,
		Model:   defaultAgent.ModelName,
		BaseURL: defaultAgent.BaseURL,
	})
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "init model failed: %v\n", err)
		os.Exit(1)
	}
	// 注册全局chatModel and tools callbacks
	callbacks.AppendGlobalHandlers(&callback.TraceLoggerCallback{})
	// 创建Agent
	agent, err := deep.New(ctx, &deep.Config{
		Name:         "IM-System-Agent",
		Description:  "DeepSeek Agent",
		ChatModel:    cm,
		Instruction:  instruction,
		MaxIteration: 5,
		Handlers: []adk.ChatModelAgentMiddleware{
			&middleware.RateLimitMiddleware{},
			&middleware.SafeAgentMiddleware{},
		},
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: []tool.BaseTool{
					tools.MustSearchHistoryTool(),
					tools.MustSchMessageTool(),
				},
			},
		},
		ModelRetryConfig: &adk.ModelRetryConfig{
			MaxRetries: 5,
			IsRetryAble: func(_ context.Context, err error) bool {
				return strings.Contains(err.Error(), "429") ||
					strings.Contains(err.Error(), "Too Many Requests") ||
					strings.Contains(err.Error(), "qpm limit")
			},
		},
	})
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	return adk.NewRunner(ctx, adk.RunnerConfig{
		Agent:           agent,
		EnableStreaming: true,
	})
}
