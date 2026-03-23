package asr

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func wsURL(s *httptest.Server) string {
	return "ws" + strings.TrimPrefix(s.URL, "http")
}

var wsUpgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

// mockASRServer simulates a Fun-ASR-Nano WebSocket server.
func mockASRServer(handler func(conn *websocket.Conn)) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := wsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		handler(conn)
	}))
}

// ---------------------------------------------------------------------------
// NewASRClient
// ---------------------------------------------------------------------------

func TestNewASRClient(t *testing.T) {
	c := NewASRClient("ws://localhost:9999")
	if c == nil || c.url != "ws://localhost:9999" {
		t.Fatal("NewASRClient failed")
	}
}

// ---------------------------------------------------------------------------
// StartSession
// ---------------------------------------------------------------------------

func TestASRClient_StartSession_Success(t *testing.T) {
	var gotConfig map[string]any
	srv := mockASRServer(func(conn *websocket.Conn) {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return
		}
		json.Unmarshal(msg, &gotConfig)
		time.Sleep(200 * time.Millisecond)
	})
	defer srv.Close()

	c := NewASRClient(wsURL(srv))
	err := c.StartSession(context.Background())
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	defer c.EndSession(context.Background())

	if c.conn == nil {
		t.Fatal("conn should not be nil after StartSession")
	}
}

func TestASRClient_StartSession_BadURL(t *testing.T) {
	c := NewASRClient("ws://127.0.0.1:1") // port that won't accept connections
	err := c.StartSession(context.Background())
	if err == nil {
		t.Fatal("expected connection error")
	}
}

// ---------------------------------------------------------------------------
// SendAudio
// ---------------------------------------------------------------------------

func TestASRClient_SendAudio_NoSession(t *testing.T) {
	c := NewASRClient("ws://localhost:9999")
	err := c.SendAudio(context.Background(), []byte{0x01})
	if err == nil || !strings.Contains(err.Error(), "not started") {
		t.Fatalf("expected 'not started' error, got: %v", err)
	}
}

func TestASRClient_SendAudio_Success(t *testing.T) {
	srv := mockASRServer(func(conn *websocket.Conn) {
		conn.ReadMessage() // config
		conn.ReadMessage() // audio
		time.Sleep(100 * time.Millisecond)
	})
	defer srv.Close()

	c := NewASRClient(wsURL(srv))
	c.StartSession(context.Background())
	defer c.EndSession(context.Background())

	audio := []byte{0xAA, 0xBB, 0xCC}
	err := c.SendAudio(context.Background(), audio)
	if err != nil {
		t.Fatalf("SendAudio: %v", err)
	}
}

// ---------------------------------------------------------------------------
// ReadResult
// ---------------------------------------------------------------------------

func TestASRClient_ReadResult_NoSession(t *testing.T) {
	c := NewASRClient("ws://localhost:9999")
	_, err := c.ReadResult(context.Background())
	if err == nil || !strings.Contains(err.Error(), "not started") {
		t.Fatalf("expected 'not started' error, got: %v", err)
	}
}

func TestASRClient_ReadResult_Success(t *testing.T) {
	srv := mockASRServer(func(conn *websocket.Conn) {
		conn.ReadMessage() // config
		result := ASRResult{Text: "hello", IsFinal: true, Mode: "2pass-offline"}
		conn.WriteJSON(result)
		time.Sleep(100 * time.Millisecond)
	})
	defer srv.Close()

	c := NewASRClient(wsURL(srv))
	c.StartSession(context.Background())
	defer c.EndSession(context.Background())

	r, err := c.ReadResult(context.Background())
	if err != nil {
		t.Fatalf("ReadResult: %v", err)
	}
	if r.Text != "hello" || !r.IsFinal {
		t.Errorf("result = %+v", r)
	}
}

func TestASRClient_ReadResult_ContextCancel(t *testing.T) {
	srv := mockASRServer(func(conn *websocket.Conn) {
		conn.ReadMessage() // config
		time.Sleep(5 * time.Second) // block indefinitely
	})
	defer srv.Close()

	c := NewASRClient(wsURL(srv))
	c.StartSession(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := c.ReadResult(ctx)
	if err == nil {
		t.Fatal("expected context error")
	}
}

// ---------------------------------------------------------------------------
// EndSession
// ---------------------------------------------------------------------------

func TestASRClient_EndSession_NilConn(t *testing.T) {
	c := NewASRClient("ws://localhost:9999")
	c.EndSession(context.Background()) // should not panic
}

func TestASRClient_EndSession_SendsIsSpeakingFalse(t *testing.T) {
	var gotMsg map[string]any
	var mu sync.Mutex
	done := make(chan struct{})
	srv := mockASRServer(func(conn *websocket.Conn) {
		conn.ReadMessage() // config
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return
		}
		mu.Lock()
		json.Unmarshal(msg, &gotMsg)
		mu.Unlock()
		close(done)
	})
	defer srv.Close()

	c := NewASRClient(wsURL(srv))
	c.StartSession(context.Background())
	c.EndSession(context.Background())

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for end-session message")
	}

	mu.Lock()
	defer mu.Unlock()
	if gotMsg == nil {
		t.Fatal("expected is_speaking message")
	}
	if speaking, ok := gotMsg["is_speaking"].(bool); !ok || speaking {
		t.Errorf("is_speaking = %v", gotMsg["is_speaking"])
	}
}

// ---------------------------------------------------------------------------
// RecognizeStream (integration)
// ---------------------------------------------------------------------------

func TestASRClient_RecognizeStream_Success(t *testing.T) {
	srv := mockASRServer(func(conn *websocket.Conn) {
		conn.ReadMessage() // config

		// Immediately send a streaming result before audio arrives
		result := ASRResult{Text: "recognized", IsFinal: false, Mode: "streaming"}
		conn.WriteJSON(result)

		// Read until connection closes
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				break
			}
		}
	})
	defer srv.Close()

	c := NewASRClient(wsURL(srv))
	audioCh := make(chan []byte, 2)

	resultCh, err := c.RecognizeStream(context.Background(), audioCh, 10)
	if err != nil {
		t.Fatalf("RecognizeStream: %v", err)
	}

	// Wait for the first result before closing audio
	select {
	case r := <-resultCh:
		if r.Text != "recognized" {
			t.Errorf("text = %q", r.Text)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for result")
	}

	close(audioCh)
	// Drain remaining
	for range resultCh {
	}
}

func TestASRClient_RecognizeStream_ContextCancel(t *testing.T) {
	srv := mockASRServer(func(conn *websocket.Conn) {
		conn.ReadMessage() // config
		time.Sleep(5 * time.Second)
	})
	defer srv.Close()

	c := NewASRClient(wsURL(srv))
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	audioCh := make(chan []byte, 10)
	resultCh, err := c.RecognizeStream(ctx, audioCh, 10)
	if err != nil {
		t.Fatalf("RecognizeStream: %v", err)
	}

	for range resultCh {
		// drain
	}
	// should complete without hanging
}

func TestASRClient_RecognizeStream_EmptyResults(t *testing.T) {
	srv := mockASRServer(func(conn *websocket.Conn) {
		conn.ReadMessage() // config
		// Send empty-text result (should be skipped) then valid result
		conn.WriteJSON(ASRResult{Text: "", IsFinal: false})
		conn.WriteJSON(ASRResult{Text: "valid", IsFinal: true})
		// Keep connection open for reader to finish
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				break
			}
		}
	})
	defer srv.Close()

	c := NewASRClient(wsURL(srv))
	audioCh := make(chan []byte, 1)

	resultCh, err := c.RecognizeStream(context.Background(), audioCh, 10)
	if err != nil {
		t.Fatalf("RecognizeStream: %v", err)
	}

	// Wait for the valid result
	select {
	case r := <-resultCh:
		if r.Text != "valid" {
			t.Errorf("text = %q, want 'valid'", r.Text)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for result")
	}

	close(audioCh)
	for range resultCh {
	}
}
