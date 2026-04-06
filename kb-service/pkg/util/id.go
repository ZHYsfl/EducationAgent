package util

import (
	"fmt"

	"github.com/google/uuid"
)

// NewID 生成带前缀的 UUID v4，例如 NewID("doc_") → "doc_a1b2c3d4-..."
func NewID(prefix string) string {
	return fmt.Sprintf("%s%s", prefix, uuid.New().String())
}
