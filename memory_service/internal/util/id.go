package util

import "github.com/google/uuid"

func NewMemoryID() string {
	return "mem_" + uuid.NewString()
}
