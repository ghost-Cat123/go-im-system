package tools

import (
	"context"
	"fmt"
	"github.com/cloudwego/eino/components/tool"
	toolutils "github.com/cloudwego/eino/components/tool/utils"
	"go-im-system/apps/logic/dao"
	"go-im-system/apps/pkg/utils"
)

// 借助LLM 通过语义检索和清洗离散的用户聊天信息

// SearHistoryReq  定义入参
type SearHistoryReq struct {
	TargetUser string `json:"target_user" jsonschema:"description=要搜索的聊天对象用户名(username)"`
	StartTime  string `json:"start_time" jsonschema:"description=搜索的起始时间, RFC3339格式"`
	EndTime    string `json:"end_time" jsonschema:"description=搜索的结束时间, RFC3339格式"`
	Keywords   string `json:"keywords" jsonschema:"description=关键词,用空格分隔,例如'数据库 密码'"`
	Limit      int    `json:"limit" jsonschema:"description=最多返回多少条消息,默认50,maximum=200"`
}

// SearHistoryResp 定义出参
type SearHistoryResp struct {
	Result string `json:"result"`
}

// 业务逻辑
func searchHistoryInvoker(ctx context.Context, req *SearHistoryReq) (*SearHistoryResp, error) {
	// 解析时间
	queryStart, queryEnd := utils.ResolveTime(req.StartTime, req.EndTime)
	// 查询数据库 获取目标ID
	targetUser, err := dao.FindUserByName(req.TargetUser)
	if err != nil {
		return &SearHistoryResp{Result: fmt.Sprintf("未找到名为 %s 的用户，请检查用户名拼写。", req.TargetUser)}, nil
	}
	currentUserID, ok := ctx.Value("current_user_id").(int64)
	if !ok {
		return nil, fmt.Errorf("internal error: missing user context")
	}
	// 查询双方发送的消息 排序+限制条数
	messages, err := dao.GetHistoryBetweenUsers(currentUserID, targetUser.UserId, queryStart, queryEnd, req.Limit)
	if err != nil {
		return nil, fmt.Errorf("查询数据库失败: %v", err)
	}
	if len(messages) == 0 {
		return &SearHistoryResp{Result: fmt.Sprintf("未找到你和 %s 之间的聊天记录。", targetUser.Username)}, nil
	}
	var rawText string
	for _, msg := range messages {
		senderName := ""
		// 如果是接收者的消息
		if msg.SenderId == targetUser.UserId {
			senderName = targetUser.Username
		} else {
			// 如果是发送者的消息
			senderName = "你"
		}
		rawText += fmt.Sprintf("[%s]: %s\n", senderName, msg.Content)
	}

	return &SearHistoryResp{Result: fmt.Sprintf("以下是原始聊天记录，请根据用户的需求进行总结或提取:\n%s", rawText)}, nil
}

func SearchHistoryTool() (tool.InvokableTool, error) {
	return toolutils.InferTool(
		"search_chat_history", // Tool 名称
		"查询与特定用户的历史聊天记录，支持时间范围和关键词过滤", // Tool 描述
		searchHistoryInvoker, // 执行函数
	)
}

func MustSearchHistoryTool() tool.InvokableTool {
	t, err := SearchHistoryTool()
	if err != nil {
		// 这里的 panic 是合理的，因为工具创建失败意味着 Agent 根本无法工作
		panic(fmt.Sprintf("failed to init search history tool: %v", err))
	}
	return t
}
