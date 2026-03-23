package doubao

import (
	"testing"
)

func TestGzipRoundtrip(t *testing.T) {
	original := []byte("hello world, this is a test of compression")
	compressed, err := gzipCompress(original)
	if err != nil {
		t.Fatal(err)
	}
	if len(compressed) == 0 {
		t.Error("compressed should not be empty")
	}

	decompressed, err := gzipDecompress(compressed)
	if err != nil {
		t.Fatal(err)
	}
	if string(decompressed) != string(original) {
		t.Errorf("roundtrip failed: %q", string(decompressed))
	}
}

func TestGzipDecompress_Invalid(t *testing.T) {
	_, err := gzipDecompress([]byte("not gzip data"))
	if err == nil {
		t.Fatal("expected error for invalid gzip")
	}
}

func TestGzipCompress_Empty(t *testing.T) {
	compressed, err := gzipCompress(nil)
	if err != nil {
		t.Fatal(err)
	}
	decompressed, err := gzipDecompress(compressed)
	if err != nil {
		t.Fatal(err)
	}
	if len(decompressed) != 0 {
		t.Errorf("expected empty, got %d bytes", len(decompressed))
	}
}
