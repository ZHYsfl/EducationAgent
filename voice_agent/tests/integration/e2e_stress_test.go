package integration

import (
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	agent "voiceagent/agent"
)

// TestE2E_ConcurrentInterrupts 测试并发打断场景
func TestE2E_ConcurrentInterrupts(t *testing.T) {
	srv, wsURL := setupTestServer(t)
	defer srv.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	var writeMu sync.Mutex
	safeSend := func(msg agent.WSMessage) {
		writeMu.Lock()
		defer writeMu.Unlock()
		sendMsg(t, conn, msg)
	}

	// 启动一个长时间处理
	safeSend(agent.WSMessage{Type: "text_input", Text: "请详细讲解量子力学"})
	waitForState(t, conn, "processing", time.Second)

	// 并发发送多个打断
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			safeSend(agent.WSMessage{Type: "text_input", Text: "打断"})
			time.Sleep(10 * time.Millisecond)
		}(i)
	}

	wg.Wait()
	time.Sleep(500 * time.Millisecond)

	// 验证系统仍然正常
	safeSend(agent.WSMessage{Type: "text_input", Text: "测试"})
	waitForState(t, conn, "processing", time.Second)
}
