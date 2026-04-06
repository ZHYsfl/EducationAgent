package integration

import (
	"testing"
	"time"

	"github.com/gorilla/websocket"
	agent "voiceagent/agent"
)

// TestE2E_RequirementsCollection validates requirements-related WS messages in current protocol.
func TestE2E_RequirementsCollection(t *testing.T) {
	srv, wsURL := setupTestServer(t)
	defer srv.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// add_reference_files is part of requirements collection in current protocol.
	sendMsg(t, conn, agent.WSMessage{
		Type: "add_reference_files",
		Files: []struct {
			FileID      string `json:"file_id"`
			FileURL     string `json:"file_url"`
			FileType    string `json:"file_type"`
			Instruction string `json:"instruction,omitempty"`
		}{
			{
				FileID:      "f_1",
				FileURL:     "https://example.com/file.pdf",
				FileType:    "pdf",
				Instruction: "作为课程参考",
			},
		},
	})

	// Trigger processing round and validate normal state transitions.
	sendMsg(t, conn, agent.WSMessage{
		Type: "text_input",
		Text: "我要做高等数学导数课件，面向大一学生",
	})

	msg := waitForMessageType(t, conn, "transcript", 2*time.Second)
	if msg.Text == "" {
		t.Fatal("expected transcript after text_input")
	}

	waitForState(t, conn, "idle", 3*time.Second)
}
