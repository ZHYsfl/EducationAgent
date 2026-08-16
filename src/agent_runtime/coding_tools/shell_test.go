package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExecuteCommandSuccess(t *testing.T) {
	out, err := ExecuteCommand(context.Background(), "echo hello", "")
	if err != nil {
		t.Fatalf("ExecuteCommand failed: %v", err)
	}
	if strings.TrimSpace(out) != "stdout:hello" {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestExecuteCommandStderrOnly(t *testing.T) {
	out, err := ExecuteCommand(context.Background(), "echo err >&2", "")
	if err != nil {
		t.Fatalf("ExecuteCommand failed: %v", err)
	}
	if strings.TrimSpace(out) != "stderr:err" {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestExecuteCommandFailure(t *testing.T) {
	out, err := ExecuteCommand(context.Background(), "printf 'out'; echo err >&2; false", "")
	if err == nil {
		t.Fatal("expected error for failing command")
	}
	expected := "stdout:out\nstderr:err"
	if strings.TrimSpace(out) != expected {
		t.Fatalf("expected output %q, got %q", expected, out)
	}
}

func TestExecuteCommandWorkdir(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "test.txt"), []byte("inside"), 0644); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	out, err := ExecuteCommand(context.Background(), "cat test.txt", tmp)
	if err != nil {
		t.Fatalf("ExecuteCommand failed: %v", err)
	}
	if strings.TrimSpace(out) != "stdout:inside" {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestExecuteCommandInvalidCommand(t *testing.T) {
	out, err := ExecuteCommand(context.Background(), "not_a_real_command_12345", "")
	if err == nil {
		t.Fatal("expected error for invalid command")
	}
	if !strings.Contains(out, "stderr:") {
		t.Fatalf("expected stderr in output, got %q", out)
	}
}
