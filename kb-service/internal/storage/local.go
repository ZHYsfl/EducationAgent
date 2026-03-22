// Package storage 本地文件系统实现
package storage

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// LocalStorage 本地文件系统存储实现
type LocalStorage struct {
	basePath string // 本地存储根目录，如 /data/kb-storage
	baseURL  string // 访问 URL 前缀，如 http://localhost:9200/storage
}

// NewLocalStorage 创建本地存储实例
// basePath: 本地存储根目录（会自动创建）
// baseURL: HTTP 访问前缀，用于生成可访问的 URL
func NewLocalStorage(basePath, baseURL string) (*LocalStorage, error) {
	// 创建根目录
	if err := os.MkdirAll(basePath, 0755); err != nil {
		return nil, fmt.Errorf("create storage dir: %w", err)
	}
	return &LocalStorage{
		basePath: basePath,
		baseURL:  strings.TrimSuffix(baseURL, "/"),
	}, nil
}

// Put 上传文件到本地存储
// key 格式：user_id/doc_id/filename，自动创建目录
func (ls *LocalStorage) Put(ctx context.Context, key string, data io.Reader) (string, error) {
	fullPath := filepath.Join(ls.basePath, key)
	dir := filepath.Dir(fullPath)

	// 创建目录
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("create dir: %w", err)
	}

	// 写入文件
	f, err := os.Create(fullPath)
	if err != nil {
		return "", fmt.Errorf("create file: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(f, data); err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}

	// 返回可访问的 URL
	url := fmt.Sprintf("%s/%s", ls.baseURL, key)
	return url, nil
}

// Get 从本地存储下载文件
func (ls *LocalStorage) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	fullPath := filepath.Join(ls.basePath, key)
	f, err := os.Open(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("file not found: %s", key)
		}
		return nil, fmt.Errorf("open file: %w", err)
	}
	return f, nil
}

// Delete 删除本地存储中的文件
func (ls *LocalStorage) Delete(ctx context.Context, key string) error {
	fullPath := filepath.Join(ls.basePath, key)
	if err := os.Remove(fullPath); err != nil {
		if os.IsNotExist(err) {
			return nil // 文件不存在视为成功
		}
		return fmt.Errorf("delete file: %w", err)
	}
	return nil
}

// Exists 检查文件是否存在
func (ls *LocalStorage) Exists(ctx context.Context, key string) (bool, error) {
	fullPath := filepath.Join(ls.basePath, key)
	_, err := os.Stat(fullPath)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// List 列出指定前缀的所有文件
func (ls *LocalStorage) List(ctx context.Context, prefix string) ([]string, error) {
	prefixPath := filepath.Join(ls.basePath, prefix)
	var keys []string

	err := filepath.Walk(prefixPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			// 转换为相对路径（key 格式）
			relPath, _ := filepath.Rel(ls.basePath, path)
			// 统一使用 / 分隔符
			key := filepath.ToSlash(relPath)
			keys = append(keys, key)
		}
		return nil
	})

	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("list files: %w", err)
	}
	return keys, nil
}
