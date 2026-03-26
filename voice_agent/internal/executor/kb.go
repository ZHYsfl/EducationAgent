package executor

import (
	"context"
	"fmt"
	"log"

	"voiceagent/internal/types"
)

func (e *Executor) executeKBQuery(ctx context.Context, params map[string]string, sessionCtx SessionContext) string {
	if e.clients == nil {
		return "Error: KB service not available"
	}

	req := types.KBQueryRequest{
		Subject:        sessionCtx.Subject,
		UserID:         sessionCtx.UserID,
		Query:          params["q"],
		TopK:           5,
		ScoreThreshold: 0.5,
	}

	results, err := e.clients.QueryKB(ctx, req)
	if err != nil {
		log.Printf("[executor] kb_query error: %v", err)
		return fmt.Sprintf("知识库查询失败: %v", err)
	}

	return fmt.Sprintf("知识库查询结果: %s", results.Summary)
}
