// Package handler 包含 Reranker 接口定义，供 query.go 使用。
// 具体实现在 reranker 包中。
package handler

import (
	"context"
	"kb-service/internal/model"
)

// Reranker 重排序接口
type Reranker interface {
	Rerank(ctx context.Context, query string, chunks []model.RetrievedChunk) ([]model.RetrievedChunk, error)
}
