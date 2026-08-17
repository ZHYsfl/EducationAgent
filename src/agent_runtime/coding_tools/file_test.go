package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "test.txt")
	content := "line1\nline2\nline3\nline4\nline5"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	out, err := ReadFile(context.Background(), path, 0, 0, 0)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if out != content {
		t.Fatalf("expected %q, got %q", content, out)
	}
}

func TestReadFileRange(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "test.txt")
	content := "line1\nline2\nline3\nline4\nline5"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	out, err := ReadFile(context.Background(), path, 2, 4, 0)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	expected := "line2\nline3\nline4"
	if out != expected {
		t.Fatalf("expected %q, got %q", expected, out)
	}
}

func TestReadFileMaxResponseLen(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "test.txt")
	content := strings.Repeat("a", 200)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	out, err := ReadFile(context.Background(), path, 0, 0, 50)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if !strings.HasSuffix(out, "[truncated by max_response_len]") {
		t.Fatalf("expected truncation marker, got %q", out)
	}
	prefix := strings.TrimSuffix(out, "\n[truncated by max_response_len]")
	if len([]rune(prefix)) != 50 {
		t.Fatalf("expected 50 runes before marker, got %d", len([]rune(prefix)))
	}
}

func TestReadFileNotFound(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "missing.txt")
	_, err := ReadFile(context.Background(), path, 0, 0, 0)
	if err == nil {
		t.Fatalf("expected error for missing file")
	}
}

func TestEditFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "test.txt")
	if err := os.WriteFile(path, []byte("foo bar baz"), 0644); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	msg, err := EditFile(context.Background(), path, "bar", "qux")
	if err != nil {
		t.Fatalf("EditFile failed: %v", err)
	}
	if msg != "successfully edited file" {
		t.Fatalf("unexpected message: %q", msg)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	if string(content) != "foo qux baz" {
		t.Fatalf("expected 'foo qux baz', got %q", string(content))
	}
}

func TestEditFileOldStringNotFound(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "test.txt")
	if err := os.WriteFile(path, []byte("foo bar baz"), 0644); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	_, err := EditFile(context.Background(), path, "missing", "qux")
	if err == nil {
		t.Fatalf("expected error when old_string not found")
	}
}
