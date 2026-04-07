package executor

import (
	"context"
	"fmt"
	"log"
	"strings"

	"voiceagentv2/internal/types"
)

func (e *Executor) executeGetMemory(ctx context.Context, params map[string]string, sessionCtx SessionContext) string {
	if e.clients == nil {
		return "Error: memory service not available"
	}

	query := strings.TrimSpace(params["query"])
	if query == "" {
		return "Error: get_memory requires query"
	}

	req := types.MemoryRecallRequest{
		UserID:    sessionCtx.UserID,
		SessionID: sessionCtx.SessionID,
		Query:     query,
		TopK:      10,
	}

	_, err := e.clients.RecallMemory(ctx, req)
	if err != nil {
		log.Printf("[executor] get_memory error: %v", err)
		return fmt.Sprintf("Memory recall request failed: %v", err)
	}

	return "Memory recall accepted. Waiting for memory callback..."
}
