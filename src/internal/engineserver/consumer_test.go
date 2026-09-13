package engineserver

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"educationagent/internal/player"
	"educationagent/internal/voiceengine"
)

func TestConsumerSpeaksSentences(t *testing.T) {
	chunk1 := bytes.Repeat([]byte{0x01}, 1500)
	chunk2 := bytes.Repeat([]byte{0x02}, 2600)
	fake := fakeTTSUpstream(t, http.StatusOK, chunk1, chunk2)
	defer fake.Close()

	engine := voiceengine.NewEngine(context.Background())
	srv := httptest.NewServer(http.HandlerFunc(NewServer(fake.URL).HandleTTSInfer))
	defer srv.Close()

	outDir := t.TempDir()
	p := player.New()
	c := NewSentenceConsumer(engine, p, "ws"+srv.URL[len("http"):]+TTSPath, outDir)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.Run(ctx)

	batch := []voiceengine.Token{
		{Content: "第一句。", State: voiceengine.StateTTSTokens, Gen: 0},
		{Content: "第二句！", State: voiceengine.StateTTSTokens, Gen: 0},
		{Content: "", State: voiceengine.StateIdle, Gen: 0},
	}
	if err := engine.Queue().Push(ctx, batch); err != nil {
		t.Fatalf("push: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if len(c.Records()) == 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	records := c.Records()
	if len(records) != 2 {
		t.Fatalf("records = %d, want 2", len(records))
	}
	for i, rec := range records {
		want := []string{"第一句。", "第二句！"}[i]
		if rec.Text != want || rec.Bytes != len(chunk1)+len(chunk2) || rec.Frames == 0 {
			t.Errorf("record %d = %+v", i, rec)
		}
		name := filepath.Join(outDir, []string{"sentence_001.pcm", "sentence_002.pcm"}[i])
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if !bytes.Equal(data, append(append([]byte{}, chunk1...), chunk2...)) {
			t.Errorf("%s content mismatch (%d bytes)", name, len(data))
		}
	}

	if got := p.DoneCount(); got != 2 {
		t.Fatalf("player DoneCount = %d, want 2", got)
	}

	// The idle token must not produce a sentence; give the consumer a beat.
	time.Sleep(100 * time.Millisecond)
	if got := len(c.Records()); got != 2 {
		t.Fatalf("idle token created a record, have %d", got)
	}
}

func TestConsumerDropsStaleGenerationSentences(t *testing.T) {
	chunk := bytes.Repeat([]byte{0x03}, 500)
	fake := fakeTTSUpstream(t, http.StatusOK, chunk)
	defer fake.Close()

	engine := voiceengine.NewEngine(context.Background())
	srv := httptest.NewServer(http.HandlerFunc(NewServer(fake.URL).HandleTTSInfer))
	defer srv.Close()

	outDir := t.TempDir()
	p := player.New()
	c := NewSentenceConsumer(engine, p, "ws"+srv.URL[len("http"):]+TTSPath, outDir)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.Run(ctx)

	// A sentence completes on the wire, but the generation has moved on:
	// the consumer must treat it as mute and speak nothing.
	engine.BumpGeneration()
	batch := []voiceengine.Token{
		{Content: "旧代的半句", State: voiceengine.StateTTSTokens, Gen: 0},
		{Content: "，", State: voiceengine.StateTTSTokens, Gen: 0},
	}
	if err := engine.Queue().Push(ctx, batch); err != nil {
		t.Fatalf("push: %v", err)
	}

	time.Sleep(300 * time.Millisecond)
	if got := len(c.Records()); got != 0 {
		t.Fatalf("stale sentence was spoken: %d records", got)
	}
	if got := p.DoneCount(); got != 0 {
		t.Fatalf("player ledger polluted: DoneCount = %d", got)
	}
}

func TestConsumerFlushesTrailingHalfSentenceOnIdle(t *testing.T) {
	chunk := bytes.Repeat([]byte{0x07}, 800)
	fake := fakeTTSUpstream(t, http.StatusOK, chunk)
	defer fake.Close()

	engine := voiceengine.NewEngine(context.Background())
	srv := httptest.NewServer(http.HandlerFunc(NewServer(fake.URL).HandleTTSInfer))
	defer srv.Close()

	outDir := t.TempDir()
	p := player.New()
	c := NewSentenceConsumer(engine, p, "ws"+srv.URL[len("http"):]+TTSPath, outDir)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.Run(ctx)

	batch := []voiceengine.Token{
		{Content: "最后半句没有标点", State: voiceengine.StateTTSTokens, Gen: 0},
		{Content: "", State: voiceengine.StateIdle, Gen: 0},
	}
	if err := engine.Queue().Push(ctx, batch); err != nil {
		t.Fatalf("push: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if len(c.Records()) == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	records := c.Records()
	if len(records) != 1 || records[0].Text != "最后半句没有标点" {
		t.Fatalf("records = %+v, want the flushed half-sentence", records)
	}
	if got := p.DoneCount(); got != 1 {
		t.Fatalf("DoneCount = %d, want 1", got)
	}
}

func TestConsumerIgnoresStaleIdleFlush(t *testing.T) {
	chunk := bytes.Repeat([]byte{0x08}, 400)
	fake := fakeTTSUpstream(t, http.StatusOK, chunk)
	defer fake.Close()

	engine := voiceengine.NewEngine(context.Background())
	srv := httptest.NewServer(http.HandlerFunc(NewServer(fake.URL).HandleTTSInfer))
	defer srv.Close()

	outDir := t.TempDir()
	p := player.New()
	c := NewSentenceConsumer(engine, p, "ws"+srv.URL[len("http"):]+TTSPath, outDir)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.Run(ctx)

	engine.BumpGeneration() // barge-in: everything stamped 0 below is now stale
	batch := []voiceengine.Token{
		{Content: "旧代半句", State: voiceengine.StateTTSTokens, Gen: 0},
		{Content: "", State: voiceengine.StateIdle, Gen: 0},
	}
	if err := engine.Queue().Push(ctx, batch); err != nil {
		t.Fatalf("push: %v", err)
	}

	time.Sleep(300 * time.Millisecond)
	if got := len(c.Records()); got != 0 {
		t.Fatalf("stale flush spoke %d sentences", got)
	}
}
