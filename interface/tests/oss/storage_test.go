package oss_test

import (
	"testing"

	"multimodal-teaching-agent/oss"
)

func TestNew_DefaultLocalProvider(t *testing.T) {
	t.Parallel()

	s, err := oss.New(oss.Config{LocalPath: t.TempDir(), BaseURL: "http://localhost:9500"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if s == nil {
		t.Fatal("expected storage instance, got nil")
	}
}

func TestNew_UnsupportedProvider(t *testing.T) {
	t.Parallel()

	s, err := oss.New(oss.Config{Provider: "invalid-provider"})
	if err == nil {
		t.Fatal("expected error for unsupported provider")
	}
	if s != nil {
		t.Fatal("expected nil storage on error")
	}
}

func TestNew_TencentStub(t *testing.T) {
	t.Parallel()

	s, err := oss.New(oss.Config{Provider: "tencent"})
	if err == nil {
		t.Fatal("expected error because tencent provider is stub")
	}
	if s != nil {
		t.Fatal("expected nil storage for tencent stub")
	}
}

func TestGenerateObjectKey_WithExtension(t *testing.T) {
	t.Parallel()

	got := oss.GenerateObjectKey(1, 2, 3, ".zip")
	want := "submissions/1/2/3.zip"
	if got != want {
		t.Fatalf("unexpected key, got=%s want=%s", got, want)
	}
}

func TestGenerateObjectKey_WithLanguageAndFallback(t *testing.T) {
	t.Parallel()

	gotPy := oss.GenerateObjectKey(10, 20, 30, "python")
	if gotPy != "submissions/10/20/30.py" {
		t.Fatalf("unexpected python key: %s", gotPy)
	}

	gotUnknown := oss.GenerateObjectKey(10, 20, 30, "brainfuck")
	if gotUnknown != "submissions/10/20/30.code" {
		t.Fatalf("unexpected fallback key: %s", gotUnknown)
	}
}
