package asr_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/websocket"
	asrpkg "voiceagent/internal/asr"
	"voiceagent/internal/doubao"
)

func TestNewDouBaoASRClient(t *testing.T) {
	cfg := asrpkg.DouBaoASRConfig{AppKey: "ak", AccessKey: "sk", ResourceId: "res"}
	c := asrpkg.NewDouBaoASRClient(cfg)
	if c.Config().AppKey != "ak" {
		t.Fatal("config not set")
	}
}

func TestDouBaoASR_RecognizeStream_FinalResult(t *testing.T) {
	srv := mockDouBaoASRServer(func(conn *websocket.Conn) {
		conn.ReadMessage()

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

		sendServerResp(conn, []byte(`{"result":{"text":""}}`), doubao.FlagLastData)
	})
	defer srv.Close()

	cfg := asrpkg.DouBaoASRConfig{AppKey: "ak", AccessKey: "sk", ResourceId: "res"}
	c := asrpkg.NewDouBaoASRClient(cfg)
	testDouBaoASRFlow(t, srv, c)
}

func testDouBaoASRFlow(t *testing.T, srv *httptest.Server, c *asrpkg.DouBaoASRClient) {
	t.Helper()

	endpoint := wsURL(srv)
	header := http.Header{}
	cfg := c.Config()
	header.Set("X-Api-App-Key", cfg.AppKey)
	header.Set("X-Api-Access-Key", cfg.AccessKey)
	header.Set("X-Api-Resource-Id", cfg.ResourceId)

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
		conn.ReadMessage()
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
