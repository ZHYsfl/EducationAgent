package pptx

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRoundTrip_DefaultTemplate 需设置 PPTAGENT_ROUNDTRIP_SRC 为某模板 source.pptx 绝对路径。
func TestRoundTrip_DefaultTemplate(t *testing.T) {
	src := os.Getenv("PPTAGENT_ROUNDTRIP_SRC")
	if src == "" {
		t.Skip("set PPTAGENT_ROUNDTRIP_SRC to .../templates/default/source.pptx")
	}
	dir := t.TempDir()
	dst := filepath.Join(dir, "out.pptx")
	if err := RoundTrip(src, dst); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(dst)
	if err != nil || st.Size() == 0 {
		t.Fatalf("output missing or empty: %v", err)
	}
}
