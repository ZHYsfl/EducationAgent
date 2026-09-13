package engineserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"educationagent/internal/protocol"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// fakeTTSUpstream streams pcm chunks with the same headers as the real wrapper.
func fakeTTSUpstream(t *testing.T, status int, chunks ...[]byte) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status != http.StatusOK {
			w.WriteHeader(status)
			fmt.Fprint(w, "backend exploded")
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("X-Sample-Rate", "22050")
		w.Header().Set("X-Channels", "1")
		flusher, _ := w.(http.Flusher)
		for _, c := range chunks {
			if _, err := w.Write(c); err != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
			time.Sleep(5 * time.Millisecond) // keep chunks as separate reads
		}
	}))
}

type ttsSession struct {
	header protocol.Envelope
	bins   [][]byte
	fin    protocol.Envelope
}

// readTTSSession reads one full 2.1 response sequence off the connection.
func readTTSSession(t *testing.T, ctx context.Context, conn *websocket.Conn) ttsSession {
	t.Helper()
	var s ttsSession
	gotHeader, gotFin := false, false
	for !gotHeader || !gotFin {
		dt, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("read frame: %v", err)
		}
		switch dt {
		case websocket.MessageBinary:
			if !gotHeader {
				t.Fatal("binary frame before audio_header")
			}
			s.bins = append(s.bins, data)
		case websocket.MessageText:
			var env protocol.Envelope
			if err := json.Unmarshal(data, &env); err != nil {
				t.Fatalf("text frame: %v", err)
			}
			switch env.Type {
			case protocol.TypeTTSInferResponseAudio:
				if gotHeader {
					t.Fatal("duplicate audio_header")
				}
				gotHeader = true
				s.header = env
			case protocol.TypeTTSInferResponseFinished:
				if !gotHeader {
					t.Fatal("finished before audio_header")
				}
				gotFin = true
				s.fin = env
			default:
				t.Fatalf("unexpected frame type %q", env.Type)
			}
		}
	}
	return s
}

func dialTTSServer(t *testing.T, ctx context.Context, httpURL string) *websocket.Conn {
	t.Helper()
	wsURL := "ws://" + strings.TrimPrefix(httpURL, "http://") + TTSPath
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	return conn
}

func sendInferRequest(t *testing.T, ctx context.Context, conn *websocket.Conn, id, text string) {
	t.Helper()
	req := protocol.NewEnvelope(protocol.TypeTTSInferRequest, id, protocol.TTSInferRequestData{
		InputToken: text,
		State:      "inferring_tts_tokens",
		Format:     "pcm16",
		SampleRate: 22050,
	})
	if err := wsjson.Write(ctx, conn, req); err != nil {
		t.Fatalf("write infer_request: %v", err)
	}
}

func TestTTSWSFrameSequence(t *testing.T) {
	chunk1 := bytes.Repeat([]byte{0x11}, 2000)
	chunk2 := bytes.Repeat([]byte{0x22}, 3122)
	fake := fakeTTSUpstream(t, http.StatusOK, chunk1, chunk2)
	defer fake.Close()

	ctx := context.Background()
	srv := httptest.NewServer(http.HandlerFunc(NewServer(fake.URL).HandleTTSInfer))
	defer srv.Close()

	conn := dialTTSServer(t, ctx, srv.URL)
	defer conn.Close(websocket.StatusNormalClosure, "")

	sendInferRequest(t, ctx, conn, "req-1", "第一句话。")
	s := readTTSSession(t, ctx, conn)

	if s.header.ID != "req-1" {
		t.Errorf("header id = %q, want req-1", s.header.ID)
	}
	headerData, err := json.Marshal(s.header.Data)
	if err != nil {
		t.Fatal(err)
	}
	var audioHeader protocol.TTSInferResponseAudioHeaderData
	if err := json.Unmarshal(headerData, &audioHeader); err != nil {
		t.Fatal(err)
	}
	if audioHeader.Format != "pcm16" || audioHeader.SampleRate != 22050 || audioHeader.Channels != 1 {
		t.Errorf("audio_header data = %+v", audioHeader)
	}

	var total int
	for _, b := range s.bins {
		total += len(b)
	}
	if total != len(chunk1)+len(chunk2) {
		t.Errorf("pcm total = %d, want %d", total, len(chunk1)+len(chunk2))
	}
	if len(s.bins) < 2 {
		t.Errorf("got %d binary frames, want >= 2", len(s.bins))
	}
	if s.fin.Code != 200 {
		t.Errorf("finished code = %d, want 200", s.fin.Code)
	}
	if s.fin.Type != protocol.TypeTTSInferResponseFinished {
		t.Errorf("finished type = %q", s.fin.Type)
	}
}

func TestTTSWSSerialRequestsOneConnection(t *testing.T) {
	fake := fakeTTSUpstream(t, http.StatusOK, bytes.Repeat([]byte{0xAB}, 100))
	defer fake.Close()

	ctx := context.Background()
	srv := httptest.NewServer(http.HandlerFunc(NewServer(fake.URL).HandleTTSInfer))
	defer srv.Close()

	conn := dialTTSServer(t, ctx, srv.URL)
	defer conn.Close(websocket.StatusNormalClosure, "")

	for i, id := range []string{"req-a", "req-b"} {
		sendInferRequest(t, ctx, conn, id, fmt.Sprintf("第%d句。", i+1))
		s := readTTSSession(t, ctx, conn)
		if s.header.ID != id || s.fin.Code != 200 {
			t.Fatalf("request %s: header=%q code=%d", id, s.header.ID, s.fin.Code)
		}
	}
}

func TestTTSWSDownstreamError(t *testing.T) {
	fake := fakeTTSUpstream(t, http.StatusInternalServerError)
	defer fake.Close()

	ctx := context.Background()
	srv := httptest.NewServer(http.HandlerFunc(NewServer(fake.URL).HandleTTSInfer))
	defer srv.Close()

	conn := dialTTSServer(t, ctx, srv.URL)
	defer conn.Close(websocket.StatusNormalClosure, "")

	sendInferRequest(t, ctx, conn, "req-err", "这句话会失败。")

	// No audio_header: the error lands directly in a finished frame.
	dt, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if dt != websocket.MessageText {
		t.Fatalf("first frame is binary, want finished text frame")
	}
	var env protocol.Envelope
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatal(err)
	}
	if env.Type != protocol.TypeTTSInferResponseFinished {
		t.Fatalf("type = %q, want finished", env.Type)
	}
	if env.Code == 200 {
		t.Fatal("error response must not carry code 200")
	}
	dataMap, ok := env.Data.(map[string]any)
	if !ok || !strings.Contains(fmt.Sprint(dataMap["error"]), "backend exploded") {
		t.Fatalf("error text missing from finished frame: %+v", env.Data)
	}
}
