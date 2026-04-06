// Package reranker 提供文档重排序（Rerank）能力。
// 支持：Jina Reranker API（https://jina.ai/reranker/）
package reranker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"time"

	"kb-service/internal/model"
)

// Reranker 重排序接口
type Reranker interface {
	// Rerank 对候选文档块按相关性重排序
	// query: 用户查询
	// chunks: 候选文档块（来自向量检索）
	// 返回重排序后的文档块列表
	Rerank(ctx context.Context, query string, chunks []model.RetrievedChunk) ([]model.RetrievedChunk, error)
}

// JinaReranker 调用 Jina Reranker API 实现重排序
// API 文档: https://jina.ai/reranker/
type JinaReranker struct {
	baseURL  string // 如 https://reranker.jina.ai
	apiKey   string
	model    string // 模型名称，默认 jina-reranker-v2
	client   *http.Client
}

// NewJinaReranker 创建 Jina Reranker 客户端
// apiKey 从环境变量 JINA_API_KEY 获取
// baseURL 可选，默认 https://reranker.jina.ai
func NewJinaReranker(apiKey, baseURL, model string) (*JinaReranker, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("Jina API key is required")
	}
	if baseURL == "" {
		baseURL = "https://reranker.jina.ai"
	}
	if model == "" {
		model = "jina-reranker-v2-base"
	}
	return &JinaReranker{
		baseURL: baseURL,
		apiKey:  apiKey,
		model:   model,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}, nil
}

// jinaRequest Jina Reranker 请求体
type jinaRequest struct {
	Model     string   `json:"model"`
	Query     string   `json:"query"`
	TopK      int      `json:"top_k"`
	Documents []string `json:"documents"`
}

// jinaResponse Jina Reranker 响应体
type jinaResponse struct {
	Model string `json:"model"`
	Usage struct {
		TokensRerank int `json:"rerank_tokens"`
	} `json:"usage"`
	Results []jinaResult `json:"results"`
}

// jinaResult 单条重排序结果
type jinaResult struct {
	Index           int     `json:"index"`
	RelevanceScore float64 `json:"relevance_score"`
}

// Rerank 实现 Reranker 接口
func (r *JinaReranker) Rerank(ctx context.Context, query string, chunks []model.RetrievedChunk) ([]model.RetrievedChunk, error) {
	if len(chunks) == 0 {
		return chunks, nil
	}

	// 提取文档文本
	docs := make([]string, len(chunks))
	for i, c := range chunks {
		docs[i] = c.Content
	}

	reqBody := jinaRequest{
		Model:     r.model,
		Query:     query,
		TopK:      len(chunks),
		Documents: docs,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal rerank request: %w", err)
	}

	url := fmt.Sprintf("%s/rerank", r.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create rerank request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+r.apiKey)

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("rerank http do: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("rerank status %d: %s", resp.StatusCode, b)
	}

	var jresp jinaResponse
	if err := json.NewDecoder(resp.Body).Decode(&jresp); err != nil {
		return nil, fmt.Errorf("decode rerank response: %w", err)
	}

	// 构建 index → score 映射
	scoreMap := make(map[int]float64)
	for _, res := range jresp.Results {
		scoreMap[res.Index] = res.RelevanceScore
	}

	// 按 Jina 返回的分数降序重排
	sort.Slice(chunks, func(i, j int) bool {
		si := scoreMap[i]
		sj := scoreMap[j]
		return si > sj
	})
	return chunks, nil
}
