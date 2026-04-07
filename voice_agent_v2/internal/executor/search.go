package executor

import (
	"context"
	"fmt"
	"log"

	"voiceagentv2/internal/types"
)

func (e *Executor) executeWebSearch(ctx context.Context, params map[string]string, sessionCtx SessionContext) string {
	if e.clients == nil {
		return "Error: Search service not available"
	}

	req := types.SearchRequest{
		RequestID:  types.NewID("search_"),
		UserID:     sessionCtx.UserID,
		SessionID:  sessionCtx.SessionID,
		Query:      params["query"],
		MaxResults: 10,
		Language:   "zh",
	}

	// 异步调用：立即返回request_id，不等待结果
	results, err := e.clients.SearchWeb(ctx, req)
	if err != nil {
		log.Printf("[executor] web_search error: %v", err)
		return fmt.Sprintf("网络搜索失败: %v", err)
	}

	// 返回request_id，搜索服务会异步处理并通过回调返回结果
	return fmt.Sprintf("搜索任务已创建，RequestID: %s", results.RequestID)
}
