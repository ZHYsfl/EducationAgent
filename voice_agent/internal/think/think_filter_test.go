package think

import (
	"testing"
)

// ===========================================================================
// ThinkFilter (streaming)
// ===========================================================================

func TestThinkFilter_BasicBlock(t *testing.T) {
	var tf ThinkFilter
	out := tf.Feed("<think>internal reasoning</think>visible text")
	if out != "visible text" {
		t.Errorf("got %q, want %q", out, "visible text")
	}
	flushed := tf.Flush()
	if flushed != "" {
		t.Errorf("flush got %q, want empty", flushed)
	}
}

func TestThinkFilter_SplitAcrossTokens(t *testing.T) {
	var tf ThinkFilter
	tokens := []string{"<thi", "nk>", "hidden", "</thi", "nk>", "after"}
	var result string
	for _, tok := range tokens {
		result += tf.Feed(tok)
	}
	result += tf.Flush()
	if result != "after" {
		t.Errorf("got %q, want %q", result, "after")
	}
}

func TestThinkFilter_NoThinkTags(t *testing.T) {
	var tf ThinkFilter
	out := tf.Feed("plain text no tags")
	out += tf.Flush()
	if out != "plain text no tags" {
		t.Errorf("got %q, want %q", out, "plain text no tags")
	}
}

func TestThinkFilter_MultipleBlocks(t *testing.T) {
	var tf ThinkFilter
	var result string
	result += tf.Feed("before<think>aaa</think>middle<think>bbb</think>end")
	result += tf.Flush()
	if result != "beforemiddleend" {
		t.Errorf("got %q, want %q", result, "beforemiddleend")
	}
}

func TestThinkFilter_UnclosedThinkDiscardedOnFlush(t *testing.T) {
	var tf ThinkFilter
	result := tf.Feed("visible<think>unclosed")
	result += tf.Flush()
	if result != "visible" {
		t.Errorf("got %q, want %q", result, "visible")
	}
}

// ===========================================================================
// StripThinkTags (non-streaming)
// ===========================================================================

func TestStripThinkTags(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"no tags", "hello world", "hello world"},
		{"single block", "<think>xyz</think>answer", "answer"},
		{"multiple blocks", "a<think>1</think>b<think>2</think>c", "abc"},
		{"unclosed", "before<think>unclosed", "before"},
		{"empty think", "<think></think>ok", "ok"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := StripThinkTags(tt.input)
			if got != tt.want {
				t.Errorf("StripThinkTags(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// ===========================================================================
// LongestSuffixPrefix
// ===========================================================================

func TestLongestSuffixPrefix(t *testing.T) {
	tests := []struct {
		text string
		tag  string
		want string
	}{
		{"abc<th", "<think>", "<th"},
		{"abc<think>", "<think>", ""},
		{"abc", "<think>", ""},
		{"<", "<think>", "<"},
		{"abc</thi", "</think>", "</thi"},
		{"abc</think>", "</think>", ""},
	}
	for _, tt := range tests {
		t.Run(tt.text+"_"+tt.tag, func(t *testing.T) {
			got := LongestSuffixPrefix(tt.text, tt.tag)
			if got != tt.want {
				t.Errorf("LongestSuffixPrefix(%q, %q) = %q, want %q", tt.text, tt.tag, got, tt.want)
			}
		})
	}
}
