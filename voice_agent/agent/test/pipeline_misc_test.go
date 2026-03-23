package agent

import (
	"context"
	"testing"
	"time"

	"voiceagent/internal/asr"
)

// ===========================================================================
// asyncExtractMemory
// ===========================================================================

func TestAsyncExtractMemory(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	p.asyncExtractMemory("用户说了什么", "助手回复了什么")
	calls := waitForExtractMem(mock, 1)
	if len(calls) != 1 {
		t.Fatalf("expected 1 ExtractMemory call, got %d", len(calls))
	}
	if calls[0].UserID != "user_test" {
		t.Errorf("userID = %q", calls[0].UserID)
	}
	if len(calls[0].Messages) != 2 {
		t.Errorf("expected 2 turns, got %d", len(calls[0].Messages))
	}
}

func TestAsyncExtractMemory_EmptyInput(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	p.asyncExtractMemory("", "")
	time.Sleep(30 * time.Millisecond)

	mock.mu.Lock()
	calls := len(mock.extractMemCalls)
	mock.mu.Unlock()
	if calls != 0 {
		t.Error("should not extract memory for empty input")
	}
}

func TestAsyncExtractMemory_NilClients(t *testing.T) {
	s := newTestSession(nil)
	p := newTestPipeline(s, nil)
	p.asyncExtractMemory("test", "test") // should not panic
}

// ===========================================================================
// OnInterrupt
// ===========================================================================

func TestOnInterrupt_PreservesTokens(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	p.rawGeneratedTokens.WriteString("<think>reasoning</think>visible text")
	p.OnInterrupt()

	if p.rawGeneratedTokens.Len() != 0 {
		t.Error("raw tokens should be cleared after interrupt")
	}
}

func TestOnInterrupt_ClosesUnclosedThink(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	p.rawGeneratedTokens.WriteString("<think>unclosed reasoning")
	p.OnInterrupt()

	msgs := p.history.ToOpenAI()
	if len(msgs) < 2 {
		t.Fatal("expected at least 2 messages (system + interrupted)")
	}
}

func TestOnInterrupt_EmptyTokens(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	p.OnInterrupt() // should not panic
}

// ===========================================================================
// OnAudioData / OnVADEnd
// ===========================================================================

func TestOnAudioData_NilChannel(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)
	p.audioCh = nil
	p.OnAudioData([]byte{1, 2, 3}) // should not panic
}

func TestOnAudioData_WithChannel(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)
	p.audioCh = make(chan []byte, 10)
	p.OnAudioData([]byte{1, 2, 3})

	select {
	case data := <-p.audioCh:
		if len(data) != 3 {
			t.Errorf("expected 3 bytes, got %d", len(data))
		}
	default:
		t.Error("expected data in audioCh")
	}
}

func TestOnVADEnd_NilChannel(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)
	p.vadEndCh = nil
	p.OnVADEnd() // should not panic
}

func TestOnVADEnd_WithChannel(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)
	p.vadEndCh = make(chan struct{}, 1)
	p.OnVADEnd()

	select {
	case <-p.vadEndCh:
	default:
		t.Error("expected signal in vadEndCh")
	}
}

// ===========================================================================
// Draft thinking helpers
// ===========================================================================

func TestDraftOutput_AppendGetReset(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	p.appendDraftOutput("hello ")
	p.appendDraftOutput("world")
	if got := p.getDraftOutput(); got != "hello world" {
		t.Errorf("got %q", got)
	}
	p.resetDraftOutput()
	if got := p.getDraftOutput(); got != "" {
		t.Errorf("after reset: %q", got)
	}
}

// ===========================================================================
// drainASRResults
// ===========================================================================

func TestDrainASRResults_Normal(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	ch := make(chan asr.ASRResult, 3)
	ch <- asr.ASRResult{Text: "hello", Mode: "online"}
	ch <- asr.ASRResult{Text: "final", Mode: "2pass-offline"}
	close(ch)

	var partials []string
	var finalText string
	p.drainASRResults(context.Background(), ch, &partials, &finalText, time.Second)

	if finalText != "final" {
		t.Errorf("finalText = %q", finalText)
	}
	if len(partials) != 1 || partials[0] != "hello" {
		t.Errorf("partials = %v", partials)
	}
}

func TestDrainASRResults_Timeout(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	ch := make(chan asr.ASRResult)
	var partials []string
	var finalText string

	done := make(chan struct{})
	go func() {
		p.drainASRResults(context.Background(), ch, &partials, &finalText, 50*time.Millisecond)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("drainASRResults should timeout")
	}
}

func TestDrainASRResults_ContextCancel(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	p := newTestPipeline(s, mock)

	ch := make(chan asr.ASRResult)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var partials []string
	var finalText string

	done := make(chan struct{})
	go func() {
		p.drainASRResults(ctx, ch, &partials, &finalText, 5*time.Second)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("drainASRResults should exit on cancelled context")
	}
}
