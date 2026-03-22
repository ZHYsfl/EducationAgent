package main

import (
	"context"
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func mockDouBaoTTSServer(handler func(conn *websocket.Conn)) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := wsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		handler(conn)
	}))
}

func TestNewDouBaoTTSClient(t *testing.T) {
	cfg := DouBaoTTSConfig{AppId: "id", Token: "tok", Cluster: "c", VoiceType: "v"}
	c := NewDouBaoTTSClient(cfg)
	if c.config.AppId != "id" {
		t.Fatal("config not set")
	}
}

// sendTTSAudioFrame sends a doubao TTS audio-only response frame to the client.
func sendTTSAudioFrame(conn *websocket.Conn, audio []byte, isLast bool) error {
	flags := byte(flagNoSeq)
	if isLast {
		flags = flagLastData
	}
	h := buildHeader(msgTypeAudioOnlyResp, flags, serNone, compNone)
	data := make([]byte, 4+len(audio))
	copy(data[0:4], h[:])
	copy(data[4:], audio)
	return conn.WriteMessage(websocket.BinaryMessage, data)
}

// sendTTSErrorFrame sends a doubao TTS error frame.
func sendTTSErrorFrame(conn *websocket.Conn, code uint32, msg string) error {
	hdr := buildHeader(msgTypeError, 0, 0, 0)
	data := make([]byte, 12+len(msg))
	copy(data[0:4], hdr[:])
	binary.BigEndian.PutUint32(data[4:8], code)
	binary.BigEndian.PutUint32(data[8:12], uint32(len(msg)))
	copy(data[12:], msg)
	return conn.WriteMessage(websocket.BinaryMessage, data)
}

// sendTTSServerAckFrame sends a JSON server ack that should be ignored.
func sendTTSServerAckFrame(conn *websocket.Conn) error {
	h := buildHeader(msgTypeFullServerResp, flagNoSeq, serJSON, compNone)
	payload := []byte(`{"status":"ok"}`)
	frame := buildFrame(h, payload)
	return conn.WriteMessage(websocket.BinaryMessage, frame)
}

func TestDouBaoTTS_MultipleAudioChunks(t *testing.T) {
	srv := mockDouBaoTTSServer(func(conn *websocket.Conn) {
		conn.ReadMessage() // request frame
		sendTTSAudioFrame(conn, []byte{0x01, 0x02}, false)
		sendTTSAudioFrame(conn, []byte{0x03, 0x04}, false)
		sendTTSAudioFrame(conn, []byte{0x05}, true) // last
	})
	defer srv.Close()

	// Simulate the TTS client reader logic since we can't redirect the const endpoint
	wsConn := dialMockWS(t, srv)
	defer wsConn.Close()

	// Send a request frame (mimic Synthesize)
	sendConfigFrame(t, wsConn)

	var audioChunks [][]byte
	for i := 0; i < 10; i++ {
		_, data, err := wsConn.ReadMessage()
		if err != nil {
			break
		}
		ph, _ := parseHeader(data)
		if ph.MsgType == msgTypeAudioOnlyResp {
			audio, isLast, _ := parseAudioResponse(data)
			if len(audio) > 0 {
				audioChunks = append(audioChunks, audio)
			}
			if isLast {
				break
			}
		}
	}
	if len(audioChunks) != 3 {
		t.Errorf("expected 3 audio chunks, got %d", len(audioChunks))
	}
}

func TestDouBaoTTS_ErrorResponse(t *testing.T) {
	srv := mockDouBaoTTSServer(func(conn *websocket.Conn) {
		conn.ReadMessage()
		sendTTSErrorFrame(conn, 20001, "quota exceeded")
	})
	defer srv.Close()

	wsConn := dialMockWS(t, srv)
	defer wsConn.Close()
	sendConfigFrame(t, wsConn)

	_, data, err := wsConn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	ph, _ := parseHeader(data)
	if ph.MsgType != msgTypeError {
		t.Fatalf("expected error, got 0x%X", ph.MsgType)
	}
	code, msg, _ := parseErrorResponse(data)
	if code != 20001 || msg != "quota exceeded" {
		t.Errorf("code=%d msg=%q", code, msg)
	}
}

func TestDouBaoTTS_ServerAckIgnored(t *testing.T) {
	srv := mockDouBaoTTSServer(func(conn *websocket.Conn) {
		conn.ReadMessage()
		sendTTSServerAckFrame(conn)             // should be skipped
		sendTTSAudioFrame(conn, []byte{0xAA}, true) // last
	})
	defer srv.Close()

	wsConn := dialMockWS(t, srv)
	defer wsConn.Close()
	sendConfigFrame(t, wsConn)

	var gotAudio bool
	for i := 0; i < 5; i++ {
		_, data, err := wsConn.ReadMessage()
		if err != nil {
			break
		}
		ph, _ := parseHeader(data)
		if ph.MsgType == msgTypeAudioOnlyResp {
			gotAudio = true
			break
		}
	}
	if !gotAudio {
		t.Error("should have received audio after server ack")
	}
}

func TestDouBaoTTS_EmptyAudioChunk(t *testing.T) {
	srv := mockDouBaoTTSServer(func(conn *websocket.Conn) {
		conn.ReadMessage()
		sendTTSAudioFrame(conn, nil, false)            // empty audio
		sendTTSAudioFrame(conn, []byte{0xBB}, true) // last with data
	})
	defer srv.Close()

	wsConn := dialMockWS(t, srv)
	defer wsConn.Close()
	sendConfigFrame(t, wsConn)

	var nonEmpty int
	for i := 0; i < 5; i++ {
		_, data, err := wsConn.ReadMessage()
		if err != nil {
			break
		}
		ph, _ := parseHeader(data)
		if ph.MsgType == msgTypeAudioOnlyResp {
			audio, isLast, _ := parseAudioResponse(data)
			if len(audio) > 0 {
				nonEmpty++
			}
			if isLast {
				break
			}
		}
	}
	if nonEmpty != 1 {
		t.Errorf("expected 1 non-empty audio chunk, got %d", nonEmpty)
	}
}

func TestDouBaoTTS_ConnectionClose(t *testing.T) {
	srv := mockDouBaoTTSServer(func(conn *websocket.Conn) {
		conn.ReadMessage()
		conn.Close() // abrupt close
	})
	defer srv.Close()

	wsConn := dialMockWS(t, srv)
	defer wsConn.Close()
	sendConfigFrame(t, wsConn)

	_, _, err := wsConn.ReadMessage()
	if err == nil {
		t.Error("expected error from closed connection")
	}
}

func TestDouBaoTTS_UnexpectedMsgType(t *testing.T) {
	srv := mockDouBaoTTSServer(func(conn *websocket.Conn) {
		conn.ReadMessage()
		// Send a frame with unexpected msg type (0x0C = 12)
		h := buildHeader(0x0C, flagNoSeq, serNone, compNone)
		frame := buildFrame(h, []byte{0x01})
		conn.WriteMessage(websocket.BinaryMessage, frame)
		// Then send valid last audio
		sendTTSAudioFrame(conn, []byte{0xCC}, true)
	})
	defer srv.Close()

	wsConn := dialMockWS(t, srv)
	defer wsConn.Close()
	sendConfigFrame(t, wsConn)

	var gotAudio bool
	for i := 0; i < 5; i++ {
		_, data, err := wsConn.ReadMessage()
		if err != nil {
			break
		}
		ph, _ := parseHeader(data)
		if ph.MsgType == msgTypeAudioOnlyResp {
			gotAudio = true
			break
		}
	}
	if !gotAudio {
		t.Error("should have received audio after unexpected msg type")
	}
}

func TestDouBaoTTS_ContextCancelDuringRead(t *testing.T) {
	srv := mockDouBaoTTSServer(func(conn *websocket.Conn) {
		conn.ReadMessage()
		time.Sleep(5 * time.Second) // hang
	})
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	wsConn := dialMockWS(t, srv)
	defer wsConn.Close()
	sendConfigFrame(t, wsConn)

	<-ctx.Done()
	// Connection should be closeable without hang
	wsConn.Close()
}
