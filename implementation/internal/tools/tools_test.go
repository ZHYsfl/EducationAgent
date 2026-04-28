package tools

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteAndReadFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "test.txt")

	ctx := context.Background()
	err := WriteFile(ctx, path, "hello world")
	require.NoError(t, err)

	content, err := ReadFile(ctx, path)
	require.NoError(t, err)
	assert.Equal(t, "hello world", content)
}

func TestAppendFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "a.txt")
	ctx := context.Background()
	require.NoError(t, AppendFile(ctx, path, "one"))
	require.NoError(t, AppendFile(ctx, path, "two"))
	content, err := ReadFile(ctx, path)
	require.NoError(t, err)
	assert.Equal(t, "onetwo", content)
}

func TestEditFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "test.txt")

	ctx := context.Background()
	require.NoError(t, WriteFile(ctx, path, "foo bar baz"))
	err := EditFile(ctx, path, "bar", "qux")
	require.NoError(t, err)

	content, err := ReadFile(ctx, path)
	require.NoError(t, err)
	assert.Equal(t, "foo qux baz", content)
}

func TestListDir(t *testing.T) {
	tmp := t.TempDir()
	ctx := context.Background()
	require.NoError(t, WriteFile(ctx, filepath.Join(tmp, "a.txt"), "a"))
	require.NoError(t, WriteFile(ctx, filepath.Join(tmp, "b.txt"), "b"))

	names, err := ListDir(ctx, tmp)
	require.NoError(t, err)
	assert.Len(t, names, 2)
}

func TestMoveFile(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "src.txt")
	dst := filepath.Join(tmp, "dst.txt")

	ctx := context.Background()
	require.NoError(t, WriteFile(ctx, src, "data"))
	require.NoError(t, MoveFile(ctx, src, dst))

	_, err := os.Stat(src)
	assert.True(t, os.IsNotExist(err))

	content, err := ReadFile(ctx, dst)
	require.NoError(t, err)
	assert.Equal(t, "data", content)
}

func TestExecuteCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping shell test on windows")
	}
	ctx := context.Background()
	stdout, stderr, err := ExecuteCommand(ctx, "echo hello", "")
	require.NoError(t, err)
	assert.Contains(t, stdout, "hello")
	assert.Empty(t, stderr)
}

func TestExecuteCommandCancellation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping shell test on windows")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, _, err := ExecuteCommand(ctx, "sleep 5", "")
	assert.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}
