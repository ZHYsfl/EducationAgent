package agent

import (
	"testing"
)

// ===========================================================================
// isSentenceEnd
// ===========================================================================

func TestIsSentenceEnd(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"你好。", true},
		{"真的吗？", true},
		{"太好了！", true},
		{"首先；", true},
		{"Hello.", true},
		{"Really?", true},
		{"Wow!", true},
		{"ok;", true},
		{"好的，", true},
		{"ok,", true},
		{"新行\n", false}, // TrimRightFunc removes \n, leaving "新行" which has no ender
		{"", false},
		{"   ", false},
		{"没有标点", false},
		{"继续说", false},
		// trailing whitespace should be trimmed
		{"好的。 ", true},
		{"hello?  \t", true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := isSentenceEnd(tt.input)
			if got != tt.want {
				t.Errorf("isSentenceEnd(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// ===========================================================================
// truncate
// ===========================================================================

func TestTruncate(t *testing.T) {
	tests := []struct {
		input  string
		maxLen int
		want   string
	}{
		{"hello", 10, "hello"},
		{"hello", 5, "hello"},
		{"hello world", 5, "hello..."},
		{"", 5, ""},
		{"你好世界测试", 3, "你好世..."},
		{"abc", 0, "..."},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := truncate(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}

// ===========================================================================
// string helpers (shared across test files)
// ===========================================================================

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexStr(s, sub) >= 0)
}

func indexStr(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
