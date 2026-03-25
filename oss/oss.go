// Package oss provides object storage operations.
package oss

import (
	"context"
	"fmt"
	"io"
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

	// ListUnderPrefix returns all object keys under the given prefix (e.g. "evaluations/123/cases/0/").
	// Returns keys like "evaluations/123/cases/0/input.json", "evaluations/123/cases/0/logs.txt".
	// Not all implementations support this (e.g. Tencent COS stub may return error).
	ListUnderPrefix(ctx context.Context, prefix string) ([]string, error)
}

// SignedURLVerifier is an optional interface for storages that verify signed URLs locally.
// Local storage implements this; cloud storages typically don't need it (CDN handles verification).
type SignedURLVerifier interface {
	VerifySignedURL(objectKey string, expires int64, signature string) bool
}

// Config contains OSS configuration.
type Config struct {
	Provider   string // "local", "tencent", "aliyun", "s3"
	Bucket     string
	Region     string
	SecretID   string // Cloud provider credentials
	SecretKey  string // Cloud provider credentials
	SigningKey string // Key for signing URLs (should be different from cloud credentials)
	BaseURL    string // For local storage or custom domain
	LocalPath  string // For local storage only
}

// New creates a new Storage instance based on configuration.
func New(cfg Config) (Storage, error) {
	switch cfg.Provider {
	case "local":
		return NewLocalStorage(cfg.LocalPath, cfg.BaseURL, cfg.SigningKey)
	case "tencent":
		return NewTencentCOS(cfg)
	default:
		return nil, fmt.Errorf("unsupported storage provider: %s", cfg.Provider)
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
// extOrLang can be either:
//   - A file extension starting with "." (e.g., ".zip", ".py")
//   - A language name (e.g., "python", "go") which will be mapped to its extension
func GenerateObjectKey(userID, phaseID, timestamp int64, extOrLang string) string {
	ext := extOrLang
	if len(extOrLang) > 0 && extOrLang[0] != '.' {
		// 是语言名，查表
		ext = LanguageExtensions[extOrLang]
		if ext == "" {
			ext = ".code"
		}
	}
	return fmt.Sprintf("submissions/%d/%d/%d%s", userID, phaseID, timestamp, ext)
}
