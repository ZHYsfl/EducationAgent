package server

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"multimodal-teaching-agent/oss"
)

// NewAppForTest builds app with injectable dependencies for tests.
func NewAppForTest(db *gorm.DB, storage oss.Storage) *App {
	a := &App{
		db:             db,
		storage:        storage,
		searchProvider: "serpapi",
		searchStrategy: "merge",
		searchTimeout:  15 * time.Second,
		httpCallback:   &http.Client{Timeout: 10 * time.Second},
	}
	a.searchProviders, _ = buildSearchProviders("", a.searchProvider, "", "", "https://metaso.cn/api/open/search")
	return a
}

// Exported handler wrappers for cross-package tests.
func (a *App) UploadFile(c *gin.Context)   { a.uploadFile(c) }
func (a *App) GetFile(c *gin.Context)      { a.getFile(c) }
func (a *App) DeleteFile(c *gin.Context)   { a.deleteFile(c) }
func (a *App) CreateSession(c *gin.Context) { a.createSession(c) }
func (a *App) GetSession(c *gin.Context)   { a.getSession(c) }
func (a *App) ListSessions(c *gin.Context) { a.listSessions(c) }
func (a *App) UpdateSession(c *gin.Context) { a.updateSession(c) }
func (a *App) SearchQuery(c *gin.Context)  { a.searchQuery(c) }
func (a *App) SearchResult(c *gin.Context) { a.searchResult(c) }

// Exported service wrappers for tests.
func (a *App) FetchSearchResults(query string, maxResults int, language string) ([]SearchResultItem, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), a.searchTimeout)
	defer cancel()
	return a.fetchSearchResults(ctx, query, maxResults, language)
}

func (a *App) SearchBySerpAPI(query string, maxResults int, language string) ([]SearchResultItem, string, error) {
	p := SerpAPIProvider{apiKey: a.serpAPIKey, client: &http.Client{Timeout: 15 * time.Second}}
	items, err := p.Search(context.Background(), query, maxResults, language)
	if err != nil {
		return nil, "", err
	}
	return items, buildSummary(query, items), nil
}
