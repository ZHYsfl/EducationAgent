package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// EditFile edits a file by replacing old_string with new_string.
func EditFile(ctx context.Context, path string, oldString string, newString string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}
	replaced := strings.Replace(string(content), oldString, newString, 1)
	if replaced == string(content) {
		return fmt.Errorf("old_string not found in file")
	}
	if err := os.WriteFile(path, []byte(replaced), 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}
	return nil
}

// WriteFile writes content to a file, creating or truncating it.
func WriteFile(ctx context.Context, path string, content string) error {
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory: %w", err)
		}
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}
	return nil
}

// ReadFile reads the entire content of a file.
func ReadFile(ctx context.Context, path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}
	return string(content), nil
}

// ListDir lists the entries in a directory.
func ListDir(ctx context.Context, path string) ([]string, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names, nil
}

// MoveFile moves a file from src to dst.
func MoveFile(ctx context.Context, src string, dst string) error {
	dir := filepath.Dir(dst)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create destination directory: %w", err)
		}
	}
	if err := os.Rename(src, dst); err != nil {
		return fmt.Errorf("failed to move file: %w", err)
	}
	return nil
}
