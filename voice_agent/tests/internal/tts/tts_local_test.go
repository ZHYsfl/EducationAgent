package tts_test

import (
	"context"
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	ttspkg "voiceagent/internal/tts"
)

func TestAddWAVHeader_Basic(t *testing.T) {
	pcm := make([]byte, 100)
	for i := range pcm {
		pcm[i] = byte(i)
	}

	wav := ttspkg.AddWAVHeader(pcm, 16000, 16, 1)
	if len(wav) != 44+100 {
		t.Fatalf("expected %d bytes, got %d", 44+100, len(wav))
	}

	if string(wav[0:4]) != "RIFF" {
		t.Error("missing RIFF header")
	}
	if string(wav[8:12]) != "WAVE" {
		t.Error("missing WAVE marker")
	}
	if string(wav[12:16]) != "fmt " {
		t.Error("missing fmt chunk")
	}
	if string(wav[36:40]) != "data" {
		t.Error("missing data chunk")
	}

	dataLen := binary.LittleEndian.Uint32(wav[40:44])
	if dataLen != 100 {
		t.Errorf("data length = %d, want 100", dataLen)
	}

	channels := binary.LittleEndian.Uint16(wav[22:24])
	if channels != 1 {
		t.Errorf("channels = %d", channels)
	}

	sampleRate := binary.LittleEndian.Uint32(wav[24:28])
	if sampleRate != 16000 {
		t.Errorf("sample rate = %d", sampleRate)
	}
}

func TestAddWAVHeader_Stereo(t *testing.T) {
	pcm := make([]byte, 200)
	wav := ttspkg.AddWAVHeader(pcm, 44100, 16, 2)

	channels := binary.LittleEndian.Uint16(wav[22:24])
	if channels != 2 {
		t.Errorf("channels = %d, want 2", channels)
	}

	sampleRate := binary.LittleEndian.Uint32(wav[24:28])
	if sampleRate != 44100 {
		t.Errorf("sample rate = %d, want 44100", sampleRate)
	}

	byteRate := binary.LittleEndian.Uint32(wav[28:32])
	expected := uint32(44100 * 2 * 16 / 8)
	if byteRate != expected {
		t.Errorf("byte rate = %d, want %d", byteRate, expected)
	}
}

func TestAddWAVHeader_EmptyPCM(t *testing.T) {
	wav := ttspkg.AddWAVHeader(nil, 16000, 16, 1)
	if len(wav) != 44 {
		t.Errorf("expected header only (44 bytes), got %d", len(wav))
	}
	dataLen := binary.LittleEndian.Uint32(wav[40:44])
	if dataLen != 0 {
		t.Errorf("data length = %d, want 0", dataLen)
	}
}

func TestNewTTSClient(t *testing.T) {
	c := ttspkg.NewTTSClient("http://localhost:50000")
	if c.BaseURL() != "http://localhost:50000" {
		t.Errorf("baseURL = %q", c.BaseURL())
	}
}

func TestTTSClient_Synthesize_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("method = %q", r.Method)
		}
		if r.URL.Path != "/inference_sft" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.WriteHeader(200)
		w.Write([]byte("fake audio chunk 1"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		w.Write([]byte("fake audio chunk 2"))
	}))
	defer srv.Close()

	c := ttspkg.NewTTSClient(srv.URL)
	ch, err := c.Synthesize(context.Background(), "测试文本", 10)
	if err != nil {
		t.Fatal(err)
	}

	var chunks [][]byte
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}
	if len(chunks) == 0 {
		t.Error("expected at least 1 chunk")
	}
}

func TestTTSClient_Synthesize_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte("error"))
	}))
	defer srv.Close()

	c := ttspkg.NewTTSClient(srv.URL)
	ch, err := c.Synthesize(context.Background(), "test", 10)
	if err != nil {
		t.Fatal(err)
	}

	var chunks [][]byte
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}
	if len(chunks) != 0 {
		t.Error("should produce no chunks on server error")
	}
}

func TestTTSClient_Synthesize_ContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	c := ttspkg.NewTTSClient(srv.URL)
	ch, err := c.Synthesize(ctx, "test", 10)
	if err != nil {
		t.Fatal(err)
	}

	cancel()

	done := make(chan struct{})
	go func() {
		for range ch {
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("channel should close after cancel")
	}
}

func TestTTSClient_Synthesize_ConnectionRefused(t *testing.T) {
	c := ttspkg.NewTTSClient("http://localhost:1")
	ch, err := c.Synthesize(context.Background(), "test", 10)
	if err != nil {
		return
	}
	for range ch {
	}
}
