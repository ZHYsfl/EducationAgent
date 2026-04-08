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

	_, err := e.clients.QueryKB(ctx, req)
	if err != nil {
		log.Printf("[executor] kb_query error: %v", err)
		return fmt.Sprintf("kb_query失败: %v", err)
	}
	return "kb_query已发送，等待检索结果"
}
