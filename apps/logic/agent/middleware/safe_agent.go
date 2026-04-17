package middleware

import (
	"context"
	"errors"
	"fmt"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"io"
)

// SafeAgentMiddleware 拦截工具与模型调用错误
type SafeAgentMiddleware struct {
	*adk.BaseChatModelAgentMiddleware
}

func (m *SafeAgentMiddleware) WrapModel(
	ctx context.Context, baseModel model.BaseChatModel, _ *adk.ModelContext,
) (model.BaseChatModel, error) {
	// 包装官方模型，重写 Call/Stream 方法，捕获模型错误
	return &safeModelWrapper{model: baseModel}, nil
}

// 模型包装器：捕获模型调用错误，转换为友好提示
type safeModelWrapper struct {
	model model.BaseChatModel
}

// Generate Call 重写 同步模型调用：捕获错误
func (w *safeModelWrapper) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	resp, err := w.model.Generate(ctx, input, opts...)
	if err != nil {
		// 官方逻辑：跳过中断重跑错误，其他错误转为友好字符串
		if _, ok := compose.IsInterruptRerunError(err); ok {
			return nil, err
		}
		// 非中断错误：包装后返回（和工具错误格式保持一致）
		return nil, fmt.Errorf("[model error] %v", err)
	}
	return resp, nil
}

// Stream 重写 流式模型调用：捕获错误（兼容流式输出）
func (w *safeModelWrapper) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	stream, err := w.model.Stream(ctx, input, opts...)
	if err != nil {
		if _, ok := compose.IsInterruptRerunError(err); ok {
			return nil, err
		}
		return nil, fmt.Errorf("[model error] %v", err)
	}
	return stream, nil
}

// WrapInvokableToolCall [工具错误兜底] 你提供的官方标准写法（完全保留，无需修改）
func (m *SafeAgentMiddleware) WrapInvokableToolCall(
	_ context.Context,
	endpoint adk.InvokableToolCallEndpoint,
	_ *adk.ToolContext,
) (adk.InvokableToolCallEndpoint, error) {
	return func(ctx context.Context, args string, opts ...tool.Option) (string, error) {
		result, err := endpoint(ctx, args, opts...)
		if err != nil {
			if _, ok := compose.IsInterruptRerunError(err); ok {
				return "", err
			}
			return fmt.Sprintf("[tool error] %v", err), nil
		}
		return result, nil
	}, nil
}

func (m *SafeAgentMiddleware) WrapStreamableToolCall(
	_ context.Context,
	endpoint adk.StreamableToolCallEndpoint,
	_ *adk.ToolContext,
) (adk.StreamableToolCallEndpoint, error) {
	return func(ctx context.Context, args string, opts ...tool.Option) (*schema.StreamReader[string], error) {
		sr, err := endpoint(ctx, args, opts...)
		if err != nil {
			if _, ok := compose.IsInterruptRerunError(err); ok {
				return nil, err
			}
			return singleChunkReader(fmt.Sprintf("[tool error] %v", err)), nil
		}
		return safeWrapReader(sr), nil
	}, nil
}

// 以下是你提供的工具流包装辅助函数（完全保留）
func singleChunkReader(msg string) *schema.StreamReader[string] {
	r, w := schema.Pipe[string](1)
	_ = w.Send(msg, nil)
	w.Close()
	return r
}

func safeWrapReader(sr *schema.StreamReader[string]) *schema.StreamReader[string] {
	r, w := schema.Pipe[string](64)
	go func() {
		defer w.Close()
		for {
			chunk, err := sr.Recv()
			if errors.Is(err, io.EOF) {
				return
			}
			if err != nil {
				_ = w.Send(fmt.Sprintf("\n[tool error] %v", err), nil)
				return
			}
			_ = w.Send(chunk, nil)
		}
	}()
	return r
}
