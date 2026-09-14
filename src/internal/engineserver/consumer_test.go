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

// stalledTTSUpstream sends headers then holds the body for a bounded time
// (a permanent block would deadlock httptest.Close at teardown).
func stalledTTSUpstream(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("X-Sample-Rate", "22050")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
}

func TestConsumerSurvivesStalledTTS(t *testing.T) {
	stall := stalledTTSUpstream(t)
	defer stall.Close()
	chunk := bytes.Repeat([]byte{0x09}, 600)
	healthy := fakeTTSUpstream(t, http.StatusOK, chunk)
	defer healthy.Close()

	engine := voiceengine.NewEngine(context.Background())
	srv := httptest.NewServer(http.HandlerFunc(NewServer(stall.URL).HandleTTSInfer))
	defer srv.Close()
	healthySrv := httptest.NewServer(http.HandlerFunc(NewServer(healthy.URL).HandleTTSInfer))
	defer healthySrv.Close()

	outDir := t.TempDir()
	p := player.New()
	c := NewSentenceConsumer(engine, p, "ws"+srv.URL[len("http"):]+TTSPath, outDir)
	c.FrameTimeout = 200 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.Run(ctx)

	if err := engine.Queue().Push(ctx, []voiceengine.Token{
		{Content: "会被卡死的句子。", State: voiceengine.StateTTSTokens, Gen: 0},
	}); err != nil {
		t.Fatalf("push: %v", err)
	}

	// The stalled sentence must fail fast and free the consumer.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if p.DoneCount() == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if p.DoneCount() != 1 {
		t.Fatalf("stalled sentence did not resolve, DoneCount = %d", p.DoneCount())
	}

	// Repoint at a healthy upstream: the consumer must speak again.
	c.SetEndpoint("ws" + healthySrv.URL[len("http"):] + TTSPath)
	if err := engine.Queue().Push(ctx, []voiceengine.Token{
		{Content: "正常的句子。", State: voiceengine.StateTTSTokens, Gen: 0},
		{Content: "", State: voiceengine.StateIdle, Gen: 0},
	}); err != nil {
		t.Fatalf("push: %v", err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if len(c.Records()) == 1 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("consumer never recovered, records = %+v", c.Records())
}

func TestConsumerDropsWhenPlayerStopped(t *testing.T) {
	chunk := bytes.Repeat([]byte{0x0A}, 500)
	fake := fakeTTSUpstream(t, http.StatusOK, chunk)
	defer fake.Close()

	engine := voiceengine.NewEngine(context.Background())
	srv := httptest.NewServer(http.HandlerFunc(NewServer(fake.URL).HandleTTSInfer))
	defer srv.Close()

	p := player.New()
	c := NewSentenceConsumer(engine, p, "ws"+srv.URL[len("http"):]+TTSPath, t.TempDir())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.Run(ctx)

	// 落闸后：世代再新的句子也不许开口、不许进账本。
	p.StopAndSnapshot()
	if err := engine.Queue().Push(ctx, []voiceengine.Token{
		{Content: "停播后到达的句子。", State: voiceengine.StateTTSTokens, Gen: 0},
	}); err != nil {
		t.Fatalf("push: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	if got := len(c.Records()); got != 0 {
		t.Fatalf("stopped player still recorded %d sentences", got)
	}
	if got := p.DoneCount(); got != 0 {
		t.Fatalf("player ledger polluted while stopped: %d", got)
	}

	// 新 turn 点火（Resume）后恢复正常。
	p.Resume()
	if err := engine.Queue().Push(ctx, []voiceengine.Token{
		{Content: "恢复后的句子。", State: voiceengine.StateTTSTokens, Gen: 0},
	}); err != nil {
		t.Fatalf("push: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if len(c.Records()) == 1 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("sentence after Resume was not spoken")
}

func TestConsumerAbortsMidStreamOnStop(t *testing.T) {
	chunk1 := bytes.Repeat([]byte{0x0B}, 400)
	chunk2 := bytes.Repeat([]byte{0x0C}, 400)
	firstFrameSent := make(chan struct{})
	release := make(chan struct{})
	// 上游吐完第一帧后卡住，直到测试放行第二帧：speak 必然已经开始，
	// stop 必然落在流的正中间（不靠 sleep 赌时序）。
	dribble := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("X-Sample-Rate", "22050")
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		_, _ = w.Write(chunk1)
		w.(http.Flusher).Flush()
		close(firstFrameSent)
		select {
		case <-r.Context().Done():
			return
		case <-release:
		}
		_, _ = w.Write(chunk2)
	}))
	defer dribble.Close()

	engine := voiceengine.NewEngine(context.Background())
	srv := httptest.NewServer(http.HandlerFunc(NewServer(dribble.URL).HandleTTSInfer))
	defer srv.Close()

	p := player.New()
	c := NewSentenceConsumer(engine, p, "ws"+srv.URL[len("http"):]+TTSPath, t.TempDir())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.Run(ctx)

	if err := engine.Queue().Push(ctx, []voiceengine.Token{
		{Content: "播到一半被打断的句子。", State: voiceengine.StateTTSTokens, Gen: 0},
	}); err != nil {
		t.Fatalf("push: %v", err)
	}

	<-firstFrameSent
	p.StopAndSnapshot() // 落闸：此后到达的帧一律丢弃不记账
	close(release)

	// 给第二帧充足的到达时间后断言：不进 records、不进账本。
	time.Sleep(500 * time.Millisecond)
	if got := len(c.Records()); got != 0 {
		t.Fatalf("interrupted sentence was recorded: %+v", c.Records())
	}
	if got := p.DoneCount(); got != 0 {
		t.Fatalf("interrupted sentence entered ledger: %d", got)
	}
}
