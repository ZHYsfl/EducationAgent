package asr

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTranscribeExtractsTag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audio/transcriptions" {
			t.Errorf("path = %s", r.URL.Path)
		}
		mr, err := multipart.NewReader(r.Body, boundaryFrom(r.Header.Get("Content-Type"))).ReadForm(1 << 20)
		if err != nil {
			t.Errorf("read form: %v", err)
			return
		}
		files := mr.File["file"]
		if len(files) != 1 {
			t.Errorf("form file missing: %v", mr.File)
			return
		}
		fh, err := files[0].Open()
		if err != nil {
			t.Errorf("open form file: %v", err)
			return
		}
		defer fh.Close()
		wav, _ := io.ReadAll(fh)
		if len(wav) < 44 || string(wav[:4]) != "RIFF" || string(wav[8:12]) != "WAVE" {
			t.Errorf("bad wav header: %q", wav[:12])
		}
		if rate := binary.LittleEndian.Uint32(wav[24:]); rate != 16000 {
			t.Errorf("sample rate = %d, want 16000", rate)
		}
		if model := mr.Value["model"]; len(model) != 1 || model[0] != "test-model" {
			t.Errorf("model field = %v", model)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"text":"language Chinese<asr_text>大家好，我是你的课件助手。</asr_text>","usage":{}}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-model", nil)
	words, err := c.Transcribe(context.Background(), bytes.Repeat([]byte{0x01, 0x02}, 16000))
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if words != "大家好，我是你的课件助手。" {
		t.Fatalf("words = %q", words)
	}
}

func TestTranscribeNoTagFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"text":"没有标签的原文"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "m", nil)
	words, err := c.Transcribe(context.Background(), []byte{0, 0})
	if err != nil {
		t.Fatalf("Transcribe: %v", err)
	}
	if words != "没有标签的原文" {
		t.Fatalf("words = %q", words)
	}
}

func TestTranscribeServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("gpu on fire"))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "m", nil)
	if _, err := c.Transcribe(context.Background(), []byte{0, 0}); err == nil {
		t.Fatal("expected error")
	}
}

func boundaryFrom(contentType string) string {
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return ""
	}
	return params["boundary"]
}
