package agent_test

import (
	"strings"
	"testing"
	"time"
	agent "voiceagent/agent"
)

func newRunnableSessionForTextInput(t *testing.T) *agent.Session {
	t.Helper()
	llm := newMockLLMServer("do not interrupt", []string{"收到", "。"})
	t.Cleanup(llm.Close)

	s := agent.NewTestSession(&agent.MockServices{})
	p := agent.NewPipeline(s, s.GetConfig(), s.GetClients())
	p.SetLargeLLM(newMockAgent(llm.URL))
	p.SetSmallLLM(newMockAgent(llm.URL))
	p.SetTTSClient(&agent.MockTTS{})
	s.SetPipeline(p)
	return s
}

func TestHandleTextMessage_VADEndDispatchesToPipeline(t *testing.T) {
	s := agent.NewTestSession(&agent.MockServices{})
	p := agent.NewTestPipeline(s, s.GetClients())
	p.SetVADEndCh(make(chan struct{}, 1))
	s.SetState(agent.StateListening)

	s.HandleTextMessage(agent.WSMessage{Type: "vad_end"})
	if len(p.GetVADEndCh()) != 1 {
		t.Fatal("expected vad_end signal to pipeline")
	}
}

func TestHandleTextMessage_VADStartDispatch(t *testing.T) {
	s := agent.NewTestSession(&agent.MockServices{})
	p := agent.NewPipeline(s, s.GetConfig(), s.GetClients())
	s.SetPipeline(p)
	p.SetASRClient(&blockingASR{})
	p.SetTTSClient(&agent.MockTTS{})

	s.HandleTextMessage(agent.WSMessage{Type: "vad_start"})
	waitUntil(t, time.Second, func() bool { return s.GetState() == agent.StateListening }, "vad_start should switch to listening")
	s.CancelCurrentPipeline()
}

func TestHandleTextInput_FromProcessing_InterruptsAndProcesses(t *testing.T) {
	s := newRunnableSessionForTextInput(t)
	s.SetState(agent.StateProcessing)

	s.GetPipeline().WriteRawTokens("<think>未完成")
	s.HandleTextInput(agent.WSMessage{Type: "text_input", Text: "新的问题"})

	waitUntil(t, 2*time.Second, func() bool { return s.GetState() == agent.StateIdle }, "processing input did not finish")
	if len(s.GetPipeline().GetHistory().Messages()) == 0 {
		t.Fatal("history should contain processed messages")
	}
	foundUser := false
	for _, m := range s.GetPipeline().GetHistory().Messages() {
		if m.Role == "user" && strings.Contains(m.Content, "新的问题") {
			foundUser = true
			break
		}
	}
	if !foundUser {
		t.Fatalf("expected user input in history, got: %+v", s.GetPipeline().GetHistory().Messages())
	}
}

func TestHandleTextInput_FromSpeaking_InterruptsAndProcesses(t *testing.T) {
	s := newRunnableSessionForTextInput(t)
	s.SetState(agent.StateSpeaking)

	s.HandleTextInput(agent.WSMessage{Type: "text_input", Text: "继续讲"})

	waitUntil(t, 2*time.Second, func() bool { return s.GetState() == agent.StateIdle }, "speaking input did not finish")
	if len(s.GetPipeline().GetHistory().Messages()) == 0 {
		t.Fatal("history should contain processed messages")
	}
}

func TestHandleTextInput_FromListening_InterruptsAndProcesses(t *testing.T) {
	s := newRunnableSessionForTextInput(t)
	s.SetState(agent.StateListening)

	s.HandleTextInput(agent.WSMessage{Type: "text_input", Text: "直接按打字提交"})

	waitUntil(t, 2*time.Second, func() bool { return s.GetState() == agent.StateIdle }, "listening input did not finish")
	foundUser := false
	for _, m := range s.GetPipeline().GetHistory().Messages() {
		if m.Role == "user" && strings.Contains(m.Content, "直接按打字提交") {
			foundUser = true
			break
		}
	}
	if !foundUser {
		t.Fatalf("expected typed user input in history, got: %+v", s.GetPipeline().GetHistory().Messages())
	}
}
