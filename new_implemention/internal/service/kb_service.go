package service

import (
	"context"
	"educationagent/internal/model"
)

// KBService defines the knowledge-base query interface.
type KBService interface {
	QueryChunks(ctx context.Context, query string) ([]model.Chunk, int, error)
}

// DefaultKBService is a stub implementation for the MVP.
type DefaultKBService struct{}

// NewKBService creates a new default KB service.
func NewKBService() KBService {
	return &DefaultKBService{}
}

// QueryChunks returns a hard-coded stub response.
// TODO:duanyipeng
func (s *DefaultKBService) QueryChunks(ctx context.Context, query string) ([]model.Chunk, int, error) {
	return []model.Chunk{
		{ChunkID: "chunk-1", Content: "This is a stub chunk about " + query},
	}, 1, nil
}
