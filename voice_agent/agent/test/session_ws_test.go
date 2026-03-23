package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"voiceagent/internal/asr"
)

func newWSConnPair(t *testing.T) (*websocket.Conn, *websocket.Conn, func()) {
	t.Helper()

	serverConnCh := make(chan *websocket.Conn, 1)
	hold := make(chan struct{})
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		serverConnCh <- conn
		<-hold
	}))

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	clientConn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		srv.Close()
		t.Fatalf("dial websocket: %v", err)
	}

	serverConn := <-serverConnCh
	cleanup := func() {
		close(hold)
		_ = clientConn.Close()
		_ = serverConn.Close()
		srv.Close()
	}
	return clientConn, serverConn, cleanup
}

func waitUntil(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal(msg)
}

type blockingASR struct{}

func (b *blockingASR) RecognizeStream(ctx context.Context, audioCh <-chan []byte, resultBufSize int) (<-chan asr.ASRResult, error) {
	ch := make(chan asr.ASRResult)
	go func() {
		<-ctx.Done()
		close(ch)
	}()
	return ch, nil
}

func TestNewSession_DefaultIDs_AndPipeline(t *testing.T) {
	_, serverConn, cleanup := newWSConnPair(t)
	defer cleanup()

	cfg := newTestConfig()
	cfg.DefaultUserID = "u_default"
	s := NewSession(serverConn, cfg, &mockServices{}, "", "")

	if s.SessionID == "" || !strings.HasPrefix(s.SessionID, "sess_") {
		t.Fatalf("unexpected session id: %q", s.SessionID)
	}
	if s.UserID != "u_default" {
		t.Fatalf("unexpected user id: %q", s.UserID)
	}
	if s.pipeline == nil {
		t.Fatal("pipeline should not be nil")
	}
}

func TestSessionWriteLoop_WritesTextAndBinary(t *testing.T) {
	clientConn, serverConn, cleanup := newWSConnPair(t)
	defer cleanup()

	s := NewSession(serverConn, newTestConfig(), &mockServices{}, "sess_wl", "u1")
	done := make(chan struct{})
	go func() {
		s.writeLoop()
		close(done)
	}()

	s.SendJSON(WSMessage{Type: "status", State: "idle"})
	clientConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	msgType, data, err := clientConn.ReadMessage()
	if err != nil {
		t.Fatalf("read text: %v", err)
	}
	if msgType != websocket.TextMessage {
		t.Fatalf("msg type = %d, want text", msgType)
	}
	var msg WSMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if msg.Type != "status" {
		t.Fatalf("unexpected msg type: %s", msg.Type)
	}

	s.SendAudio([]byte{1, 2, 3})
	msgType, data, err = clientConn.ReadMessage()
	if err != nil {
		t.Fatalf("read binary: %v", err)
	}
	if msgType != websocket.BinaryMessage || len(data) != 3 {
		t.Fatalf("unexpected binary frame type=%d len=%d", msgType, len(data))
	}

	s.Close()
	waitUntil(t, time.Second, func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	}, "writeLoop did not exit")
}

func TestSessionReadLoop_DispatchesTextAndBinary(t *testing.T) {
	clientConn, serverConn, cleanup := newWSConnPair(t)
	defer cleanup()

	s := NewSession(serverConn, newTestConfig(), &mockServices{}, "sess_rl", "u1")
	done := make(chan struct{})
	go func() {
		s.readLoop()
		close(done)
	}()

	if err := clientConn.WriteJSON(WSMessage{Type: "page_navigate", TaskID: "task_1", PageID: "p_1"}); err != nil {
		t.Fatalf("write json: %v", err)
	}
	waitUntil(t, time.Second, func() bool { return s.GetViewingPageID() == "p_1" }, "page navigate not handled")

	s.SetState(StateListening)
	s.pipeline.audioCh = make(chan []byte, 1)
	if err := clientConn.WriteMessage(websocket.BinaryMessage, []byte{9, 9, 9}); err != nil {
		t.Fatalf("write binary: %v", err)
	}
	waitUntil(t, time.Second, func() bool { return len(s.pipeline.audioCh) == 1 }, "binary audio not dispatched")

	_ = clientConn.Close()
	waitUntil(t, time.Second, func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	}, "readLoop did not exit")
}

func TestSessionRun_StopsOnDisconnect(t *testing.T) {
	clientConn, serverConn, cleanup := newWSConnPair(t)
	defer cleanup()

	s := NewSession(serverConn, newTestConfig(), &mockServices{}, "sess_run", "u1")
	done := make(chan struct{})
	go func() {
		s.Run()
		close(done)
	}()

	if err := clientConn.WriteJSON(WSMessage{Type: "page_navigate", TaskID: "task_run", PageID: "p_run"}); err != nil {
		t.Fatalf("write json: %v", err)
	}
	waitUntil(t, time.Second, func() bool { return s.GetViewingPageID() == "p_run" }, "run did not handle text message")

	_ = clientConn.Close()
	waitUntil(t, 2*time.Second, func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	}, "Run did not stop after disconnect")
}

func TestSessionOnVADStart_StateTransitions(t *testing.T) {
	tests := []SessionState{StateIdle, StateProcessing, StateSpeaking}
	for _, initial := range tests {
		t.Run(initial.String(), func(t *testing.T) {
			s := newTestSession(&mockServices{})
			p := NewPipeline(s, s.config, s.clients)
			s.pipeline = p
			p.asrClient = &blockingASR{}
			p.ttsClient = &mockTTS{}

			s.SetState(initial)
			s.onVADStart()

			waitUntil(t, time.Second, func() bool { return s.GetState() == StateListening }, "state did not transition to listening")

			s.cancelCurrentPipeline()
		})
	}
}

func TestNewPipeline_ModeSelection(t *testing.T) {
	s := &Session{
		writeCh: make(chan writeItem, 4),
		done:    make(chan struct{}),
	}
	m := &mockServices{}

	localCfg := newTestConfig()
	localCfg.ASRMode = "local"
	localCfg.TTSMode = "local"
	p1 := NewPipeline(s, localCfg, m)
	if p1 == nil || p1.asrClient == nil || p1.ttsClient == nil {
		t.Fatal("local mode pipeline init failed")
	}

	remoteCfg := newTestConfig()
	remoteCfg.ASRMode = "remote"
	remoteCfg.TTSMode = "remote"
	remoteCfg.DouBaoASRAppKey = "ak"
	remoteCfg.DouBaoASRAccessKey = "sk"
	remoteCfg.DouBaoASRResourceId = "rid"
	remoteCfg.DouBaoTTSAppId = "app"
	remoteCfg.DouBaoTTSToken = "tok"
	remoteCfg.DouBaoTTSCluster = "clu"
	remoteCfg.DouBaoTTSVoiceType = "voice"
	p2 := NewPipeline(s, remoteCfg, m)
	if p2 == nil || p2.asrClient == nil || p2.ttsClient == nil {
		t.Fatal("remote mode pipeline init failed")
	}
}
