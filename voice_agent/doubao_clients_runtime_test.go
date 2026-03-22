package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
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
		_ = conn.WriteMessage(websocket.BinaryMessage, buildFrame(buildHeader(msgTypeFullServerResp, flagNoSeq, serJSON, compNone), p1))

		resp2 := map[string]any{
			"result": map[string]any{
				"text":       "你好",
				"utterances": []map[string]any{{"text": "你好", "definite": true}},
			},
		}
		p2, _ := json.Marshal(resp2)
		_ = conn.WriteMessage(websocket.BinaryMessage, buildFrame(buildHeader(msgTypeFullServerResp, flagNoSeq, serJSON, compNone), p2))

		last := []byte(`{"result":{"text":""}}`)
		_ = conn.WriteMessage(websocket.BinaryMessage, buildFrame(buildHeader(msgTypeFullServerResp, flagLastData, serJSON, compNone), last))

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
		_ = conn.WriteMessage(websocket.BinaryMessage, buildFrame(buildHeader(msgTypeFullServerResp, flagLastData, serJSON, compNone), p))
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
		h := buildHeader(msgTypeAudioOnlyResp, flagNoSeq, serNone, compNone)
		_ = conn.WriteMessage(websocket.BinaryMessage, append(h[:], []byte{0x11, 0x22}...))

		resp := map[string]any{
			"result": map[string]any{
				"text":       "可见文本",
				"utterances": []map[string]any{{"text": "可见文本", "definite": true}},
			},
		}
		p, _ := json.Marshal(resp)
		_ = conn.WriteMessage(websocket.BinaryMessage, buildFrame(buildHeader(msgTypeFullServerResp, flagLastData, serJSON, compNone), p))
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

func TestDouBaoTTSClient_Synthesize_EndToEnd(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := wsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		_, _, _ = conn.ReadMessage() // request frame

		ack := buildFrame(buildHeader(msgTypeFullServerResp, flagNoSeq, serJSON, compNone), []byte(`{"status":"ok"}`))
		_ = conn.WriteMessage(websocket.BinaryMessage, ack)

		h1 := buildHeader(msgTypeAudioOnlyResp, flagNoSeq, serNone, compNone)
		h2 := buildHeader(msgTypeAudioOnlyResp, flagLastData, serNone, compNone)
		_ = conn.WriteMessage(websocket.BinaryMessage, append(h1[:], []byte{0x01, 0x02}...))
		_ = conn.WriteMessage(websocket.BinaryMessage, append(h2[:], []byte{0x03}...))
	}))
	defer srv.Close()

	old := doubaoTTSEndpoint
	doubaoTTSEndpoint = wsURL(srv)
	defer func() { doubaoTTSEndpoint = old }()

	c := NewDouBaoTTSClient(DouBaoTTSConfig{
		AppId: "app", Token: "tok", Cluster: "clu", VoiceType: "voice",
	})
	ch, err := c.Synthesize(context.Background(), "你好", 8)
	if err != nil {
		t.Fatalf("Synthesize error: %v", err)
	}

	var chunks [][]byte
	for b := range ch {
		chunks = append(chunks, b)
	}
	if len(chunks) != 2 {
		t.Fatalf("expected 2 audio chunks, got %d", len(chunks))
	}
}

func TestDouBaoTTSClient_Synthesize_ErrorFrame(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := wsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_, _, _ = conn.ReadMessage() // request
		_ = sendTTSErrorFrame(conn, 5001, "quota")
	}))
	defer srv.Close()

	old := doubaoTTSEndpoint
	doubaoTTSEndpoint = wsURL(srv)
	defer func() { doubaoTTSEndpoint = old }()

	c := NewDouBaoTTSClient(DouBaoTTSConfig{AppId: "app", Token: "tok", Cluster: "clu", VoiceType: "voice"})
	ch, err := c.Synthesize(context.Background(), "hello", 4)
	if err != nil {
		t.Fatalf("Synthesize error: %v", err)
	}
	for range ch {
		t.Fatal("expected no audio on error frame")
	}
}

func TestDouBaoTTSClient_Synthesize_ConnectError(t *testing.T) {
	old := doubaoTTSEndpoint
	doubaoTTSEndpoint = "ws://127.0.0.1:1/unreachable"
	defer func() { doubaoTTSEndpoint = old }()

	c := NewDouBaoTTSClient(DouBaoTTSConfig{AppId: "app", Token: "tok", Cluster: "clu", VoiceType: "voice"})
	ch, err := c.Synthesize(context.Background(), "hello", 2)
	if err == nil {
		t.Fatal("expected connect error")
	}
	// error path returns a closed channel
	for range ch {
		t.Fatal("channel should be closed on connect error")
	}
}

func TestDouBaoTTSClient_Synthesize_InvalidHeaderThenValid(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := wsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_, _, _ = conn.ReadMessage() // request

		// invalid frame (parse header error)
		_ = conn.WriteMessage(websocket.BinaryMessage, []byte{0x00})

		h := buildHeader(msgTypeAudioOnlyResp, flagLastData, serNone, compNone)
		_ = conn.WriteMessage(websocket.BinaryMessage, append(h[:], []byte{0x7F}...))
	}))
	defer srv.Close()

	old := doubaoTTSEndpoint
	doubaoTTSEndpoint = wsURL(srv)
	defer func() { doubaoTTSEndpoint = old }()

	c := NewDouBaoTTSClient(DouBaoTTSConfig{AppId: "app", Token: "tok", Cluster: "clu", VoiceType: "voice"})
	ch, err := c.Synthesize(context.Background(), "hello", 4)
	if err != nil {
		t.Fatalf("Synthesize error: %v", err)
	}
	var got [][]byte
	for b := range ch {
		got = append(got, b)
	}
	if len(got) != 1 || len(got[0]) != 1 {
		t.Fatalf("unexpected chunks: %+v", got)
	}
}

func TestDouBaoTTSClient_Synthesize_AudioParseError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := wsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_, _, _ = conn.ReadMessage() // request

		// malformed audio frame: flagPosSeq but no sequence bytes
		h := buildHeader(msgTypeAudioOnlyResp, flagPosSeq, serNone, compNone)
		_ = conn.WriteMessage(websocket.BinaryMessage, h[:])
	}))
	defer srv.Close()

	old := doubaoTTSEndpoint
	doubaoTTSEndpoint = wsURL(srv)
	defer func() { doubaoTTSEndpoint = old }()

	c := NewDouBaoTTSClient(DouBaoTTSConfig{AppId: "app", Token: "tok", Cluster: "clu", VoiceType: "voice"})
	ch, err := c.Synthesize(context.Background(), "hello", 4)
	if err != nil {
		t.Fatalf("Synthesize error: %v", err)
	}
	for range ch {
		t.Fatal("expected no chunks on malformed audio frame")
	}
}

