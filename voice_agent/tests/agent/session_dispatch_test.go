package agent_test

import (
	agent "voiceagent/agent"
	"context"
	"strings"
	"testing"
	"time"
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

func TestHandleTextMessage_TaskInitDispatch(t *testing.T) {
	s := agent.NewTestSession(&agent.MockServices{})
	_ = agent.NewTestPipelineWithTTS(s, s.GetClients())

	s.HandleTextMessage(agent.WSMessage{
		Type:     "task_init",
		Topic:    "代数",
		Audience: "高一",
	})

	s.RLockReqMu()
	defer s.RUnlockReqMu()
	if s.GetRequirements() == nil {
		t.Fatal("requirements should be initialized by task_init")
	}
	if s.GetRequirements().Topic != "代数" {
		t.Fatalf("topic=%q", s.GetRequirements().Topic)
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

func TestHandleTextMessage_RequirementsConfirmDispatch(t *testing.T) {
	m := &agent.MockServices{
		InitPPTFn: func(ctx context.Context, req agent.PPTInitRequest) (agent.PPTInitResponse, error) {
			return agent.PPTInitResponse{TaskID: "task_from_confirm"}, nil
		},
	}
	s := agent.NewTestSession(m)
	_ = agent.NewTestPipelineWithTTS(s, m)
	s.SetRequirements(makeFullRequirements())
	s.GetRequirements().Status = "confirming"
	s.GetRequirements().SessionID = s.SessionID
	s.GetRequirements().UserID = s.UserID

	agent.RegisterSession(s)
	defer agent.UnregisterSession(s)

	ok := true
	s.HandleTextMessage(agent.WSMessage{
		Type:      "requirements_confirm",
		Confirmed: &ok,
	})
	waitUntil(t, time.Second, func() bool { return s.GetActiveTask() == "task_from_confirm" }, "requirements_confirm should trigger task creation")
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
