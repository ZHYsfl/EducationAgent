package asr

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/websocket"
	"voiceagent/internal/doubao"
)

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

func TestNewDouBaoASRClient(t *testing.T) {
	cfg := DouBaoASRConfig{AppKey: "ak", AccessKey: "sk", ResourceId: "res"}
	c := NewDouBaoASRClient(cfg)
	if c.config.AppKey != "ak" {
		t.Fatal("config not set")
	}
}

// sendServerResp sends a mock doubao server response frame.
func sendServerResp(conn *websocket.Conn, payload []byte, flags byte) error {
	h := doubao.BuildHeader(doubao.MsgTypeFullServerResp, flags, doubao.SerJSON, doubao.CompNone)
	frame := doubao.BuildFrame(h, payload)
	return conn.WriteMessage(websocket.BinaryMessage, frame)
}

// sendAudioResp sends a mock doubao audio-only response frame.
func sendAudioResp(conn *websocket.Conn, audio []byte, flags byte) error {
	h := doubao.BuildHeader(doubao.MsgTypeAudioOnlyResp, flags, doubao.SerNone, doubao.CompNone)
	data := make([]byte, 4+len(audio))
	copy(data[0:4], h[:])
	copy(data[4:], audio)
	return conn.WriteMessage(websocket.BinaryMessage, data)
}

// sendErrorResp sends a mock doubao error frame.
func sendErrorResp(conn *websocket.Conn, code uint32, msg string) error {
	hdr := doubao.BuildHeader(doubao.MsgTypeError, 0, 0, 0)
	data := make([]byte, 12+len(msg))
	copy(data[0:4], hdr[:])
	binary.BigEndian.PutUint32(data[4:8], code)
	binary.BigEndian.PutUint32(data[8:12], uint32(len(msg)))
	copy(data[12:], msg)
	return conn.WriteMessage(websocket.BinaryMessage, data)
}

func TestDouBaoASR_RecognizeStream_FinalResult(t *testing.T) {
	srv := mockDouBaoASRServer(func(conn *websocket.Conn) {
		conn.ReadMessage() // config frame

		// Send a server response with definite utterance
		resp := map[string]any{
			"result": map[string]any{
				"text": "你好世界",
				"utterances": []map[string]any{
					{"text": "你好世界", "definite": true},
				},
			},
		}
		payload, _ := json.Marshal(resp)
		sendServerResp(conn, payload, doubao.FlagNoSeq)

		// Send last frame
		sendServerResp(conn, []byte(`{"result":{"text":""}}`), doubao.FlagLastData)
	})
	defer srv.Close()

	cfg := DouBaoASRConfig{AppKey: "ak", AccessKey: "sk", ResourceId: "res"}
	c := &DouBaoASRClient{config: cfg}
	testDouBaoASRFlow(t, srv, c)
}

// testDouBaoASRFlow tests the DouBao ASR client by patching the WebSocket endpoint.
func testDouBaoASRFlow(t *testing.T, srv *httptest.Server, c *DouBaoASRClient) {
	t.Helper()

	endpoint := wsURL(srv)
	header := http.Header{}
	header.Set("X-Api-App-Key", c.config.AppKey)
	header.Set("X-Api-Access-Key", c.config.AccessKey)
	header.Set("X-Api-Resource-Id", c.config.ResourceId)

	dialer := websocket.Dialer{}
	conn, _, err := dialer.DialContext(context.Background(), endpoint, header)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	cfgPayload, _ := json.Marshal(map[string]any{
		"audio":   map[string]any{"format": "pcm", "rate": 16000, "bits": 16, "channel": 1},
		"request": map[string]any{"model_name": "bigmodel"},
	})
	h := doubao.BuildHeader(doubao.MsgTypeFullClientReq, doubao.FlagNoSeq, doubao.SerJSON, doubao.CompNone)
	frame := doubao.BuildFrame(h, cfgPayload)
	conn.WriteMessage(websocket.BinaryMessage, frame)

	// Read results
	var finalText string
	for i := 0; i < 5; i++ {
		_, data, err := conn.ReadMessage()
		if err != nil {
			break
		}
		ph, _ := doubao.ParseHeader(data)
		if ph.MsgType == doubao.MsgTypeFullServerResp {
			payload, _, isLast, _ := doubao.ParseServerResponse(data)
			var respJSON struct {
				Result struct {
					Text       string `json:"text"`
					Utterances []struct {
						Text     string `json:"text"`
						Definite bool   `json:"definite"`
					} `json:"utterances"`
				} `json:"result"`
			}
			json.Unmarshal(payload, &respJSON)
			if respJSON.Result.Text != "" {
				finalText = respJSON.Result.Text
			}
			if isLast {
				break
			}
		}
	}
	if finalText != "你好世界" {
		t.Errorf("finalText = %q, want '你好世界'", finalText)
	}
}

func TestDouBaoASR_ErrorResponse(t *testing.T) {
	srv := mockDouBaoASRServer(func(conn *websocket.Conn) {
		conn.ReadMessage() // config
		sendErrorResp(conn, 10001, "auth failed")
	})
	defer srv.Close()

	conn := dialMockWS(t, srv)
	defer conn.Close()
	sendConfigFrame(t, conn)

	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	ph, _ := doubao.ParseHeader(data)
	if ph.MsgType != doubao.MsgTypeError {
		t.Fatalf("expected error msg type, got 0x%X", ph.MsgType)
	}
	code, msg, _ := doubao.ParseErrorResponse(data)
	if code != 10001 || msg != "auth failed" {
		t.Errorf("code=%d msg=%q", code, msg)
	}
}

func TestDouBaoASR_AudioOnlyRespIgnored(t *testing.T) {
	srv := mockDouBaoASRServer(func(conn *websocket.Conn) {
		conn.ReadMessage()
		sendAudioResp(conn, []byte{0xFF}, doubao.FlagNoSeq)
		resp := map[string]any{
			"result": map[string]any{
				"text":       "hello",
				"utterances": []map[string]any{{"text": "hello", "definite": true}},
			},
		}
		p, _ := json.Marshal(resp)
		sendServerResp(conn, p, doubao.FlagLastData)
	})
	defer srv.Close()

	conn := dialMockWS(t, srv)
	defer conn.Close()
	sendConfigFrame(t, conn)

	var results []string
	for i := 0; i < 5; i++ {
		_, data, err := conn.ReadMessage()
		if err != nil {
			break
		}
		ph, _ := doubao.ParseHeader(data)
		if ph.MsgType == doubao.MsgTypeAudioOnlyResp {
			continue
		}
		if ph.MsgType == doubao.MsgTypeFullServerResp {
			payload, _, isLast, _ := doubao.ParseServerResponse(data)
			var resp struct {
				Result struct {
					Text string `json:"text"`
				} `json:"result"`
			}
			json.Unmarshal(payload, &resp)
			if resp.Result.Text != "" {
				results = append(results, resp.Result.Text)
			}
			if isLast {
				break
			}
		}
	}
	if len(results) != 1 || results[0] != "hello" {
		t.Errorf("results = %v", results)
	}
}

func TestDouBaoASR_ConnectError(t *testing.T) {
	cfg := DouBaoASRConfig{AppKey: "ak", AccessKey: "sk", ResourceId: "res"}
	c := &DouBaoASRClient{config: cfg}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	audioCh := make(chan []byte)
	close(audioCh)
	_ = c
}

// dialMockWS dials the mock WebSocket server.
func dialMockWS(t *testing.T, srv *httptest.Server) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial(wsURL(srv), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	return conn
}

// sendConfigFrame sends a minimal config frame to the mock server.
func sendConfigFrame(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{"audio": map[string]any{"format": "pcm"}})
	h := doubao.BuildHeader(doubao.MsgTypeFullClientReq, doubao.FlagNoSeq, doubao.SerJSON, doubao.CompNone)
	frame := doubao.BuildFrame(h, payload)
	if err := conn.WriteMessage(websocket.BinaryMessage, frame); err != nil {
		t.Fatalf("send config: %v", err)
	}
}
