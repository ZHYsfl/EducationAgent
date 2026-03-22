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
func NewLocalStorage(basePath, baseURL, signingKey string) (*LocalStorage, error) {
	if basePath == "" {
		basePath = "./storage"
	}
	if err := os.MkdirAll(basePath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create storage directory: %w", err)
	}

	key := signingKey
	if key == "" {
		key = "local-dev-signing-key-change-in-production"
	}

	return &LocalStorage{
		basePath:   basePath,
		baseURL:    strings.TrimSuffix(baseURL, "/"),
		signingKey: []byte(key),
	}, nil
}

func copyWithContext(ctx context.Context, dst io.Writer, src io.Reader) (int64, error) {
	buf := make([]byte, copyBufferSize)
	var written int64

	for {
		select {
		case <-ctx.Done():
			return written, ctx.Err()
		default:
		}

		nr, readErr := src.Read(buf)
		if nr > 0 {
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

		if readErr != nil {
			if readErr == io.EOF {
				return written, nil
			}
			return written, readErr
		}
	}
}

func (s *LocalStorage) Upload(ctx context.Context, objectKey string, reader io.Reader, size int64) error {
	fullPath := filepath.Join(s.basePath, objectKey)

	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	file, err := os.Create(fullPath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}

	_, err = copyWithContext(ctx, file, reader)
	closeErr := file.Close()

	if err != nil {
		_ = os.Remove(fullPath)
		return fmt.Errorf("failed to write file: %w", err)
	}
	if closeErr != nil {
		return fmt.Errorf("failed to close file: %w", closeErr)
	}
	return nil
}

func (s *LocalStorage) Download(ctx context.Context, objectKey string) (io.ReadCloser, error) {
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

func (s *LocalStorage) Delete(ctx context.Context, objectKey string) error {
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

func (s *LocalStorage) GenerateSignedURL(objectKey string, expiry time.Duration) (string, error) {
	return s.GenerateSignedURLWithBase("", objectKey, expiry)
}

func (s *LocalStorage) GenerateSignedURLWithBase(baseURL string, objectKey string, expiry time.Duration) (string, error) {
	expires := time.Now().Add(expiry).Unix()
	message := fmt.Sprintf("%s:%d", objectKey, expires)
	h := hmac.New(sha256.New, s.signingKey)
	h.Write([]byte(message))
	signature := base64.URLEncoding.EncodeToString(h.Sum(nil))

	base := s.baseURL
	if baseURL != "" {
		base = strings.TrimSuffix(baseURL, "/")
	}

	signedURL := fmt.Sprintf("%s/oss/%s?expires=%d&signature=%s",
		base,
		url.PathEscape(objectKey),
		expires,
		url.QueryEscape(signature),
	)
	return signedURL, nil
}

func (s *LocalStorage) Exists(ctx context.Context, objectKey string) (bool, error) {
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

func (s *LocalStorage) VerifySignedURL(objectKey string, expires int64, signature string) bool {
	if time.Now().Unix() > expires {
		return false
	}
	message := fmt.Sprintf("%s:%d", objectKey, expires)
	h := hmac.New(sha256.New, s.signingKey)
	h.Write([]byte(message))
	expectedSig := base64.URLEncoding.EncodeToString(h.Sum(nil))
	return hmac.Equal([]byte(signature), []byte(expectedSig))
}
