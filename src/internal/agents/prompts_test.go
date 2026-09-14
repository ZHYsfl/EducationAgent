package agents

import (
	"strings"
	"testing"
)

func TestChineseSection(t *testing.T) {
	for name, fn := range map[string]func() string{
		"phase1": VoicePhase1Prompt,
		"phase2": VoicePhase2Prompt,
		"ppt":    PPTPrompt,
	} {
		p := fn()
		if p == "" || strings.Contains(p, "## English") || strings.Contains(p, "## 中文版") {
			end := len(p)
			if end > 60 {
				end = 60
			}
			t.Fatalf("%s prompt not extracted cleanly: %q", name, p[:end])
		}
	}
	if !strings.Contains(VoicePhase1Prompt(), "需求收集阶段") {
		t.Fatal("phase1 prompt wrong")
	}
	if !strings.Contains(VoicePhase2Prompt(), "沟通桥梁") {
		t.Fatal("phase2 prompt wrong")
	}
	if !strings.Contains(PPTPrompt(), "Slidev") {
		t.Fatal("ppt prompt wrong")
	}
}
