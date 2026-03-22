package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestNoCacheHandler_WasmFile_NoHeaders(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	handler := noCacheHandler(inner)

	req := httptest.NewRequest("GET", "/model.wasm", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if cc := rr.Header().Get("Cache-Control"); cc != "" {
		t.Error("should not set Cache-Control for .wasm file")
	}
}

func TestNoCacheHandler_JSFile_HasNoCacheHeaders(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	handler := noCacheHandler(inner)

	req := httptest.NewRequest("GET", "/app.js", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Header().Get("Cache-Control") == "" {
		t.Fatal("expected Cache-Control for js")
	}
	if rr.Header().Get("Pragma") != "no-cache" {
		t.Fatal("expected pragma no-cache")
	}
}

func TestNoCacheHandler_Root_HasNoCacheHeaders(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	handler := noCacheHandler(inner)

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Header().Get("Cache-Control") == "" {
		t.Fatal("expected Cache-Control for root")
	}
}

func TestAdaptiveController_Save_ActualFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sizes.json")

	sizes := DefaultChannelSizes()
	ac := NewAdaptiveController(sizes)
	ac.Save(path)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Read file: %v", err)
	}
	if len(data) == 0 {
		t.Error("file should not be empty")
	}
}
