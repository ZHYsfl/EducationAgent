package main

import (
	"context"
	"strings"
	"testing"
	"time"
)

func newRunnableSessionForTextInput(t *testing.T) *Session {
	t.Helper()
	llm := newMockLLMServer("do not interrupt", []string{"收到", "。"})
	t.Cleanup(llm.Close)

	s := newTestSession(&mockServices{})
	p := NewPipeline(s, s.config, s.clients)
	p.largeLLM = newMockAgent(llm.URL)
	p.smallLLM = newMockAgent(llm.URL)
	p.ttsClient = &mockTTS{}
	s.pipeline = p
	return s
}

func TestHandleTextMessage_VADEndDispatchesToPipeline(t *testing.T) {
	s := newTestSession(&mockServices{})
	p := newTestPipeline(s, s.clients)
	p.vadEndCh = make(chan struct{}, 1)
	s.SetState(StateListening)

	s.handleTextMessage(WSMessage{Type: "vad_end"})
	if len(p.vadEndCh) != 1 {
		t.Fatal("expected vad_end signal to pipeline")
	}
}

func TestHandleTextMessage_TaskInitDispatch(t *testing.T) {
	s := newTestSession(&mockServices{})
	_ = newTestPipelineWithTTS(s, s.clients)

	s.handleTextMessage(WSMessage{
		Type:   "task_init",
		Topic:  "代数",
		Audience: "高一",
	})

	s.reqMu.RLock()
	defer s.reqMu.RUnlock()
	if s.Requirements == nil {
		t.Fatal("requirements should be initialized by task_init")
	}
	if s.Requirements.Topic != "代数" {
		t.Fatalf("topic=%q", s.Requirements.Topic)
	}
}

func TestHandleTextMessage_VADStartDispatch(t *testing.T) {
	s := newTestSession(&mockServices{})
	p := NewPipeline(s, s.config, s.clients)
	s.pipeline = p
	p.asrClient = &blockingASR{}
	p.ttsClient = &mockTTS{}

	s.handleTextMessage(WSMessage{Type: "vad_start"})
	waitUntil(t, time.Second, func() bool { return s.GetState() == StateListening }, "vad_start should switch to listening")
	s.cancelCurrentPipeline()
}

func TestHandleTextMessage_RequirementsConfirmDispatch(t *testing.T) {
	m := &mockServices{
		InitPPTFn: func(ctx context.Context, req PPTInitRequest) (PPTInitResponse, error) {
			return PPTInitResponse{TaskID: "task_from_confirm"}, nil
		},
	}
	s := newTestSession(m)
	_ = newTestPipelineWithTTS(s, m)
	s.Requirements = makeFullRequirements()
	s.Requirements.Status = "confirming"
	s.Requirements.SessionID = s.SessionID
	s.Requirements.UserID = s.UserID

	registerSession(s)
	defer unregisterSession(s)

	ok := true
	s.handleTextMessage(WSMessage{
		Type:      "requirements_confirm",
		Confirmed: &ok,
	})
	waitUntil(t, time.Second, func() bool { return s.GetActiveTask() == "task_from_confirm" }, "requirements_confirm should trigger task creation")
}

func TestHandleTextInput_FromProcessing_InterruptsAndProcesses(t *testing.T) {
	s := newRunnableSessionForTextInput(t)
	s.SetState(StateProcessing)

	s.pipeline.tokensMu.Lock()
	s.pipeline.rawGeneratedTokens.WriteString("<think>未完成")
	s.pipeline.tokensMu.Unlock()

	s.handleTextInput(WSMessage{Type: "text_input", Text: "新的问题"})

	waitUntil(t, 2*time.Second, func() bool { return s.GetState() == StateIdle }, "processing input did not finish")
	if len(s.pipeline.history.messages) == 0 {
		t.Fatal("history should contain processed messages")
	}
	foundUser := false
	for _, m := range s.pipeline.history.messages {
		if m.Role == "user" && strings.Contains(m.Content, "新的问题") {
			foundUser = true
			break
		}
	}
	if !foundUser {
		t.Fatalf("expected user input in history, got: %+v", s.pipeline.history.messages)
	}
}

func TestHandleTextInput_FromSpeaking_InterruptsAndProcesses(t *testing.T) {
	s := newRunnableSessionForTextInput(t)
	s.SetState(StateSpeaking)

	s.handleTextInput(WSMessage{Type: "text_input", Text: "继续讲"})

	waitUntil(t, 2*time.Second, func() bool { return s.GetState() == StateIdle }, "speaking input did not finish")
	if len(s.pipeline.history.messages) == 0 {
		t.Fatal("history should contain processed messages")
	}
}

