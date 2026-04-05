package agent_test

import (
	agent "voiceagent/agent"
	"testing"
	"time"
)

// ===========================================================================
// asyncExtractMemory
// ===========================================================================

func TestAsyncPushContext(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipeline(s, mock)

	p.AsyncPushContext("用户说了什么", "助手回复了什么")
	calls := agent.WaitForPushCtx(mock, 1)
	if len(calls) != 1 {
		t.Fatalf("expected 1 PushContext call, got %d", len(calls))
	}
	if calls[0].UserID != "user_test" {
		t.Errorf("userID = %q", calls[0].UserID)
	}
}

func TestAsyncPushContext_EmptyInput(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipeline(s, mock)

	p.AsyncPushContext("", "")
	time.Sleep(30 * time.Millisecond)

	mock.Mu.Lock()
	calls := len(mock.PushCtxCalls)
	mock.Mu.Unlock()
	if calls != 0 {
		t.Error("should not push context for empty input")
	}
}

func TestAsyncPushContext_NilClients(t *testing.T) {
	s := agent.NewTestSession(nil)
	p := agent.NewTestPipeline(s, nil)
	p.AsyncPushContext("test", "test") // should not panic
}

// ===========================================================================
// OnInterrupt
// ===========================================================================

func TestOnInterrupt_PreservesTokens(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipeline(s, mock)

	p.WriteRawTokens("<think>reasoning</think>visible text")
	p.OnInterrupt()

	if p.RawTokensLen() != 0 {
		t.Error("raw tokens should be cleared after interrupt")
	}
}

func TestOnInterrupt_ClosesUnclosedThink(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipeline(s, mock)

	p.WriteRawTokens("<think>unclosed reasoning")
	p.OnInterrupt()

	msgs := p.GetHistory().ToOpenAI()
	if len(msgs) < 2 {
		t.Fatal("expected at least 2 messages (system + interrupted)")
	}
}

func TestOnInterrupt_EmptyTokens(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipeline(s, mock)

	p.OnInterrupt() // should not panic
}

// ===========================================================================
// OnAudioData / OnVADEnd
// ===========================================================================

func TestOnAudioData_NilChannel(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipeline(s, mock)
	p.SetAudioCh(nil)
	p.OnAudioData([]byte{1, 2, 3}) // should not panic
}

func TestOnAudioData_WithChannel(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipeline(s, mock)
	p.SetAudioCh(make(chan []byte, 10))
	p.OnAudioData([]byte{1, 2, 3})

	select {
	case data := <-p.GetAudioCh():
		if len(data) != 3 {
			t.Errorf("expected 3 bytes, got %d", len(data))
		}
	default:
		t.Error("expected data in audioCh")
	}
}

func TestOnVADEnd_NilChannel(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipeline(s, mock)
	p.SetVADEndCh(nil)
	p.OnVADEnd() // should not panic
}

func TestOnVADEnd_WithChannel(t *testing.T) {
	mock := &agent.MockServices{}
	s := agent.NewTestSession(mock)
	p := agent.NewTestPipeline(s, mock)
	p.SetVADEndCh(make(chan struct{}, 1))
	p.OnVADEnd()

	select {
	case <-p.GetVADEndCh():
	default:
		t.Error("expected signal in vadEndCh")
	}
}

