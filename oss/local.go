// Package oss provides object storage operations.
package oss

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// copyBufferSize is the buffer size for chunked copy operations.
const copyBufferSize = 32 * 1024 // 32KB chunks

// LocalStorage implements Storage interface using local file system.
// Useful for development and testing.
type LocalStorage struct {
	basePath   string // Local directory to store files
	baseURL    string // Base URL for generating signed URLs
	signingKey []byte // Key for signing URLs (different from cloud credentials)
}

// NewLocalStorage creates a new local storage instance.
// signingKey is used for signing URLs; should be different from JWT secret and cloud credentials.
func NewLocalStorage(basePath, baseURL, signingKey string) (*LocalStorage, error) {
	// Create base directory if not exists
	if err := os.MkdirAll(basePath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create storage directory: %w", err)
	}

	// Use provided signing key or default for development
	key := signingKey
	if key == "" {
		key = "local-dev-signing-key-change-in-production"
	}

	return &LocalStorage{
		basePath:   basePath,
		baseURL:    baseURL,
		signingKey: []byte(key),
	}, nil
}

// copyWithContext copies data from reader to writer, checking context cancellation between chunks.
// This allows the operation to be cancelled if the context is done (e.g., user disconnects or timeout).
func copyWithContext(ctx context.Context, dst io.Writer, src io.Reader) (int64, error) {
	buf := make([]byte, copyBufferSize)
	var written int64

	for {
		// Check if context is cancelled before each read
		select {
		case <-ctx.Done():
			return written, ctx.Err()
		default:
			// Continue copying
		}

		// Read a chunk
		nr, readErr := src.Read(buf)
		if nr > 0 {
			// Write the chunk
			nw, writeErr := dst.Write(buf[:nr])
			if nw > 0 {
				written += int64(nw)
			}
			if writeErr != nil {
				return written, writeErr
			}
			if nr != nw {
				return written, io.ErrShortWrite
			}
		}

		// Handle read completion or error
		if readErr != nil {
			if readErr == io.EOF {
				return written, nil // Success - finished reading
			}
			return written, readErr
		}
	}
}

// Upload uploads data to the local file system.
// Supports context cancellation - if context is cancelled (timeout or user disconnect),
// the upload will stop and the partially written file will be cleaned up.
func (s *LocalStorage) Upload(ctx context.Context, objectKey string, reader io.Reader, size int64) error {
	fullPath := filepath.Join(s.basePath, objectKey)

	// Create parent directories
	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Create file
	file, err := os.Create(fullPath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}

	// Copy data with context awareness
	_, err = copyWithContext(ctx, file, reader)

	// Close file before potential deletion
	closeErr := file.Close()

	// If copy failed (including context cancellation), clean up partial file
	if err != nil {
		_ = os.Remove(fullPath) // Best effort cleanup
		return fmt.Errorf("failed to write file: %w", err)
	}

	if closeErr != nil {
		return fmt.Errorf("failed to close file: %w", closeErr)
	}

	return nil
}

// Download downloads data from the local file system.
// Returns an io.ReadCloser that the caller must close.
func (s *LocalStorage) Download(ctx context.Context, objectKey string) (io.ReadCloser, error) {
	// Check context before starting
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	fullPath := filepath.Join(s.basePath, objectKey)

	file, err := os.Open(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("object not found: %s", objectKey)
		}
		return nil, fmt.Errorf("failed to open file: %w", err)
	}

	return file, nil
}

// Delete deletes a file from local storage.
func (s *LocalStorage) Delete(ctx context.Context, objectKey string) error {
	// Check context before starting
	if err := ctx.Err(); err != nil {
		return err
	}

	fullPath := filepath.Join(s.basePath, objectKey)

	err := os.Remove(fullPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete file: %w", err)
	}

	return nil
}

// GenerateSignedURL generates a signed URL for temporary access.
func (s *LocalStorage) GenerateSignedURL(objectKey string, expiry time.Duration) (string, error) {
	return s.GenerateSignedURLWithBase("", objectKey, expiry)
}

// GenerateSignedURLWithBase generates a signed URL using the given baseURL.
// If baseURL is empty, uses the storage's default base. Allows browser-facing URLs to use
// BASE_URL (public) and worker-facing URLs to use OSS_BASE_URL (e.g. 127.0.0.1 when co-located).
func (s *LocalStorage) GenerateSignedURLWithBase(baseURL string, objectKey string, expiry time.Duration) (string, error) {
	expires := time.Now().Add(expiry).Unix()

	// Create signature
	message := fmt.Sprintf("%s:%d", objectKey, expires)
	h := hmac.New(sha256.New, s.signingKey)
	h.Write([]byte(message))
	signature := base64.URLEncoding.EncodeToString(h.Sum(nil))

	base := s.baseURL
	if baseURL != "" {
		base = strings.TrimSuffix(baseURL, "/")
	}

	// Build signed URL
	signedURL := fmt.Sprintf("%s/oss/%s?expires=%d&signature=%s",
		base,
		url.PathEscape(objectKey),
		expires,
		url.QueryEscape(signature),
	)

	return signedURL, nil
}

// Exists checks if a file exists in local storage.
func (s *LocalStorage) Exists(ctx context.Context, objectKey string) (bool, error) {
	// Check context before starting
	if err := ctx.Err(); err != nil {
		return false, err
	}

	fullPath := filepath.Join(s.basePath, objectKey)

	_, err := os.Stat(fullPath)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, fmt.Errorf("failed to check file: %w", err)
}

// ListUnderPrefix returns all object keys under the given prefix.
func (s *LocalStorage) ListUnderPrefix(ctx context.Context, prefix string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	fullPath := filepath.Join(s.basePath, prefix)
	entries, err := os.ReadDir(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read directory %s: %w", prefix, err)
	}
	base := strings.TrimSuffix(prefix, "/")
	if base != "" {
		base += "/"
	}
	var keys []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		keys = append(keys, base+e.Name())
	}
	return keys, nil
}

// VerifySignedURL verifies a signed URL (for serving files).
func (s *LocalStorage) VerifySignedURL(objectKey string, expires int64, signature string) bool {
	// Check expiry
	if time.Now().Unix() > expires {
		return false
	}

	// Verify signature
	message := fmt.Sprintf("%s:%d", objectKey, expires)
	h := hmac.New(sha256.New, s.signingKey)
	h.Write([]byte(message))
	expectedSig := base64.URLEncoding.EncodeToString(h.Sum(nil))

	return hmac.Equal([]byte(signature), []byte(expectedSig))
}
