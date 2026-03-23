package tts_test

import (
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
	"voiceagent/internal/doubao"
)

var ttsWsUpgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

func ttsSrvURL(s *httptest.Server) string {
	return "ws" + strings.TrimPrefix(s.URL, "http")
}

func ttsDial(t *testing.T, srv *httptest.Server) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial(ttsSrvURL(srv), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	return conn
}

func ttsSendConfigFrame(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{"audio": map[string]any{"format": "pcm"}})
	h := doubao.BuildHeader(doubao.MsgTypeFullClientReq, doubao.FlagNoSeq, doubao.SerJSON, doubao.CompNone)
	frame := doubao.BuildFrame(h, payload)
	if err := conn.WriteMessage(websocket.BinaryMessage, frame); err != nil {
		t.Fatalf("send config: %v", err)
	}
}

func mockDouBaoTTSServer(handler func(conn *websocket.Conn)) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := ttsWsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		handler(conn)
	}))
}

func sendTTSAudioFrame(conn *websocket.Conn, audio []byte, isLast bool) error {
	flags := byte(doubao.FlagNoSeq)
	if isLast {
		flags = doubao.FlagLastData
	}
	h := doubao.BuildHeader(doubao.MsgTypeAudioOnlyResp, flags, doubao.SerNone, doubao.CompNone)
	data := make([]byte, 4+len(audio))
	copy(data[0:4], h[:])
	copy(data[4:], audio)
	return conn.WriteMessage(websocket.BinaryMessage, data)
}

func sendTTSErrorFrame(conn *websocket.Conn, code uint32, msg string) error {
	hdr := doubao.BuildHeader(doubao.MsgTypeError, 0, 0, 0)
	data := make([]byte, 12+len(msg))
	copy(data[0:4], hdr[:])
	binary.BigEndian.PutUint32(data[4:8], code)
	binary.BigEndian.PutUint32(data[8:12], uint32(len(msg)))
	copy(data[12:], msg)
	return conn.WriteMessage(websocket.BinaryMessage, data)
}

func sendTTSServerAckFrame(conn *websocket.Conn) error {
	h := doubao.BuildHeader(doubao.MsgTypeFullServerResp, doubao.FlagNoSeq, doubao.SerJSON, doubao.CompNone)
	payload := []byte(`{"status":"ok"}`)
	frame := doubao.BuildFrame(h, payload)
	return conn.WriteMessage(websocket.BinaryMessage, frame)
}
