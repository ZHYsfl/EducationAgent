package oss

import (
	"bytes"
	"context"
	"fmt"
	"time"

	sharedoss "educationagent/oss"
)

type Client struct {
	bucket  string
	storage sharedoss.Storage
}

func NewClient(cfg sharedoss.Config) (*Client, error) {
	st, err := sharedoss.New(cfg)
	if err != nil {
		return nil, err
	}
	bucket := cfg.Bucket
	if bucket == "" {
		bucket = "exports"
	}
	return &Client{bucket: bucket, storage: st}, nil
}

func (c *Client) PutObject(ctx context.Context, key string, content []byte) (string, int64, error) {
	objectKey := c.bucket + "/" + key
	if err := c.storage.Upload(ctx, objectKey, bytes.NewReader(content), int64(len(content))); err != nil {
		return "", 0, err
	}
	url, err := c.storage.GenerateSignedURL(objectKey, 24*time.Hour)
	if err != nil {
		return "", 0, err
	}
	return url, int64(len(content)), nil
}

func (c *Client) Exists(ctx context.Context, key string) (bool, error) {
	return c.storage.Exists(ctx, c.bucket+"/"+key)
}

func (c *Client) ObjectKey(key string) string {
	return fmt.Sprintf("%s/%s", c.bucket, key)
}
