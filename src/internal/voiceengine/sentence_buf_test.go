package voiceengine

import (
	"testing"
)

func mustNoSentence(t *testing.T, s string, ok bool) {
	t.Helper()
	if ok {
		t.Fatalf("unexpected sentence %q", s)
	}
}

func TestSentenceBufDropsStaleGeneration(t *testing.T) {
	b := &sentenceBuf{gen: -1}

	if _, _, ok := b.HandleToken(Token{Content: "你好", State: StateTTSTokens, Gen: 5}, 5); ok {
		t.Fatal("single token must not emit a sentence")
	}

	// Rule 1: a stale-generation token is mute even when the buffer is busy.
	if s, _, ok := b.HandleToken(Token{Content: "垃圾", State: StateTTSTokens, Gen: 4}, 5); ok || s != "" {
		t.Fatalf("stale token must be dropped, got %q ok=%v", s, ok)
	}

	// The stale token must not have polluted the buffer.
	s, gen, ok := b.HandleToken(Token{Content: "，", State: StateTTSTokens, Gen: 5}, 5)
	if !ok || s != "你好，" || gen != 5 {
		t.Fatalf("got (%q, %d, %v), want (%q, 5, true)", s, gen, ok, "你好，")
	}
}

func TestSentenceBufClaimsGenerationWhenEmpty(t *testing.T) {
	b := &sentenceBuf{gen: -1}
	if b.gen != -1 {
		t.Fatalf("fresh buffer gen = %d, want -1", b.gen)
	}
	if _, _, ok := b.HandleToken(Token{Content: "x", State: StateTTSTokens, Gen: 7}, 7); ok {
		t.Fatal("unexpected sentence")
	}
	if b.gen != 7 {
		t.Fatalf("buffer did not claim generation: gen = %d, want 7", b.gen)
	}
}

func TestSentenceBufClearsHalfSentenceOnNewGeneration(t *testing.T) {
	b := &sentenceBuf{gen: -1}

	if _, _, ok := b.HandleToken(Token{Content: "旧半句", State: StateTTSTokens, Gen: 5}, 5); ok {
		t.Fatal("unexpected sentence")
	}

	// Rule 3: a newer-generation token drops the half-sentence of the old one.
	s, gen, ok := b.HandleToken(Token{Content: "新句。", State: StateTTSTokens, Gen: 6}, 6)
	if !ok || s != "新句。" || gen != 6 {
		t.Fatalf("got (%q, %d, %v), want (%q, 6, true)", s, gen, ok, "新句。")
	}
}

func TestSentenceBufMixedGenerations(t *testing.T) {
	b := &sentenceBuf{gen: -1}

	s, gen, ok := b.HandleToken(Token{Content: "你好", State: StateTTSTokens, Gen: 5}, 5)
	mustNoSentence(t, s, ok)
	s, gen, ok = b.HandleToken(Token{Content: "，", State: StateTTSTokens, Gen: 5}, 5)
	if !ok || s != "你好，" || gen != 5 {
		t.Fatalf("got (%q, %d, %v), want (%q, 5, true)", s, gen, ok, "你好，")
	}

	// Barge-in: generation moves to 6; leftover gen-5 tokens are mute.
	current := int64(6)
	s, _, ok = b.HandleToken(Token{Content: "吗", State: StateTTSTokens, Gen: 5}, current)
	mustNoSentence(t, s, ok)

	s, _, ok = b.HandleToken(Token{Content: "世界", State: StateTTSTokens, Gen: 6}, current)
	mustNoSentence(t, s, ok)
	s, _, ok = b.HandleToken(Token{Content: "垃圾", State: StateTTSTokens, Gen: 5}, current)
	mustNoSentence(t, s, ok)

	s, gen, ok = b.HandleToken(Token{Content: "。", State: StateTTSTokens, Gen: 6}, current)
	if !ok || s != "世界。" || gen != 6 {
		t.Fatalf("got (%q, %d, %v), want (%q, 6, true)", s, gen, ok, "世界。")
	}
}

func TestSentenceBufPunctuation(t *testing.T) {
	puncts := []rune{'，', '。', '！', '？', '；', '：', '…', ',', '.', '!', '?', ';', ':'}
	for _, r := range puncts {
		b := &sentenceBuf{gen: -1}
		body := "词" + string(r)
		s, _, ok := b.HandleToken(Token{Content: body, State: StateTTSTokens, Gen: 1}, 1)
		if !ok || s != body {
			t.Errorf("punct %q: got (%q, %v), want sentence %q", r, s, ok, body)
		}
	}

	nonPuncts := []rune{'a', '1', '、', '（', '~'}
	for _, r := range nonPuncts {
		b := &sentenceBuf{gen: -1}
		if s, _, ok := b.HandleToken(Token{Content: "x" + string(r), State: StateTTSTokens, Gen: 1}, 1); ok {
			t.Errorf("non-punct %q must not flush, got %q", r, s)
		}
	}
}

func TestSentenceBufHalfWordTokens(t *testing.T) {
	b := &sentenceBuf{gen: -1}
	for _, tok := range []Token{
		{Content: "wor", State: StateTTSTokens, Gen: 1},
		{Content: "ld", State: StateTTSTokens, Gen: 1},
		{Content: "!", State: StateTTSTokens, Gen: 1},
	} {
		s, _, ok := b.HandleToken(tok, 1)
		if ok {
			if s != "world!" {
				t.Fatalf("got %q, want %q", s, "world!")
			}
			return
		}
	}
	t.Fatal("sentence never flushed")
}

func TestSentenceBufReset(t *testing.T) {
	b := &sentenceBuf{gen: -1}
	b.HandleToken(Token{Content: "残留", State: StateTTSTokens, Gen: 3}, 3)
	b.Reset()
	if b.gen != -1 {
		t.Fatalf("gen = %d after Reset, want -1", b.gen)
	}
	s, _, ok := b.HandleToken(Token{Content: "新。", State: StateTTSTokens, Gen: 4}, 4)
	if !ok || s != "新。" {
		t.Fatalf("got (%q, %v), want (%q, true)", s, ok, "新。")
	}
}
