// Package storage 提供对象存储抽象接口和实现。
// 支持本地文件系统存储和云 OSS（后续扩展）。
package storage

import (
	"context"
	"io"
)

// ObjectStorage 对象存储抽象接口
type ObjectStorage interface {
	// Put 上传对象，返回对象 URL
	Put(ctx context.Context, key string, data io.Reader) (url string, err error)

	// Get 下载对象
	Get(ctx context.Context, key string) (io.ReadCloser, error)

	// Delete 删除对象
	Delete(ctx context.Context, key string) error

	// Exists 检查对象是否存在
	Exists(ctx context.Context, key string) (bool, error)

	// List 列出指定前缀的所有对象
	List(ctx context.Context, prefix string) ([]string, error)
}
