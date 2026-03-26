package integration

import (
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	agent "voiceagent/agent"
)

// TestE2E_ConcurrentSessions 测试多个会话并发运行
func TestE2E_ConcurrentSessions(t *testing.T) {
	srv, wsURL := setupTestServer(t)
	defer srv.Close()

	const numSessions = 10
	var wg sync.WaitGroup

	for i := 0; i < numSessions; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
			if err != nil {
				t.Errorf("session %d dial: %v", id, err)
				return
			}
			defer conn.Close()

			// 每个会话发送语音交互
			sendMsg(t, conn, agent.WSMessage{Type: "vad_start"})
			time.Sleep(50 * time.Millisecond)

			for j := 0; j < 5; j++ {
				conn.WriteMessage(websocket.BinaryMessage, []byte{byte(id), byte(j)})
				time.Sleep(10 * time.Millisecond)
			}

			sendMsg(t, conn, agent.WSMessage{Type: "vad_end"})
			time.Sleep(100 * time.Millisecond)
		}(i)
	}

	wg.Wait()
}
