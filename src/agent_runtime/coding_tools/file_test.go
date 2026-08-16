package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "subdir", "test.txt")

	msg, err := WriteFile(context.Background(), path, "hello")
	if err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	if msg != "successfully wrote file" {
		t.Fatalf("unexpected success message: %q", msg)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read back file: %v", err)
	}
	if string(content) != "hello" {
		t.Fatalf("unexpected content: %q", string(content))
	}
}

func TestReadFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "test.txt")
	if err := os.WriteFile(path, []byte("world"), 0644); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	content, err := ReadFile(context.Background(), path)
	if err != nil {
		t.Fatalf("ReadFile failed: %v", err)
	}
	if content != "world" {
		t.Fatalf("unexpected content: %q", content)
	}
}

func TestReadFileNotFound(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "missing.txt")

	content, err := ReadFile(context.Background(), path)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if content != "" {
		t.Fatalf("expected empty string, got %q", content)
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
		t.Fatalf("unexpected success message: %q", msg)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read back file: %v", err)
	}
	if string(content) != "foo qux baz" {
		t.Fatalf("unexpected content: %q", string(content))
	}
}

func TestEditFileOldStringNotFound(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "test.txt")
	if err := os.WriteFile(path, []byte("foo bar baz"), 0644); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	msg, err := EditFile(context.Background(), path, "missing", "qux")
	if err == nil {
		t.Fatal("expected error when old_string not found")
	}
	if msg != "old_string not found in file" {
		t.Fatalf("unexpected message: %q", msg)
	}
}

func TestAppendFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "test.txt")

	_, err := WriteFile(context.Background(), path, "hello")
	if err != nil {
		t.Fatalf("setup WriteFile failed: %v", err)
	}

	msg, err := AppendFile(context.Background(), path, " world")
	if err != nil {
		t.Fatalf("AppendFile failed: %v", err)
	}
	if msg != "successfully appended to file" {
		t.Fatalf("unexpected success message: %q", msg)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read back file: %v", err)
	}
	if string(content) != "hello world" {
		t.Fatalf("unexpected content: %q", string(content))
	}
}

func TestAppendFileCreatesFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "new.txt")

	msg, err := AppendFile(context.Background(), path, "first")
	if err != nil {
		t.Fatalf("AppendFile failed: %v", err)
	}
	if msg != "successfully appended to file" {
		t.Fatalf("unexpected success message: %q", msg)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read back file: %v", err)
	}
	if string(content) != "first" {
		t.Fatalf("unexpected content: %q", string(content))
	}
}

func TestListDir(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "a.txt"), []byte(""), 0644); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "b.txt"), []byte(""), 0644); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	list, err := ListDir(context.Background(), tmp)
	if err != nil {
		t.Fatalf("ListDir failed: %v", err)
	}

	names := strings.Split(list, "\n")
	if len(names) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(names))
	}
	if names[0] != "a.txt" || names[1] != "b.txt" {
		t.Fatalf("unexpected entries: %v", names)
	}
}

func TestListDirNotFound(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing")

	list, err := ListDir(context.Background(), path)
	if err == nil {
		t.Fatal("expected error for missing directory")
	}
	if list != "" {
		t.Fatalf("expected empty string, got %q", list)
	}
}

func TestMoveFile(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src.txt")
	dst := filepath.Join(tmp, "subdir", "dst.txt")
	if err := os.WriteFile(src, []byte("move me"), 0644); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	msg, err := MoveFile(context.Background(), src, dst)
	if err != nil {
		t.Fatalf("MoveFile failed: %v", err)
	}
	if msg != "successfully moved file" {
		t.Fatalf("unexpected success message: %q", msg)
	}

	content, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("failed to read destination file: %v", err)
	}
	if string(content) != "move me" {
		t.Fatalf("unexpected content: %q", string(content))
	}

	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatal("source file should not exist after move")
	}
}
