// Package parser 提供向量化（Embedding）能力。
// 对接外部 Embedding HTTP 服务（BAAI/bge-m3 等）。
package parser

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Embedder 向量化接口
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// HTTPEmbedder 调用外部 HTTP Embedding 服务
// 接口约定：POST /embed  {"texts":[...]}  →  {"embeddings":[[...], ...]}
type HTTPEmbedder struct {
	baseURL string
	client  *http.Client
}

type embedRequest struct {
	Texts []string `json:"texts"`
}

type embedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
}

// NewHTTPEmbedder 创建 HTTP Embedding 客户端
func NewHTTPEmbedder(baseURL string) *HTTPEmbedder {
	return &HTTPEmbedder{
		baseURL: baseURL,
		client:  &http.Client{Timeout: 60 * time.Second},
	}
}

// Embed 批量向量化文本，返回 [][]float32
func (e *HTTPEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	body, err := json.Marshal(embedRequest{Texts: texts})
	if err != nil {
		return nil, fmt.Errorf("embed marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.baseURL+"/embed", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("embed new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embed http do: %w", err)
	}
	defer resp.Body.Close()
	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("embed read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embed service error %d: %s", resp.StatusCode, string(respBytes))
	}
	var er embedResponse
	if err := json.Unmarshal(respBytes, &er); err != nil {
		return nil, fmt.Errorf("embed unmarshal: %w", err)
	}
	return er.Embeddings, nil
}

// MockEmbedder 开发/测试用 mock，返回随机小值向量（维度 1024）
// 注意：不使用全零向量，避免 RediSearch Cosine 距离计算时除以零导致未定义行为
type MockEmbedder struct{}

func (m *MockEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	result := make([][]float32, len(texts))
	for i := range result {
		vec := make([]float32, 1024)
		// 用固定非零值：每个维度 0.001 * (i+1)，保证模不为零且可重复
		for j := range vec {
			vec[j] = 0.001
		}
		result[i] = vec
	}
	return result, nil
}
