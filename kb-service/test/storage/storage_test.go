// Package storage_test storage 层黑盒测试（无外部依赖）
package storage_test

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"kb-service/internal/storage"
)

func newOSS(t *testing.T) storage.ObjectStorage {
	t.Helper()
	oss, err := storage.NewLocalStorage(t.TempDir(), "http://localhost:9200/storage")
	if err != nil {
		t.Fatalf("NewLocalStorage: %v", err)
	}
	return oss
}

func TestLocalStorage_PutAndGet(t *testing.T) {
	oss := newOSS(t)
	ctx := context.Background()
	content := "hello, world"

	url, err := oss.Put(ctx, "test/hello.txt", strings.NewReader(content))
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}
	if url == "" {
		t.Errorf("Put 应返回非空 URL")
	}

	rc, err := oss.Get(ctx, "test/hello.txt")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	defer rc.Close()
	got, _ := io.ReadAll(rc)
	if string(got) != content {
		t.Errorf("内容不匹配，期望 %q，得到 %q", content, got)
	}
}

func TestLocalStorage_Exists(t *testing.T) {
	oss := newOSS(t)
	ctx := context.Background()

	exists, err := oss.Exists(ctx, "not/exist.txt")
	if err != nil {
		t.Fatalf("Exists error: %v", err)
	}
	if exists {
		t.Errorf("不存在的文件 Exists 应返回 false")
	}

	_, _ = oss.Put(ctx, "exist/file.txt", strings.NewReader("data"))
	exists, err = oss.Exists(ctx, "exist/file.txt")
	if err != nil {
		t.Fatalf("Exists error: %v", err)
	}
	if !exists {
		t.Errorf("已存在文件 Exists 应返回 true")
	}
}

func TestLocalStorage_Delete(t *testing.T) {
	oss := newOSS(t)
	ctx := context.Background()

	// 删除不存在的文件不报错
	if err := oss.Delete(ctx, "no/file.txt"); err != nil {
		t.Errorf("删除不存在文件应静默，得到: %v", err)
	}

	_, _ = oss.Put(ctx, "del/file.txt", strings.NewReader("x"))
	if err := oss.Delete(ctx, "del/file.txt"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	exists, _ := oss.Exists(ctx, "del/file.txt")
	if exists {
		t.Errorf("Delete 后文件应不存在")
	}
}

func TestLocalStorage_List(t *testing.T) {
	oss := newOSS(t)
	ctx := context.Background()

	_, _ = oss.Put(ctx, "dir/a.txt", strings.NewReader("a"))
	_, _ = oss.Put(ctx, "dir/b.txt", strings.NewReader("b"))
	_, _ = oss.Put(ctx, "other/c.txt", strings.NewReader("c"))

	keys, err := oss.List(ctx, "dir")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(keys) != 2 {
		t.Errorf("期望 2 个文件，得到 %d：%v", len(keys), keys)
	}
}

func TestLocalStorage_URLFormat(t *testing.T) {
	oss := newOSS(t)
	url, err := oss.Put(context.Background(), "user/doc/file.pdf", bytes.NewReader([]byte("%PDF")))
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}
	if !strings.Contains(url, "user/doc/file.pdf") {
		t.Errorf("URL 应包含 key，得到: %s", url)
	}
	if !strings.HasPrefix(url, "http://") {
		t.Errorf("URL 应以 http:// 开头，得到: %s", url)
	}
}
