package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// getDefaultBaseURL 返回 kb-service 的默认地址，优先从环境变量读取
func getDefaultBaseURL() string {
	if url := os.Getenv("KB_SERVICE_URL"); url != "" {
		return url
	}
	return "http://localhost:9200"
}

// Chunk 知识库检索结果块（符合 api.md 规范）
type Chunk struct {
	ChunkID string `json:"chunk_id"`
	Content string `json:"content"`
}

// QueryChunksResponse kb-service 查询响应
type QueryChunksResponse struct {
	Code    int       `json:"code"`
	Message string    `json:"message"`
	Data    ChunkData `json:"data"`
}

// ChunkData 响应中的 chunks 数据
type ChunkData struct {
	Chunks []ChunkInfo `json:"chunks"`
	Total  int         `json:"total"`
}

// ChunkInfo 从 kb-service 返回的 chunk 信息
type ChunkInfo struct {
	ChunkID string `json:"chunk_id"`
	Content string `json:"content"`
}

// KB_BASE_URL kb-service 服务地址（保留用于文档和向后兼容）
const KB_BASE_URL = "" // 已废弃，请使用环境变量 KB_SERVICE_URL 或 KBServiceConfig

// KBService defines the knowledge-base query interface.
type KBService interface {
	QueryChunks(ctx context.Context, query string) ([]Chunk, int, error)
}

// DefaultKBService is the actual implementation that calls kb-service API.
type DefaultKBService struct {
	client  *http.Client
	baseURL string
}

// KBServiceConfig kb-service 连接配置
type KBServiceConfig struct {
	BaseURL string // kb-service 地址，如 http://localhost:9200
}

// NewKBService creates a new KB service with default config.
func NewKBService() KBService {
	return &DefaultKBService{
		client:  &http.Client{Timeout: 30 * time.Second},
		baseURL: getDefaultBaseURL(),
	}
}

// NewKBServiceWithConfig creates a new KB service with custom config.
func NewKBServiceWithConfig(cfg KBServiceConfig) KBService {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = getDefaultBaseURL()
	}
	return &DefaultKBService{
		client:  &http.Client{Timeout: 30 * time.Second},
		baseURL: baseURL,
	}
}

// QueryChunks queries knowledge base chunks using keyword search.
// It calls kb-service's POST /api/v1/kb/query-chunks endpoint.
// Returns chunks, total count, and error if any.
func (s *DefaultKBService) QueryChunks(ctx context.Context, query string) ([]Chunk, int, error) {
	// 构建请求体（符合 api.md 规范）
	reqBody := map[string]interface{}{
		"from":  "ppt_agent",
		"to":    "kb_service",
		"query": query, // api.md 规范：query 字段（单字符串）
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, 0, fmt.Errorf("请求体序列化失败: %w", err)
	}

	// 发送 HTTP POST 请求
	url := s.baseURL + "/api/v1/kb/query-chunks"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonData))
	if err != nil {
		return nil, 0, fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("调用 kb-service 失败: %w", err)
	}
	defer resp.Body.Close()

	// 读取响应体
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("读取响应失败: %w", err)
	}

	// 解析响应
	var response QueryChunksResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, 0, fmt.Errorf("解析响应失败: %w", err)
	}

	if response.Code != 200 {
		return nil, 0, fmt.Errorf("kb-service 返回错误: code=%d, message=%s", response.Code, response.Message)
	}

	// 转换 ChunkInfo 为 Chunk
	chunks := make([]Chunk, 0, len(response.Data.Chunks))
	for _, c := range response.Data.Chunks {
		chunks = append(chunks, Chunk{
			ChunkID: c.ChunkID,
			Content: c.Content,
		})
	}

	return chunks, response.Data.Total, nil
}
