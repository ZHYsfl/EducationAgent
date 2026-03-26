package server

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"multimodal-teaching-agent/oss"
)

func escapeObjectKeyForURL(objectKey string) string {
	objectKey = strings.TrimPrefix(objectKey, "/")
	if objectKey == "" {
		return ""
	}
	parts := strings.Split(objectKey, "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return strings.Join(parts, "/")
}

func joinURL(base, p string) string {
	base = strings.TrimSuffix(strings.TrimSpace(base), "/")
	p = strings.TrimPrefix(strings.TrimSpace(p), "/")
	if base == "" {
		return "/" + p
	}
	if p == "" {
		return base
	}
	return base + "/" + p
}

func (a *App) publicObjectURL(objectKey string) string {
	escaped := escapeObjectKeyForURL(objectKey)
	if a.ossPublicBaseURL != "" {
		return joinURL(a.ossPublicBaseURL, escaped)
	}

	switch strings.ToLower(a.ossProvider) {
	case "", "local":
		return joinURL(a.ossBaseURL, "oss/"+escaped)
	case "minio", "s3":
		scheme := "http"
		if a.ossUseSSL {
			scheme = "https"
		}
		if a.ossEndpoint != "" && a.ossBucket != "" {
			return fmt.Sprintf("%s://%s/%s/%s", scheme, strings.TrimSuffix(a.ossEndpoint, "/"), url.PathEscape(a.ossBucket), escaped)
		}
		return ""
	default:
		return ""
	}
}

func (a *App) serveOSSObject(c *gin.Context) {
	// Only meaningful for local storage. For MinIO/S3, signed URLs are served by the storage endpoint itself.
	if strings.ToLower(a.ossProvider) != "" && strings.ToLower(a.ossProvider) != "local" {
		c.Status(http.StatusNotFound)
		return
	}

	objectKey := strings.TrimPrefix(c.Param("object_key"), "/")
	if objectKey == "" {
		c.Status(http.StatusBadRequest)
		return
	}

	expiresStr := strings.TrimSpace(c.Query("expires"))
	sig := strings.TrimSpace(c.Query("signature"))
	if expiresStr == "" || sig == "" {
		if !a.ossAllowUnsigned {
			c.Status(http.StatusForbidden)
			return
		}
	} else {
		expires, err := strconv.ParseInt(expiresStr, 10, 64)
		if err != nil {
			c.Status(http.StatusBadRequest)
			return
		}
		if v, ok := a.storage.(oss.SignedURLVerifier); ok {
			if !v.VerifySignedURL(objectKey, expires, sig) {
				c.Status(http.StatusForbidden)
				return
			}
		} else {
			c.Status(http.StatusForbidden)
			return
		}
	}

	r, err := a.storage.Download(c.Request.Context(), objectKey)
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	defer r.Close()

	// Peek first bytes to set Content-Type.
	buf := make([]byte, 512)
	n, _ := io.ReadFull(r, buf)
	contentType := http.DetectContentType(buf[:max(0, n)])
	if n <= 0 {
		contentType = "application/octet-stream"
	}
	reader := io.MultiReader(bytes.NewReader(buf[:max(0, n)]), r)

	c.Header("Content-Type", contentType)
	c.Status(http.StatusOK)
	_, _ = io.Copy(c.Writer, reader)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

