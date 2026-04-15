package service

import (
	"context"
	"fmt"
	"strings"
)

// SearchService defines the web search interface.
type SearchService interface {
	SearchWeb(ctx context.Context, query string) (string, error)
}

// DefaultSearchService is a stub implementation for the MVP.
type DefaultSearchService struct{}

// NewSearchService creates a new default search service.
func NewSearchService() SearchService {
	return &DefaultSearchService{}
}

// SearchWeb queries the local knowledge base and returns a concise text summary.
func (s *DefaultSearchService) SearchWeb(ctx context.Context, query string) (string, error) {
	trimmedQuery := strings.TrimSpace(query)
	if trimmedQuery == "" {
		return "", fmt.Errorf("query cannot be empty")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

	kb := NewKBService()
	chunks, total, err := kb.QueryChunks(ctx, trimmedQuery)
	if err != nil {
		return "", err
	}
	if total == 0 {
		return fmt.Sprintf("No relevant results found for: %s", trimmedQuery), nil
	}

	maxItems := 3
	if len(chunks) < maxItems {
		maxItems = len(chunks)
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Found %d relevant results for: %s\n", total, trimmedQuery))
	for i := 0; i < maxItems; i++ {
		content := strings.TrimSpace(chunks[i].Content)
		content = strings.ReplaceAll(content, "\r", " ")
		content = strings.ReplaceAll(content, "\n", " ")
		content = strings.Join(strings.Fields(content), " ")
		if len(content) > 140 {
			content = content[:140] + "..."
		}
		b.WriteString(fmt.Sprintf("%d) %s", i+1, content))
		if i != maxItems-1 {
			b.WriteString("\n")
		}
	}
	return b.String(), nil
}
