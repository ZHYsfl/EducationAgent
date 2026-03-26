package integration

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	agent "voiceagent/agent"
)

func setupTestServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	cfg := agent.NewTestConfig()
	services := &agent.MockServices{}

	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		sess, err := agent.NewSession(conn, cfg, services, "", "test_user")
		if err != nil {
			conn.Close()
			return
		}
		agent.RegisterSession(sess)
		defer agent.UnregisterSession(sess)
		sess.Run()
	}))

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	return srv, wsURL
}

func sendMsg(t *testing.T, conn *websocket.Conn, msg agent.WSMessage) {
	t.Helper()
	if err := conn.WriteJSON(msg); err != nil {
		t.Fatalf("write json: %v", err)
	}
}

func waitForState(t *testing.T, conn *websocket.Conn, expectedState string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		var msg agent.WSMessage
		if err := conn.ReadJSON(&msg); err == nil {
			if msg.Type == "status" && msg.State == expectedState {
				return
			}
		}
	}
	t.Fatalf("timeout waiting for state: %s", expectedState)
}

func waitForMessageType(t *testing.T, conn *websocket.Conn, msgType string, timeout time.Duration) agent.WSMessage {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		var msg agent.WSMessage
		if err := conn.ReadJSON(&msg); err == nil {
			if msg.Type == msgType {
				return msg
			}
		}
	}
	t.Fatalf("timeout waiting for message type: %s", msgType)
	return agent.WSMessage{}
}
