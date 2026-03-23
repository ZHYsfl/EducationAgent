package agent_test

import (
	agent "voiceagent/agent"
	"math/rand"
	"testing"
	"time"
)

func TestSessionRun_ChaosTraffic_NoCrash(t *testing.T) {
	clientConn, serverConn, cleanup := newWSConnPair(t)
	defer cleanup()

	llm := newMockLLMServer("do not interrupt", []string{"收到", "。"})
	defer llm.Close()

	s := agent.NewSession(serverConn, agent.NewTestConfig(), &agent.MockServices{}, "sess_chaos", "user_chaos")
	s.GetPipeline().SetASRClient(&blockingASR{})
	s.GetPipeline().SetTTSClient(&agent.MockTTS{})
	s.GetPipeline().SetSmallLLM(newMockAgent(llm.URL))
	s.GetPipeline().SetLargeLLM(newMockAgent(llm.URL))

	done := make(chan struct{})
	go func() {
		s.Run()
		close(done)
	}()

	trueVal := true
	msgs := []agent.WSMessage{
		{Type: "page_navigate", TaskID: "task_1", PageID: "p1"},
		{Type: "task_init", Topic: "高数", Audience: "大一", TotalPages: 10},
		{Type: "text_input", Text: "帮我总结这页"},
		{Type: "vad_start"},
		{Type: "vad_end"},
		{Type: "requirements_confirm", Confirmed: &trueVal},
		{Type: "unknown_type"},
	}

	r := rand.New(rand.NewSource(42))
	for i := 0; i < 200; i++ {
		m := msgs[r.Intn(len(msgs))]
		_ = clientConn.WriteJSON(m)
		if i%5 == 0 {
			_ = clientConn.WriteMessage(2, []byte{1, 2, 3, byte(i % 255)}) // binary audio
		}
		if i%17 == 0 {
			_ = clientConn.WriteMessage(1, []byte("not-json-payload")) // invalid JSON
		}
	}

	time.Sleep(300 * time.Millisecond)
	_ = clientConn.Close()

	waitUntil(t, 3*time.Second, func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	}, "session run did not exit under chaos traffic")
}

func TestSessionRun_InterruptStorm_NoDeadlock(t *testing.T) {
	clientConn, serverConn, cleanup := newWSConnPair(t)
	defer cleanup()

	llm := newMockLLMServer("do not interrupt", []string{"先别急", "。"})
	defer llm.Close()

	s := agent.NewSession(serverConn, agent.NewTestConfig(), &agent.MockServices{}, "sess_storm", "user_storm")
	s.GetPipeline().SetASRClient(&blockingASR{})
	s.GetPipeline().SetTTSClient(&agent.MockTTS{})
	s.GetPipeline().SetSmallLLM(newMockAgent(llm.URL))
	s.GetPipeline().SetLargeLLM(newMockAgent(llm.URL))

	done := make(chan struct{})
	go func() {
		s.Run()
		close(done)
	}()

	// high-frequency interrupt-like traffic
	for i := 0; i < 80; i++ {
		_ = clientConn.WriteJSON(agent.WSMessage{Type: "text_input", Text: "第" + agent.Truncate("1234567890", 1) + "次打断"})
		_ = clientConn.WriteJSON(agent.WSMessage{Type: "vad_start"})
		_ = clientConn.WriteJSON(agent.WSMessage{Type: "vad_end"})
	}

	time.Sleep(250 * time.Millisecond)
	_ = clientConn.Close()
	waitUntil(t, 3*time.Second, func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	}, "session run did not exit under interrupt storm")
}
