package asr

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"voiceagent/internal/doubao"
)

func TestDouBaoASRClient_RecognizeStream_EndToEnd(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := wsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		_, _, _ = conn.ReadMessage() // config frame

		resp1 := map[string]any{
			"result": map[string]any{
				"text":       "你",
				"utterances": []map[string]any{{"text": "你", "definite": false}},
			},
		}
		p1, _ := json.Marshal(resp1)
		_ = conn.WriteMessage(websocket.BinaryMessage, doubao.BuildFrame(doubao.BuildHeader(doubao.MsgTypeFullServerResp, doubao.FlagNoSeq, doubao.SerJSON, doubao.CompNone), p1))

		resp2 := map[string]any{
			"result": map[string]any{
				"text":       "你好",
				"utterances": []map[string]any{{"text": "你好", "definite": true}},
			},
		}
		p2, _ := json.Marshal(resp2)
		_ = conn.WriteMessage(websocket.BinaryMessage, doubao.BuildFrame(doubao.BuildHeader(doubao.MsgTypeFullServerResp, doubao.FlagNoSeq, doubao.SerJSON, doubao.CompNone), p2))

		last := []byte(`{"result":{"text":""}}`)
		_ = conn.WriteMessage(websocket.BinaryMessage, doubao.BuildFrame(doubao.BuildHeader(doubao.MsgTypeFullServerResp, doubao.FlagLastData, doubao.SerJSON, doubao.CompNone), last))

		time.Sleep(30 * time.Millisecond)
	}))
	defer srv.Close()

	old := doubaoASREndpoint
	doubaoASREndpoint = wsURL(srv)
	defer func() { doubaoASREndpoint = old }()

	c := NewDouBaoASRClient(DouBaoASRConfig{AppKey: "ak", AccessKey: "sk", ResourceId: "rid"})
	audioCh := make(chan []byte, 1)
	audioCh <- []byte{1, 2, 3}
	close(audioCh)

	resultCh, err := c.RecognizeStream(context.Background(), audioCh, 8)
	if err != nil {
		t.Fatalf("RecognizeStream error: %v", err)
	}
	var got []ASRResult
	for r := range resultCh {
		got = append(got, r)
	}
	if len(got) == 0 {
		t.Fatal("expected ASR results")
	}
	if got[len(got)-1].Text != "你好" || !got[len(got)-1].IsFinal {
		t.Fatalf("unexpected final ASR result: %+v", got[len(got)-1])
	}
}

func TestDouBaoASRClient_RecognizeStream_ErrorFrame(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := wsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_, _, _ = conn.ReadMessage() // config
		_ = sendErrorResp(conn, 401, "bad auth")
	}))
	defer srv.Close()

	old := doubaoASREndpoint
	doubaoASREndpoint = wsURL(srv)
	defer func() { doubaoASREndpoint = old }()

	c := NewDouBaoASRClient(DouBaoASRConfig{AppKey: "ak", AccessKey: "sk", ResourceId: "rid"})
	audioCh := make(chan []byte)
	close(audioCh)
	resultCh, err := c.RecognizeStream(context.Background(), audioCh, 4)
	if err != nil {
		t.Fatalf("RecognizeStream error: %v", err)
	}
	for range resultCh {
		t.Fatal("expected no ASR result on error frame")
	}
}

func TestDouBaoASRClient_RecognizeStream_ConnectError(t *testing.T) {
	old := doubaoASREndpoint
	doubaoASREndpoint = "ws://127.0.0.1:1/unreachable"
	defer func() { doubaoASREndpoint = old }()

	c := NewDouBaoASRClient(DouBaoASRConfig{AppKey: "ak", AccessKey: "sk", ResourceId: "rid"})
	audioCh := make(chan []byte)
	close(audioCh)
	_, err := c.RecognizeStream(context.Background(), audioCh, 4)
	if err == nil {
		t.Fatal("expected connect error")
	}
}

func TestDouBaoASRClient_RecognizeStream_InvalidHeaderThenValid(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := wsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_, _, _ = conn.ReadMessage() // config

		// invalid frame (header parse should fail and continue)
		_ = conn.WriteMessage(websocket.BinaryMessage, []byte{0x01})

		resp := map[string]any{
			"result": map[string]any{
				"text":       "最终结果",
				"utterances": []map[string]any{{"text": "最终结果", "definite": true}},
			},
		}
		p, _ := json.Marshal(resp)
		_ = conn.WriteMessage(websocket.BinaryMessage, doubao.BuildFrame(doubao.BuildHeader(doubao.MsgTypeFullServerResp, doubao.FlagLastData, doubao.SerJSON, doubao.CompNone), p))
		time.Sleep(80 * time.Millisecond)
	}))
	defer srv.Close()

	old := doubaoASREndpoint
	doubaoASREndpoint = wsURL(srv)
	defer func() { doubaoASREndpoint = old }()

	c := NewDouBaoASRClient(DouBaoASRConfig{AppKey: "ak", AccessKey: "sk", ResourceId: "rid"})
	audioCh := make(chan []byte)
	close(audioCh)
	resultCh, err := c.RecognizeStream(context.Background(), audioCh, 4)
	if err != nil {
		t.Fatalf("RecognizeStream error: %v", err)
	}
	var got []ASRResult
	for r := range resultCh {
		got = append(got, r)
	}
	if len(got) != 1 || got[0].Text != "最终结果" {
		t.Fatalf("unexpected ASR results: %+v", got)
	}
}

func TestDouBaoASRClient_RecognizeStream_IgnoreNonFullServerResp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := wsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_, _, _ = conn.ReadMessage() // config

		// audio-only response should be ignored by ASR reader
		h := doubao.BuildHeader(doubao.MsgTypeAudioOnlyResp, doubao.FlagNoSeq, doubao.SerNone, doubao.CompNone)
		_ = conn.WriteMessage(websocket.BinaryMessage, append(h[:], []byte{0x11, 0x22}...))

		resp := map[string]any{
			"result": map[string]any{
				"text":       "可见文本",
				"utterances": []map[string]any{{"text": "可见文本", "definite": true}},
			},
		}
		p, _ := json.Marshal(resp)
		_ = conn.WriteMessage(websocket.BinaryMessage, doubao.BuildFrame(doubao.BuildHeader(doubao.MsgTypeFullServerResp, doubao.FlagLastData, doubao.SerJSON, doubao.CompNone), p))
	}))
	defer srv.Close()

	old := doubaoASREndpoint
	doubaoASREndpoint = wsURL(srv)
	defer func() { doubaoASREndpoint = old }()

	c := NewDouBaoASRClient(DouBaoASRConfig{AppKey: "ak", AccessKey: "sk", ResourceId: "rid"})
	audioCh := make(chan []byte)
	close(audioCh)
	resultCh, err := c.RecognizeStream(context.Background(), audioCh, 4)
	if err != nil {
		t.Fatalf("RecognizeStream error: %v", err)
	}
	var got []ASRResult
	for r := range resultCh {
		got = append(got, r)
	}
	if len(got) != 1 || got[0].Text != "可见文本" {
		t.Fatalf("unexpected ASR results: %+v", got)
	}
}
