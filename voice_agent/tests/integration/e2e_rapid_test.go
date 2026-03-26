package integration

import (
	"testing"
	"time"

	"github.com/gorilla/websocket"
	agent "voiceagent/agent"
)

// TestE2E_RapidFireMessages 测试快速连续消息发送
func TestE2E_RapidFireMessages(t *testing.T) {
	srv, wsURL := setupTestServer(t)
	defer srv.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// 快速发送 100 条消息
	for i := 0; i < 100; i++ {
		sendMsg(t, conn, agent.WSMessage{
			Type: "text_input",
			Text: "快速消息",
		})
	}

	time.Sleep(500 * time.Millisecond)
}
