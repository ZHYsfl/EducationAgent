package oss

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// S3Storage implements Storage interface using S3-compatible object storage (MinIO/AWS S3).
type S3Storage struct {
	client *minio.Client
	bucket string
}

func NewS3Storage(cfg Config) (*S3Storage, error) {
	endpoint := strings.TrimSpace(cfg.Endpoint)
	if endpoint == "" {
		return nil, fmt.Errorf("missing endpoint for s3/minio storage")
	}
	if strings.TrimSpace(cfg.Bucket) == "" {
		return nil, fmt.Errorf("missing bucket for s3/minio storage")
	}
	if strings.TrimSpace(cfg.SecretID) == "" || strings.TrimSpace(cfg.SecretKey) == "" {
		return nil, fmt.Errorf("missing credentials for s3/minio storage")
	}

	cl, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.SecretID, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
		Region: strings.TrimSpace(cfg.Region),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to init s3/minio client: %w", err)
	}
	return &S3Storage{client: cl, bucket: cfg.Bucket}, nil
}

func (s *S3Storage) Upload(ctx context.Context, objectKey string, reader io.Reader, size int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := s.client.PutObject(ctx, s.bucket, objectKey, reader, size, minio.PutObjectOptions{})
	if err != nil {
		return fmt.Errorf("put object failed: %w", err)
	}
	return nil
}

func (s *S3Storage) Download(ctx context.Context, objectKey string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	obj, err := s.client.GetObject(ctx, s.bucket, objectKey, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("get object failed: %w", err)
	}
	if _, statErr := obj.Stat(); statErr != nil {
		_ = obj.Close()
		return nil, fmt.Errorf("object stat failed: %w", statErr)
	}
	return obj, nil
}

func (s *S3Storage) Delete(ctx context.Context, objectKey string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	err := s.client.RemoveObject(ctx, s.bucket, objectKey, minio.RemoveObjectOptions{})
	if err != nil {
		return fmt.Errorf("remove object failed: %w", err)
	}
	return nil
}

func (s *S3Storage) GenerateSignedURL(objectKey string, expiry time.Duration) (string, error) {
	ctx := context.Background()
	u, err := s.client.PresignedGetObject(ctx, s.bucket, objectKey, expiry, nil)
	if err != nil {
		return "", fmt.Errorf("presign get object failed: %w", err)
	}
	return u.String(), nil
}

func (s *S3Storage) Exists(ctx context.Context, objectKey string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	_, err := s.client.StatObject(ctx, s.bucket, objectKey, minio.StatObjectOptions{})
	if err == nil {
		return true, nil
	}
	resp := minio.ToErrorResponse(err)
	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	return false, fmt.Errorf("stat object failed: %w", err)
}

func (s *S3Storage) ListUnderPrefix(ctx context.Context, prefix string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	opts := minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	}
	var keys []string
	for obj := range s.client.ListObjects(ctx, s.bucket, opts) {
		if obj.Err != nil {
			return nil, fmt.Errorf("list objects failed: %w", obj.Err)
		}
		keys = append(keys, obj.Key)
	}
	return keys, nil
}

