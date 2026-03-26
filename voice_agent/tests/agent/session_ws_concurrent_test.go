package agent_test

import (
	"sync"
	"testing"

	agent "voiceagent/agent"
)

func TestSession_ConcurrentSendJSON(t *testing.T) {
	_, serverConn, cleanup := newWSConnPair(t)
	defer cleanup()

	s, err := agent.NewSession(serverConn, agent.NewTestConfig(), &agent.MockServices{}, "sess_concurrent", "u1")
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	done := make(chan struct{})
	go func() {
		s.WriteLoop()
		close(done)
	}()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				s.SendJSON(agent.WSMessage{Type: "status", State: "test"})
			}
		}(i)
	}
	wg.Wait()

	s.Close()
	<-done
}

func TestSession_ConcurrentSendAudio(t *testing.T) {
	_, serverConn, cleanup := newWSConnPair(t)
	defer cleanup()

	s, err := agent.NewSession(serverConn, agent.NewTestConfig(), &agent.MockServices{}, "sess_audio", "u1")
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	done := make(chan struct{})
	go func() {
		s.WriteLoop()
		close(done)
	}()

	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				s.SendAudio([]byte{byte(id), byte(j)})
			}
		}(i)
	}
	wg.Wait()

	s.Close()
	<-done
}

func TestSession_ConcurrentReadWrite(t *testing.T) {
	clientConn, serverConn, cleanup := newWSConnPair(t)
	defer cleanup()

	s, err := agent.NewSession(serverConn, agent.NewTestConfig(), &agent.MockServices{}, "sess_rw", "u1")
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	readDone := make(chan struct{})
	writeDone := make(chan struct{})
	go func() {
		s.ReadLoop()
		close(readDone)
	}()
	go func() {
		s.WriteLoop()
		close(writeDone)
	}()

	var wg sync.WaitGroup

	// 只测试服务端并发发送
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				s.SendJSON(agent.WSMessage{Type: "status", State: "idle"})
			}
		}()
	}

	// 客户端串行发送（模拟真实场景）
	go func() {
		for i := 0; i < 100; i++ {
			clientConn.WriteJSON(agent.WSMessage{Type: "page_navigate", PageID: "p1"})
		}
	}()

	wg.Wait()

	clientConn.Close()
	<-readDone
	<-writeDone
}

