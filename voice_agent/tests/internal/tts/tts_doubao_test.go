package tts_test

import (
	"context"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"voiceagent/internal/doubao"
	ttspkg "voiceagent/internal/tts"
)

func TestNewDouBaoTTSClient(t *testing.T) {
	cfg := ttspkg.DouBaoTTSConfig{AppId: "id", Token: "tok", Cluster: "c", VoiceType: "v"}
	c := ttspkg.NewDouBaoTTSClient(cfg)
	if c.Config().AppId != "id" {
		t.Fatal("config not set")
	}
}

func TestDouBaoTTS_MultipleAudioChunks(t *testing.T) {
	srv := mockDouBaoTTSServer(func(conn *websocket.Conn) {
		conn.ReadMessage() // request frame
		sendTTSAudioFrame(conn, []byte{0x01, 0x02}, false)
		sendTTSAudioFrame(conn, []byte{0x03, 0x04}, false)
		sendTTSAudioFrame(conn, []byte{0x05}, true) // last
	})
	defer srv.Close()

	wsConn := ttsDial(t, srv)
	defer wsConn.Close()

	ttsSendConfigFrame(t, wsConn)

	var audioChunks [][]byte
	for i := 0; i < 10; i++ {
		_, data, err := wsConn.ReadMessage()
		if err != nil {
			break
		}
		ph, _ := doubao.ParseHeader(data)
		if ph.MsgType == doubao.MsgTypeAudioOnlyResp {
			audio, isLast, _ := doubao.ParseAudioResponse(data)
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

	wsConn := ttsDial(t, srv)
	defer wsConn.Close()
	ttsSendConfigFrame(t, wsConn)

	_, data, err := wsConn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	ph, _ := doubao.ParseHeader(data)
	if ph.MsgType != doubao.MsgTypeError {
		t.Fatalf("expected error, got 0x%X", ph.MsgType)
	}
	code, msg, _ := doubao.ParseErrorResponse(data)
	if code != 20001 || msg != "quota exceeded" {
		t.Errorf("code=%d msg=%q", code, msg)
	}
}

func TestDouBaoTTS_ServerAckIgnored(t *testing.T) {
	srv := mockDouBaoTTSServer(func(conn *websocket.Conn) {
		conn.ReadMessage()
		sendTTSServerAckFrame(conn)                 // should be skipped
		sendTTSAudioFrame(conn, []byte{0xAA}, true) // last
	})
	defer srv.Close()

	wsConn := ttsDial(t, srv)
	defer wsConn.Close()
	ttsSendConfigFrame(t, wsConn)

	var gotAudio bool
	for i := 0; i < 5; i++ {
		_, data, err := wsConn.ReadMessage()
		if err != nil {
			break
		}
		ph, _ := doubao.ParseHeader(data)
		if ph.MsgType == doubao.MsgTypeAudioOnlyResp {
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
		sendTTSAudioFrame(conn, nil, false)         // empty audio
		sendTTSAudioFrame(conn, []byte{0xBB}, true) // last with data
	})
	defer srv.Close()

	wsConn := ttsDial(t, srv)
	defer wsConn.Close()
	ttsSendConfigFrame(t, wsConn)

	var nonEmpty int
	for i := 0; i < 5; i++ {
		_, data, err := wsConn.ReadMessage()
		if err != nil {
			break
		}
		ph, _ := doubao.ParseHeader(data)
		if ph.MsgType == doubao.MsgTypeAudioOnlyResp {
			audio, isLast, _ := doubao.ParseAudioResponse(data)
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

	wsConn := ttsDial(t, srv)
	defer wsConn.Close()
	ttsSendConfigFrame(t, wsConn)

	_, _, err := wsConn.ReadMessage()
	if err == nil {
		t.Error("expected error from closed connection")
	}
}

func TestDouBaoTTS_UnexpectedMsgType(t *testing.T) {
	srv := mockDouBaoTTSServer(func(conn *websocket.Conn) {
		conn.ReadMessage()
		h := doubao.BuildHeader(0x0C, doubao.FlagNoSeq, doubao.SerNone, doubao.CompNone)
		frame := doubao.BuildFrame(h, []byte{0x01})
		conn.WriteMessage(websocket.BinaryMessage, frame)
		sendTTSAudioFrame(conn, []byte{0xCC}, true)
	})
	defer srv.Close()

	wsConn := ttsDial(t, srv)
	defer wsConn.Close()
	ttsSendConfigFrame(t, wsConn)

	var gotAudio bool
	for i := 0; i < 5; i++ {
		_, data, err := wsConn.ReadMessage()
		if err != nil {
			break
		}
		ph, _ := doubao.ParseHeader(data)
		if ph.MsgType == doubao.MsgTypeAudioOnlyResp {
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

	wsConn := ttsDial(t, srv)
	defer wsConn.Close()
	ttsSendConfigFrame(t, wsConn)

	<-ctx.Done()
	wsConn.Close()
}
