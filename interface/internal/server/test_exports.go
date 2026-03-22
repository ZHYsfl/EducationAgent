package server

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"multimodal-teaching-agent/oss"
)

// NewAppForTest builds app with injectable dependencies for tests.
func NewAppForTest(db *gorm.DB, storage oss.Storage) *App {
	return &App{db: db, storage: storage, searchProvider: "serpapi"}
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
func (a *App) FetchSearchResults(query string, maxResults int, language, searchType string) ([]SearchResultItem, string, error) {
	return a.fetchSearchResults(query, maxResults, language, searchType)
}

func (a *App) SearchBySerpAPI(query string, maxResults int, language, searchType string) ([]SearchResultItem, string, error) {
	return a.searchBySerpAPI(query, maxResults, language, searchType)
}
