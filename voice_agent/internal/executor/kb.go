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
		UserID:         sessionCtx.UserID,
		Query:          params["query"],
		TopK:           5,
		ScoreThreshold: 0.5,
	}

	// 异步调用：立即返回，不等待结果
	// KB服务会异步处理并通过回调返回结果
	_, err := e.clients.QueryKB(ctx, req)
	if err != nil {
		log.Printf("[executor] kb_query error: %v", err)
		return fmt.Sprintf("知识库查询失败: %v", err)
	}

	return "知识库查询任务已创建，正在检索相关知识..."
}
