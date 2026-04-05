package oss

import "fmt"

// NewTencentCOS is a placeholder to keep compatibility with existing config.
// TODO: implement Tencent COS provider when needed.
func NewTencentCOS(cfg Config) (Storage, error) {
	return nil, fmt.Errorf("tencent COS provider is not implemented")
}
