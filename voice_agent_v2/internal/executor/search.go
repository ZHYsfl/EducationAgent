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

	_, err := e.clients.SearchWeb(ctx, req)
	if err != nil {
		log.Printf("[executor] web_search error: %v", err)
		return fmt.Sprintf("web_search失败: %v", err)
	}
	return "web_search已发送，等待搜索结果"
}
