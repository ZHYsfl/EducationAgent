package server

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func ok(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, APIResponse{Code: 200, Message: "success", Data: data})
}

func fail(c *gin.Context, code int, message string) {
	c.JSON(http.StatusOK, APIResponse{Code: code, Message: message, Data: nil})
}

func nowMs() int64 { return time.Now().UnixMilli() }
func newID(prefix string) string { return prefix + uuid.NewString() }

func isAllowedPurpose(v string) bool {
	switch v {
	case "reference", "export", "knowledge_base", "render":
		return true
	default:
		return false
	}
}

func isAllowedSessionStatus(v string) bool {
	switch v {
	case "active", "completed", "archived":
		return true
	default:
		return false
	}
}

func detectFileType(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".pdf":
		return "pdf"
	case ".docx":
		return "docx"
	case ".pptx":
		return "pptx"
	case ".jpg", ".jpeg", ".png", ".webp":
		return "image"
	case ".mp4", ".webm":
		return "video"
	case ".html", ".htm":
		return "html"
	default:
		return "other"
	}
}

func parsePositiveInt(s string, def int) int {
	v, err := strconv.Atoi(s)
	if err != nil || v <= 0 {
		return def
	}
	return v
}

func parseUserID(auth string) string {
	if strings.HasPrefix(auth, "Bearer ") {
		v := strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
		if strings.HasPrefix(v, "user_") {
			return v
		}
	}
	return ""
}

func emptyToNil(v string) *string {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	return &v
}

func getenv(k, d string) string {
	v := strings.TrimSpace(os.Getenv(k))
	if v == "" {
		return d
	}
	return v
}

func toString(v interface{}) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func titleFromSnippet(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "搜索结果"
	}
	if len([]rune(s)) > 20 {
		r := []rune(s)
		return string(r[:20]) + "..."
	}
	return s
}

func titleOrDefault(title, snippet string) string {
	title = strings.TrimSpace(title)
	if title != "" {
		return title
	}
	return titleFromSnippet(snippet)
}

func refineSnippet(snippet, query string) string {
	s := strings.TrimSpace(snippet)
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "<", "")
	s = strings.ReplaceAll(s, ">", "")
	if s == "" {
		return fmt.Sprintf("与“%s”相关的检索结果摘要。", query)
	}
	if len([]rune(s)) > 180 {
		r := []rune(s)
		s = string(r[:180]) + "..."
	}
	return s
}

func buildSummary(query string, items []SearchResultItem) string {
	if len(items) == 0 {
		return ""
	}
	return fmt.Sprintf("围绕“%s”检索到 %d 条结果，已完成摘要精炼，可直接用于回注与知识库沉淀。", query, len(items))
}

func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "unknown"
	}
	return u.Host
}

// Exported helpers for cross-package tests.
func ParseUserID(auth string) string               { return parseUserID(auth) }
func DetectFileType(name string) string            { return detectFileType(name) }
func ParsePositiveInt(s string, def int) int       { return parsePositiveInt(s, def) }
func HostOf(raw string) string                     { return hostOf(raw) }
func TitleOrDefault(title, snippet string) string  { return titleOrDefault(title, snippet) }
func IsAllowedPurpose(v string) bool               { return isAllowedPurpose(v) }
func IsAllowedSessionStatus(v string) bool         { return isAllowedSessionStatus(v) }
func RefineSnippet(snippet, query string) string   { return refineSnippet(snippet, query) }
func BuildSummary(query string, items []SearchResultItem) string { return buildSummary(query, items) }
func EmptyToNil(v string) *string                  { return emptyToNil(v) }
func ToString(v interface{}) string                { return toString(v) }
