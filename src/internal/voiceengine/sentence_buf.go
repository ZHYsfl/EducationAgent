package voiceengine

import (
	"strings"
	"sync"
	"unicode/utf8"
)

// sentenceBuf accumulates streamed tokens into sentences, generation-aware:
// it never hands out a sentence built from tokens of different generations.
type sentenceBuf struct {
	mu   sync.Mutex
	text strings.Builder
	gen  int64 // -1 = 空
}

func (b *sentenceBuf) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.text.Reset()
	b.gen = -1
}

// Snapshot returns the buffered half-sentence without clearing it.
func (b *sentenceBuf) Snapshot() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.text.String()
}

// HandleToken applies, in order: (1) a token from a stale generation is
// dropped; (2) an empty buffer claims the token's generation; (3) a token
// from a newer generation clears the half-sentence the old one left behind;
// (4) the text is appended and a sentence-ending punctuation flushes it.
func (b *sentenceBuf) HandleToken(t Token, currentGen int64) (sentence string, gen int64, ok bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if t.Gen != currentGen {
		return "", 0, false
	}
	if b.gen == -1 {
		b.gen = t.Gen
	}
	if t.Gen != b.gen {
		b.text.Reset()
		b.gen = t.Gen
	}
	b.text.WriteString(t.Content)
	if r, _ := utf8.DecodeLastRuneInString(t.Content); isSentenceEnd(r) {
		s := b.text.String()
		b.text.Reset()
		b.gen = -1
		return s, t.Gen, true
	}
	return "", 0, false
}

func isSentenceEnd(r rune) bool {
	switch r {
	case '，', '。', '！', '？', '；', '：', '…',
		',', '.', '!', '?', ';', ':':
		return true
	}
	return false
}
