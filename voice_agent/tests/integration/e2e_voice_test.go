package integration

import (
	"testing"
	"time"

	"github.com/gorilla/websocket"
	agent "voiceagent/agent"
)

// TestE2E_VoiceInteraction verifies graceful fallback when ASR is not configured in test config.
func TestE2E_VoiceInteraction(t *testing.T) {
	srv, wsURL := setupTestServer(t)
	defer srv.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Start voice flow.
	sendMsg(t, conn, agent.WSMessage{Type: "vad_start"})
	waitForState(t, conn, "listening", time.Second)

	// NewTestConfig leaves ASR URL empty, so pipeline should fail ASR startup and return to idle.
	waitForState(t, conn, "idle", 2*time.Second)
}
