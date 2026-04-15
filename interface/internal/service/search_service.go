package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// SearchService defines the web search interface.
type SearchService interface {
	SearchWeb(ctx context.Context, query string) (string, error)
}

// DefaultSearchService implements SearchService with a lightweight web query.
type DefaultSearchService struct {
	httpClient *http.Client
	apiURL     string
	maxItems   int
}

// NewSearchService creates a default web search service.
func NewSearchService() SearchService {
	return &DefaultSearchService{
		httpClient: &http.Client{Timeout: 12 * time.Second},
		apiURL:     "https://api.duckduckgo.com/",
		maxItems:   5,
	}
}

type ddgResponse struct {
	AbstractText  string          `json:"AbstractText"`
	Answer        string          `json:"Answer"`
	RelatedTopics json.RawMessage `json:"RelatedTopics"`
}

type ddgTopic struct {
	Text     string     `json:"Text"`
	FirstURL string     `json:"FirstURL"`
	Topics   []ddgTopic `json:"Topics"`
}

// SearchWeb performs a web lookup and returns a compact natural-language summary.
func (s *DefaultSearchService) SearchWeb(ctx context.Context, query string) (string, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return "", fmt.Errorf("query 不能为空")
	}

	reqURL, err := url.Parse(s.apiURL)
	if err != nil {
		return "", fmt.Errorf("无效搜索 API 地址: %w", err)
	}
	params := reqURL.Query()
	params.Set("q", q)
	params.Set("format", "json")
	params.Set("no_html", "1")
	params.Set("skip_disambig", "1")
	reqURL.RawQuery = params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL.String(), nil)
	if err != nil {
		return "", fmt.Errorf("创建搜索请求失败: %w", err)
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("搜索请求失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("搜索服务返回状态码 %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return "", fmt.Errorf("读取搜索响应失败: %w", err)
	}

	var raw ddgResponse
	if err = json.Unmarshal(body, &raw); err != nil {
		return "", fmt.Errorf("解析搜索响应失败: %w", err)
	}

	summary := strings.TrimSpace(raw.AbstractText)
	if summary == "" {
		summary = strings.TrimSpace(raw.Answer)
	}

	if summary == "" {
		highlights := flattenTopics(raw.RelatedTopics, s.maxItems)
		if len(highlights) == 0 {
			return fmt.Sprintf("未检索到与“%s”直接相关的公开网页结果。", q), nil
		}
		return fmt.Sprintf("围绕“%s”的检索要点：%s。", q, strings.Join(highlights, "；")), nil
	}

	summary = strings.ReplaceAll(summary, "\n", " ")
	summary = strings.TrimSpace(summary)
	return fmt.Sprintf("围绕“%s”的搜索结果摘要：%s", q, summary), nil
}

func flattenTopics(raw json.RawMessage, maxItems int) []string {
	if len(raw) == 0 || maxItems <= 0 {
		return nil
	}
	var topics []ddgTopic
	if err := json.Unmarshal(raw, &topics); err != nil {
		return nil
	}
	out := make([]string, 0, maxItems)
	var walk func(items []ddgTopic)
	walk = func(items []ddgTopic) {
		for _, item := range items {
			if len(out) >= maxItems {
				return
			}
			text := strings.TrimSpace(item.Text)
			if text != "" {
				out = append(out, text)
			}
			if len(item.Topics) > 0 {
				walk(item.Topics)
				if len(out) >= maxItems {
					return
				}
			}
		}
	}
	walk(topics)
	return out
}
