package agent_test

import (
	"os"
	"path/filepath"
	"testing"

	adaptivepkg "voiceagent/internal/adaptive"
)

func TestAdaptiveController_Save_ActualFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sizes.json")

	sizes := adaptivepkg.DefaultChannelSizes()
	ac := adaptivepkg.NewAdaptiveController(sizes)
	ac.Save(path)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Read file: %v", err)
	}
	if len(data) == 0 {
		t.Error("file should not be empty")
	}
}
