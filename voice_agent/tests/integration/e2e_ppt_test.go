package integration

import (
	"testing"
	"time"

	"github.com/gorilla/websocket"
	agent "voiceagent/agent"
)

// TestE2E_PPTWorkflow validates current WS protocol behavior for text-driven flow.
func TestE2E_PPTWorkflow(t *testing.T) {
	srv, wsURL := setupTestServer(t)
	defer srv.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// The current client protocol enters workflow via text_input.
	sendMsg(t, conn, agent.WSMessage{
		Type: "text_input",
		Text: "帮我做一个 Python 基础课件",
	})

	msg := waitForMessageType(t, conn, "transcript", 2*time.Second)
	if msg.Text == "" {
		t.Fatal("expected non-empty transcript")
	}

	// page_navigate is still part of protocol; this should not break session flow.
	sendMsg(t, conn, agent.WSMessage{
		Type:   "page_navigate",
		TaskID: "task_mock",
		PageID: "page_1",
	})

	// Follow-up edit request should continue to process normally.
	sendMsg(t, conn, agent.WSMessage{
		Type: "text_input",
		Text: "把标题改得更醒目一点",
	})

	waitForState(t, conn, "processing", 2*time.Second)
	waitForState(t, conn, "idle", 3*time.Second)
}
