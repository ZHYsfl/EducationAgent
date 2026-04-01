package oss

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"
)

// Storage defines the interface for object storage operations.
type Storage interface {
	// Upload uploads data to the specified object key.
	Upload(ctx context.Context, objectKey string, reader io.Reader, size int64) error

	// Download downloads data from the specified object key.
	Download(ctx context.Context, objectKey string) (io.ReadCloser, error)

	// Delete deletes the object at the specified key.
	Delete(ctx context.Context, objectKey string) error

	// GenerateSignedURL generates a signed URL for temporary access.
	GenerateSignedURL(objectKey string, expiry time.Duration) (string, error)

	// Exists checks if an object exists.
	Exists(ctx context.Context, objectKey string) (bool, error)

	// ListUnderPrefix returns all object keys under the given prefix.
	ListUnderPrefix(ctx context.Context, prefix string) ([]string, error)
}

// SignedURLVerifier is an optional interface for storages that verify signed URLs locally.
// Local storage implements this; cloud storages typically don't need it.
type SignedURLVerifier interface {
	VerifySignedURL(objectKey string, expires int64, signature string) bool
}

// Config contains OSS configuration.
type Config struct {
	Provider   string // only "local" is supported
	SigningKey string // Key for signing URLs
	BaseURL    string // For local storage or custom domain
	LocalPath  string // For local storage only
}

// New creates a new Storage instance based on configuration.
func New(cfg Config) (Storage, error) {
	provider := strings.ToLower(strings.TrimSpace(cfg.Provider))
	switch provider {
	case "", "local":
		return NewLocalStorage(cfg.LocalPath, cfg.BaseURL, cfg.SigningKey)
	default:
		return nil, fmt.Errorf("unsupported storage provider: %s (only local is available)", cfg.Provider)
	}
}

// LanguageExtensions maps programming languages to file extensions.
var LanguageExtensions = map[string]string{
	"python":     ".py",
	"go":         ".go",
	"javascript": ".js",
	"typescript": ".ts",
	"java":       ".java",
	"c":          ".c",
	"cpp":        ".cpp",
	"rust":       ".rs",
}

// GenerateObjectKey generates a standardized object key for code submissions.
// extOrLang can be extension (".zip", ".py") or language name ("python", "go").
func GenerateObjectKey(userID, phaseID, timestamp int64, extOrLang string) string {
	ext := extOrLang
	if len(extOrLang) > 0 && extOrLang[0] != '.' {
		ext = LanguageExtensions[extOrLang]
		if ext == "" {
			ext = ".code"
		}
	}
	return fmt.Sprintf("submissions/%d/%d/%d%s", userID, phaseID, timestamp, ext)
}
