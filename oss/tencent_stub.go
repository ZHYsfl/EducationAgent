package oss

import "fmt"

// NewTencentCOS is a placeholder to keep builds working until
// Tencent COS implementation is added.
func NewTencentCOS(cfg Config) (Storage, error) {
	_ = cfg
	return nil, fmt.Errorf("tencent oss provider is not implemented yet")
}
