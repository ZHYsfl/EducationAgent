package asr_test

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

func wsURL(s *httptest.Server) string {
	return "ws" + strings.TrimPrefix(s.URL, "http")
}

var wsUpgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

func mockDouBaoASRServer(handler func(conn *websocket.Conn)) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := wsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		handler(conn)
	}))
}

func sendServerResp(conn *websocket.Conn, payload []byte, flags byte) error {
	h := doubao.BuildHeader(doubao.MsgTypeFullServerResp, flags, doubao.SerJSON, doubao.CompNone)
	frame := doubao.BuildFrame(h, payload)
	return conn.WriteMessage(websocket.BinaryMessage, frame)
}

func sendAudioResp(conn *websocket.Conn, audio []byte, flags byte) error {
	h := doubao.BuildHeader(doubao.MsgTypeAudioOnlyResp, flags, doubao.SerNone, doubao.CompNone)
	data := make([]byte, 4+len(audio))
	copy(data[0:4], h[:])
	copy(data[4:], audio)
	return conn.WriteMessage(websocket.BinaryMessage, data)
}

func sendErrorResp(conn *websocket.Conn, code uint32, msg string) error {
	hdr := doubao.BuildHeader(doubao.MsgTypeError, 0, 0, 0)
	data := make([]byte, 12+len(msg))
	copy(data[0:4], hdr[:])
	binary.BigEndian.PutUint32(data[4:8], code)
	binary.BigEndian.PutUint32(data[8:12], uint32(len(msg)))
	copy(data[12:], msg)
	return conn.WriteMessage(websocket.BinaryMessage, data)
}

func dialMockWS(t *testing.T, srv *httptest.Server) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial(wsURL(srv), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	return conn
}

func sendConfigFrame(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{"audio": map[string]any{"format": "pcm"}})
	h := doubao.BuildHeader(doubao.MsgTypeFullClientReq, doubao.FlagNoSeq, doubao.SerJSON, doubao.CompNone)
	frame := doubao.BuildFrame(h, payload)
	if err := conn.WriteMessage(websocket.BinaryMessage, frame); err != nil {
		t.Fatalf("send config: %v", err)
	}
}
