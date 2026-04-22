package handler

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"

	"educationagent/internal/state"

	"github.com/gin-gonic/gin"
)

// PPTLogStream handles GET /api/v1/ppt/log-stream (SSE).
func PPTLogStream(st *state.AppState) gin.HandlerFunc {
	return func(c *gin.Context) {
		ch := st.SubscribePPTLog()
		defer st.UnsubscribePPTLog(ch)

		c.Writer.Header().Set("Content-Type", "text/event-stream")
		c.Writer.Header().Set("Cache-Control", "no-cache")
		c.Writer.Header().Set("Connection", "keep-alive")
		c.Writer.WriteHeader(http.StatusOK)

		for {
			select {
			case msg, ok := <-ch:
				if !ok {
					return
				}
				data, _ := json.Marshal(msg)
				c.Writer.Write([]byte("data: "))
				c.Writer.Write(data)
				c.Writer.Write([]byte("\n\n"))
				c.Writer.Flush()
			case <-c.Request.Context().Done():
				return
			}
		}
	}
}

// FSList handles GET /api/v1/fs/list?path=...
func FSList(st *state.AppState) gin.HandlerFunc {
	return func(c *gin.Context) {
		root := "/root/autodl-tmp/workspace"
		type entry struct {
			Name  string `json:"name"`
			Path  string `json:"path"`
			IsDir bool   `json:"isDir"`
		}
		var entries []entry
		filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || path == root {
				return nil
			}
			if d.IsDir() && (d.Name() == "node_modules" || d.Name() == ".git") {
				return fs.SkipDir
			}
			rel, _ := filepath.Rel(root, path)
			entries = append(entries, entry{Name: d.Name(), Path: rel, IsDir: d.IsDir()})
			return nil
		})
		c.JSON(http.StatusOK, entries)
	}
}

// FSDownload handles GET /api/v1/fs/download?path=...
func FSDownload(st *state.AppState) gin.HandlerFunc {
	return func(c *gin.Context) {
		rel := c.Query("path")
		if rel == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "path required"})
			return
		}
		full := filepath.Join("/root/autodl-tmp/workspace", rel)
		if !filepath.HasPrefix(full, "/root/autodl-tmp/workspace") {
			c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
			return
		}
		c.FileAttachment(full, filepath.Base(full))
	}
}

// FSRead handles GET /api/v1/fs/read?path=...
func FSRead(st *state.AppState) gin.HandlerFunc {
	return func(c *gin.Context) {
		rel := c.Query("path")
		if rel == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "path required"})
			return
		}
		full := filepath.Join("/root/autodl-tmp/workspace", rel)
		// Prevent path traversal
		if !filepath.HasPrefix(full, "/root/autodl-tmp/workspace") {
			c.JSON(http.StatusForbidden, gin.H{"error": "forbidden"})
			return
		}
		data, err := os.ReadFile(full)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"content": string(data)})
	}
}
