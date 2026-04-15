package service

import "context"

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

// SearchWeb returns a hard-coded stub response.
// TODO: zhongtianyi
func (s *DefaultSearchService) SearchWeb(ctx context.Context, query string) (string, error) {
	return "summary of search results for: " + query, nil
}
