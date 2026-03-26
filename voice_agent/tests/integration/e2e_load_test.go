package integration

import (
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	agent "voiceagent/agent"
)

// TestE2E_HighLoadMultipleTasksPerSession 测试单会话多任务高负载
func TestE2E_HighLoadMultipleTasksPerSession(t *testing.T) {
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

	// 快速创建多个任务
	for i := 0; i < 5; i++ {
		safeSend(agent.WSMessage{
			Type:  "task_init",
			Topic: "任务" + string(rune('A'+i)),
		})
		time.Sleep(50 * time.Millisecond)
	}

	// 并发发送消息到不同任务
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			safeSend(agent.WSMessage{
				Type: "text_input",
				Text: "修改请求",
			})
		}(i)
	}

	wg.Wait()
}
