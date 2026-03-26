package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"voiceagent/internal/types"
)

func (e *Executor) executeWebSearch(ctx context.Context, params map[string]string, sessionCtx SessionContext) string {
	if e.clients == nil {
		return "Error: Search service not available"
	}

	req := types.SearchRequest{
		RequestID:  types.NewID("search_"),
		UserID:     sessionCtx.UserID,
		Query:      params["query"],
		MaxResults: 10,
		Language:   "zh",
		SearchType: "general",
	}

	results, err := e.clients.SearchWeb(ctx, req)
	if err != nil {
		log.Printf("[executor] web_search error: %v", err)
		return fmt.Sprintf("网络搜索失败: %v", err)
	}

	data, _ := json.Marshal(results)
	return fmt.Sprintf("搜索结果: %s", string(data))
}
