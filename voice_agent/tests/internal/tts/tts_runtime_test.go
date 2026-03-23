package tts_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/websocket"
	"voiceagent/internal/doubao"
	ttspkg "voiceagent/internal/tts"
)

func TestDouBaoTTSClient_Synthesize_EndToEnd(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := ttsWsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		_, _, _ = conn.ReadMessage() // request frame

		ack := doubao.BuildFrame(doubao.BuildHeader(doubao.MsgTypeFullServerResp, doubao.FlagNoSeq, doubao.SerJSON, doubao.CompNone), []byte(`{"status":"ok"}`))
		_ = conn.WriteMessage(websocket.BinaryMessage, ack)

		h1 := doubao.BuildHeader(doubao.MsgTypeAudioOnlyResp, doubao.FlagNoSeq, doubao.SerNone, doubao.CompNone)
		h2 := doubao.BuildHeader(doubao.MsgTypeAudioOnlyResp, doubao.FlagLastData, doubao.SerNone, doubao.CompNone)
		_ = conn.WriteMessage(websocket.BinaryMessage, append(h1[:], []byte{0x01, 0x02}...))
		_ = conn.WriteMessage(websocket.BinaryMessage, append(h2[:], []byte{0x03}...))
	}))
	defer srv.Close()

	restore := ttspkg.SetDouBaoTTSEndpointForTest(ttsSrvURL(srv))
	defer restore()

	c := ttspkg.NewDouBaoTTSClient(ttspkg.DouBaoTTSConfig{
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
		conn, err := ttsWsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_, _, _ = conn.ReadMessage() // request
		_ = sendTTSErrorFrame(conn, 5001, "quota")
	}))
	defer srv.Close()

	restore := ttspkg.SetDouBaoTTSEndpointForTest(ttsSrvURL(srv))
	defer restore()

	c := ttspkg.NewDouBaoTTSClient(ttspkg.DouBaoTTSConfig{AppId: "app", Token: "tok", Cluster: "clu", VoiceType: "voice"})
	ch, err := c.Synthesize(context.Background(), "hello", 4)
	if err != nil {
		t.Fatalf("Synthesize error: %v", err)
	}
	for range ch {
		t.Fatal("expected no audio on error frame")
	}
}

func TestDouBaoTTSClient_Synthesize_ConnectError(t *testing.T) {
	restore := ttspkg.SetDouBaoTTSEndpointForTest("ws://127.0.0.1:1/unreachable")
	defer restore()

	c := ttspkg.NewDouBaoTTSClient(ttspkg.DouBaoTTSConfig{AppId: "app", Token: "tok", Cluster: "clu", VoiceType: "voice"})
	ch, err := c.Synthesize(context.Background(), "hello", 2)
	if err == nil {
		t.Fatal("expected connect error")
	}
	for range ch {
		t.Fatal("channel should be closed on connect error")
	}
}

func TestDouBaoTTSClient_Synthesize_InvalidHeaderThenValid(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := ttsWsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_, _, _ = conn.ReadMessage() // request

		_ = conn.WriteMessage(websocket.BinaryMessage, []byte{0x00})

		h := doubao.BuildHeader(doubao.MsgTypeAudioOnlyResp, doubao.FlagLastData, doubao.SerNone, doubao.CompNone)
		_ = conn.WriteMessage(websocket.BinaryMessage, append(h[:], []byte{0x7F}...))
	}))
	defer srv.Close()

	restore := ttspkg.SetDouBaoTTSEndpointForTest(ttsSrvURL(srv))
	defer restore()

	c := ttspkg.NewDouBaoTTSClient(ttspkg.DouBaoTTSConfig{AppId: "app", Token: "tok", Cluster: "clu", VoiceType: "voice"})
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
		conn, err := ttsWsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		_, _, _ = conn.ReadMessage() // request

		h := doubao.BuildHeader(doubao.MsgTypeAudioOnlyResp, doubao.FlagPosSeq, doubao.SerNone, doubao.CompNone)
		_ = conn.WriteMessage(websocket.BinaryMessage, h[:])
	}))
	defer srv.Close()

	restore := ttspkg.SetDouBaoTTSEndpointForTest(ttsSrvURL(srv))
	defer restore()

	c := ttspkg.NewDouBaoTTSClient(ttspkg.DouBaoTTSConfig{AppId: "app", Token: "tok", Cluster: "clu", VoiceType: "voice"})
	ch, err := c.Synthesize(context.Background(), "hello", 4)
	if err != nil {
		t.Fatalf("Synthesize error: %v", err)
	}
	for range ch {
		t.Fatal("expected no chunks on malformed audio frame")
	}
}
