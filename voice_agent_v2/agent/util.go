package agent

import (
	"unicode"
)

func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}

var sentenceEnders = map[rune]bool{
	'。': true, '！': true, '？': true, '；': true,
	'.': true, '!': true, '?': true, ';': true,
	'，': true, ',': true,
	'\n': true,
}

func isSentenceEnd(s string) bool {
	s2 := []rune(s)
	for len(s2) > 0 && unicode.IsSpace(s2[len(s2)-1]) {
		s2 = s2[:len(s2)-1]
	}
	if len(s2) == 0 {
		return false
	}
	return sentenceEnders[s2[len(s2)-1]]
}
