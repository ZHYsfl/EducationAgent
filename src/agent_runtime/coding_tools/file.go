package tools

import (
	"context"
	"fmt"
	"os"
	"strings"
)

const defaultMaxResponseLen = 10000

// EditFile edits a file by replacing the first occurrence of old_string with
// new_string.
func EditFile(ctx context.Context, path string, oldString string, newString string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Sprintf("failed to read file: %v", err), fmt.Errorf("failed to read file: %w", err)
	}
	replaced := strings.Replace(string(content), oldString, newString, 1)
	if replaced == string(content) {
		return "old_string not found in file", fmt.Errorf("old_string not found in file")
	}
	if err := os.WriteFile(path, []byte(replaced), 0644); err != nil {
		return fmt.Sprintf("failed to write file: %v", err), fmt.Errorf("failed to write file: %w", err)
	}
	return "successfully edited file", nil
}

// ReadFile reads a file, optionally restricted to a [start_line, end_line]
// range (1-based, inclusive). If max_response_len is 0 or negative, a default
// cap of 10000 runes is applied to avoid overflowing the LLM context.
func ReadFile(ctx context.Context, path string, startLine int, endLine int, maxResponseLen int) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}

	selected := string(content)
	if startLine > 0 || endLine > 0 {
		lines := strings.Split(selected, "\n")
		if startLine <= 0 {
			startLine = 1
		}
		if endLine <= 0 || endLine > len(lines) {
			endLine = len(lines)
		}
		if startLine > endLine {
			startLine = 1
			endLine = len(lines)
		}
		selected = strings.Join(lines[startLine-1:endLine], "\n")
	}

	if maxResponseLen <= 0 {
		maxResponseLen = defaultMaxResponseLen
	}
	runes := []rune(selected)
	if len(runes) > maxResponseLen {
		selected = string(runes[:maxResponseLen]) + "\n[truncated by max_response_len]"
	}

	return selected, nil
}
