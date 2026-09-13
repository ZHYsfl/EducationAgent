// Package tests holds the end-to-end checks; they run only with E2E=1 and
// need the real DeepSeek API plus the local TTS wrapper on :8001.
package tests

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"educationagent/internal/config"
	"educationagent/internal/engineserver"
)

func TestHappyPath(t *testing.T) {
	if os.Getenv("E2E") != "1" {
		t.Skip("set E2E=1 to run the end-to-end happy path")
	}
	if err := config.LoadFile("../.env"); err != nil {
		t.Fatalf("load ../.env: %v", err)
	}
	cfg := config.Load()
	if cfg.DeepSeekAPIKey == "" {
		t.Skip("DEEPSEEK_API_KEY missing")
	}

	outDir := t.TempDir()
	app := engineserver.NewApp(cfg, engineserver.WithOutDir(outDir))

	srv := httptest.NewServer(app.Handler)
	defer srv.Close()
	app.Consumer.SetEndpoint("ws" + strings.TrimPrefix(srv.URL, "http") + engineserver.TTSPath)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go app.Consumer.Run(ctx)

	const prompt = "你是语音助手，回答要口语化、简短。请用三句话介绍西湖，每句以中文句号结尾，不要分点，不要列表。"

	start := time.Now()
	done := make(chan struct{})
	go app.RunTurn(ctx, prompt, done)
	select {
	case <-done:
	case <-time.After(180 * time.Second):
		t.Fatal("producer turn did not finish in 180s")
	}
	t.Logf("producer turn finished in %v", time.Since(start))

	// The last sentence may still be in flight after <-done: poll for a
	// stable ledger (record count == file count == player done count).
	var recordsN, filesN, doneN int
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		records := app.Consumer.Records()
		entries, _ := filepath.Glob(filepath.Join(outDir, "*.pcm"))
		recordsN, filesN, doneN = len(records), len(entries), app.Player.DoneCount()
		if recordsN >= 3 && recordsN == filesN && filesN == doneN {
			time.Sleep(500 * time.Millisecond)
			entries, _ := filepath.Glob(filepath.Join(outDir, "*.pcm"))
			if len(entries) == filesN && len(app.Consumer.Records()) == recordsN {
				break
			}
		}
		time.Sleep(200 * time.Millisecond)
	}

	if recordsN < 3 {
		t.Fatalf("only %d sentences spoken, want >= 3", recordsN)
	}
	if filesN != recordsN {
		t.Fatalf("pcm files = %d, sentence records = %d", filesN, recordsN)
	}
	if doneN != filesN {
		t.Fatalf("player done = %d, pcm files = %d", doneN, filesN)
	}

	var totalBytes int64
	entries, _ := filepath.Glob(filepath.Join(outDir, "*.pcm"))
	for _, name := range entries {
		info, err := os.Stat(name)
		if err != nil {
			t.Fatalf("stat %s: %v", name, err)
		}
		if info.Size()%2 != 0 {
			t.Errorf("%s: odd pcm16le size %d", name, info.Size())
		}
		totalBytes += info.Size()
	}
	if totalBytes <= 22050*2*2 {
		t.Fatalf("total pcm = %d bytes, want > %d (>2s of audio)", totalBytes, 22050*2*2)
	}

	for _, rec := range app.Consumer.Records() {
		if rec.Frames < 1 || rec.Bytes == 0 {
			t.Errorf("bad sentence record %+v (frame order/length enforced by consumer)", rec)
		}
		t.Logf("sentence: %q (%d bytes, %d frames)", rec.Text, rec.Bytes, rec.Frames)
	}
	t.Logf("sentences=%d total_pcm_bytes=%d elapsed=%v", recordsN, totalBytes, time.Since(start))
}
