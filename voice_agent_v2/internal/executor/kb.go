package executor

import (
	"context"
	"fmt"
	"log"

	"voiceagentv2/internal/types"
)

func (e *Executor) executeKBQuery(ctx context.Context, params map[string]string, sessionCtx SessionContext) string {
	if e.clients == nil {
		return "Error: KB service not available"
	}

	req := types.KBQueryRequest{
		UserID:         sessionCtx.UserID,
		SessionID:      sessionCtx.SessionID,
		Query:          params["query"],
		TopK:           5,
		ScoreThreshold: 0.5,
	}

	// 异步调用：立即返回，不等待结果
	// KB服务会异步处理并通过回调返回结果
	_, err := e.clients.QueryKB(ctx, req)
	if err != nil {
		log.Printf("[executor] kb_query error: %v", err)
		return fmt.Sprintf("KB query failed: %v", err)
	}

	return "KB query accepted. Retrieving related knowledge..."
}
