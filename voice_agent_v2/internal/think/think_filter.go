package think

import "strings"

// ThinkFilter is a streaming filter that strips <think>...</think> blocks
// from an LLM's token stream. Thinking models (Qwen3, DeepSeek-R1, etc.)
// embed internal reasoning inside these tags; we need to exclude that content
// from TTS, frontend display, and token budget accounting while preserving
// the actual response.
//
// The filter handles tags arriving as whole tokens (the common case when
// <think>/<\/think> are special tokens) as well as tags split across
// multiple streaming chunks.
type ThinkFilter struct {
	inThink bool
	partial string // buffered suffix that might be a partial tag
}

// Feed processes one streaming token and returns the visible (non-thinking)
// portion. Returns "" when the token is entirely inside a <think> block or
// consists only of tag markup.
func (f *ThinkFilter) Feed(token string) string {
	text := f.partial + token
	f.partial = ""

	var visible strings.Builder

	for len(text) > 0 {
		if f.inThink {
			idx := strings.Index(text, "</think>")
			if idx >= 0 {
				f.inThink = false
				text = text[idx+len("</think>"):]
			} else {
				f.partial = LongestSuffixPrefix(text, "</think>")
				text = ""
			}
		} else {
			idx := strings.Index(text, "<think>")
			if idx >= 0 {
				visible.WriteString(text[:idx])
				f.inThink = true
				text = text[idx+len("<think>"):]
			} else {
				suffix := LongestSuffixPrefix(text, "<think>")
				visible.WriteString(text[:len(text)-len(suffix)])
				f.partial = suffix
				text = ""
			}
		}
	}
	return visible.String()
}

// Flush returns any buffered partial content when the stream ends.
// Content buffered while inside a <think> block is discarded.
func (f *ThinkFilter) Flush() string {
	s := f.partial
	f.partial = ""
	if f.inThink {
		return ""
	}
	return s
}

// StripThinkTags removes all <think>...</think> blocks from text.
// Used for non-streaming responses (e.g. Small LLM ChatText) where the full
// response arrives at once and we only want the visible label text.
// If a <think> block is not closed, everything from <think> onward is dropped.
func StripThinkTags(s string) string {
	for {
		start := strings.Index(s, "<think>")
		if start < 0 {
			break
		}
		end := strings.Index(s[start:], "</think>")
		if end < 0 {
			s = s[:start]
			break
		}
		s = s[:start] + s[start+end+len("</think>"):]
	}
	return s
}

// LongestSuffixPrefix returns the longest suffix of text that equals a prefix
// of tag. For example, LongestSuffixPrefix("abc<th", "<think>") returns "<th".
func LongestSuffixPrefix(text, tag string) string {
	maxLen := len(tag) - 1
	if maxLen > len(text) {
		maxLen = len(text)
	}
	for i := maxLen; i > 0; i-- {
		if strings.HasSuffix(text, tag[:i]) {
			return tag[:i]
		}
	}
	return ""
}
