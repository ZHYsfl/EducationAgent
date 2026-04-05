package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// SearchProvider is an external web search backend used by fetchSearchResultsParallel.
type SearchProvider interface {
	Search(ctx context.Context, query string, maxResults int, language string) ([]SearchResultItem, error)
}

// SerpAPIProvider uses https://serpapi.com (engine=google).
type SerpAPIProvider struct {
	apiKey string
	client *http.Client
}

func (p *SerpAPIProvider) Search(ctx context.Context, query string, maxResults int, language string) ([]SearchResultItem, error) {
	if strings.TrimSpace(p.apiKey) == "" {
		return nil, fmt.Errorf("SERPAPI_KEY 未配置")
	}
	if maxResults <= 0 {
		maxResults = 10
	}
	if maxResults > 20 {
		maxResults = 20
	}
	u, err := url.Parse("https://serpapi.com/search.json")
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("engine", "google")
	q.Set("q", query)
	q.Set("api_key", p.apiKey)
	q.Set("num", fmt.Sprintf("%d", maxResults))
	if language != "" {
		q.Set("hl", language)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("serpapi 状态码=%d", resp.StatusCode)
	}
	var root struct {
		OrganicResults []struct {
			Title   string `json:"title"`
			Link    string `json:"link"`
			Snippet string `json:"snippet"`
		} `json:"organic_results"`
	}
	if err = json.Unmarshal(body, &root); err != nil {
		return nil, err
	}
	out := make([]SearchResultItem, 0, len(root.OrganicResults))
	for _, o := range root.OrganicResults {
		link := strings.TrimSpace(o.Link)
		if link == "" {
			continue
		}
		out = append(out, SearchResultItem{
			Title:   titleOrDefault(o.Title, o.Snippet),
			URL:     link,
			Snippet: refineSnippet(o.Snippet, query),
			Source:  "serpapi",
		})
		if len(out) >= maxResults {
			break
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("serpapi 无有机结果")
	}
	return out, nil
}

// DuckDuckGoProvider uses the DuckDuckGo instant-answer JSON API (no API key).
type DuckDuckGoProvider struct {
	client *http.Client
}

func (p *DuckDuckGoProvider) Search(ctx context.Context, query string, maxResults int, language string) ([]SearchResultItem, error) {
	if maxResults <= 0 {
		maxResults = 10
	}
	u := "https://api.duckduckgo.com/?format=json&no_html=1&skip_disambig=1&q=" + url.QueryEscape(query)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("duckduckgo 状态码=%d", resp.StatusCode)
	}
	var d struct {
		AbstractURL  string          `json:"AbstractURL"`
		AbstractText string          `json:"AbstractText"`
		Answer       string          `json:"Answer"`
		RelatedTopics json.RawMessage `json:"RelatedTopics"`
	}
	if err = json.Unmarshal(body, &d); err != nil {
		return nil, err
	}
	out := make([]SearchResultItem, 0, maxResults)
	add := func(title, link, snippet string) {
		link = strings.TrimSpace(link)
		if link == "" || len(out) >= maxResults {
			return
		}
		out = append(out, SearchResultItem{
			Title:   titleOrDefault(title, snippet),
			URL:     link,
			Snippet: refineSnippet(snippet, query),
			Source:  "duckduckgo",
		})
	}
	if t := strings.TrimSpace(d.AbstractText); t != "" {
		add(titleFromSnippet(t), d.AbstractURL, t)
	}
	if a := strings.TrimSpace(d.Answer); a != "" && len(out) < maxResults {
		add(titleFromSnippet(a), d.AbstractURL, a)
	}
	var tops []struct {
		Text     string `json:"Text"`
		Result   string `json:"Result"`
		FirstURL string `json:"FirstURL"`
	}
	if len(d.RelatedTopics) > 0 {
		_ = json.Unmarshal(d.RelatedTopics, &tops)
	}
	for _, t := range tops {
		if len(out) >= maxResults {
			break
		}
		title := strings.TrimSpace(t.Text)
		if title == "" {
			title = strings.TrimSpace(t.Result)
		}
		add(title, t.FirstURL, title)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("duckduckgo 无可用结果")
	}
	return out, nil
}

// MetasoProvider is reserved; returns an error until a concrete API mapping exists.
type MetasoProvider struct {
	apiKey  string
	baseURL string
	client  *http.Client
}

func (p *MetasoProvider) Search(ctx context.Context, query string, maxResults int, language string) ([]SearchResultItem, error) {
	if strings.TrimSpace(p.apiKey) == "" {
		return nil, fmt.Errorf("METASO_API_KEY 未配置")
	}
	return nil, fmt.Errorf("Metaso 搜索提供方尚未接入")
}

func buildSearchProviders(csv, legacy, serpKey, metasoKey, metasoURL string) ([]SearchProvider, error) {
	client := &http.Client{Timeout: 20 * time.Second}
	var list []SearchProvider
	parts := strings.Split(csv, ",")
	hasCSV := false
	for _, part := range parts {
		name := strings.ToLower(strings.TrimSpace(part))
		if name == "" {
			continue
		}
		hasCSV = true
		switch name {
		case "serpapi":
			list = append(list, &SerpAPIProvider{apiKey: serpKey, client: client})
		case "metaso":
			list = append(list, &MetasoProvider{apiKey: metasoKey, baseURL: metasoURL, client: client})
		case "duckduckgo", "ddg":
			list = append(list, &DuckDuckGoProvider{client: client})
		}
	}
	if !hasCSV {
		switch strings.ToLower(strings.TrimSpace(legacy)) {
		case "serpapi":
			list = append(list, &SerpAPIProvider{apiKey: serpKey, client: client})
		case "metaso":
			list = append(list, &MetasoProvider{apiKey: metasoKey, baseURL: metasoURL, client: client})
		default:
			list = append(list, &DuckDuckGoProvider{client: client})
		}
	}
	if len(list) == 0 {
		list = append(list, &DuckDuckGoProvider{client: client})
	}
	return list, nil
}

func (a *App) fetchSearchResultsParallel(ctx context.Context, query string, maxResults int, language string) ([]SearchResultItem, string, error) {
	if len(a.searchProviders) == 0 {
		return nil, "", fmt.Errorf("未配置搜索提供方")
	}
	if maxResults <= 0 {
		maxResults = 10
	}

	if strings.EqualFold(a.searchStrategy, "first_success") {
		for _, p := range a.searchProviders {
			items, err := p.Search(ctx, query, maxResults, language)
			if err == nil && len(items) > 0 {
				return items, buildSummary(query, items), nil
			}
		}
		return nil, "", fmt.Errorf("所有搜索源均未返回结果")
	}

	var mu sync.Mutex
	merged := make([]SearchResultItem, 0, maxResults)
	seen := make(map[string]struct{})
	var errs []error
	var wg sync.WaitGroup
	for _, p := range a.searchProviders {
		p := p
		wg.Add(1)
		go func() {
			defer wg.Done()
			items, err := p.Search(ctx, query, maxResults, language)
			if err != nil {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
				return
			}
			mu.Lock()
			for _, it := range items {
				u := strings.TrimSpace(it.URL)
				if u == "" {
					continue
				}
				if _, ok := seen[u]; ok {
					continue
				}
				seen[u] = struct{}{}
				merged = append(merged, it)
				if len(merged) >= maxResults {
					break
				}
			}
			mu.Unlock()
		}()
	}
	wg.Wait()
	if len(merged) == 0 {
		if len(errs) > 0 {
			return nil, "", errs[0]
		}
		return nil, "", fmt.Errorf("未检索到结果")
	}
	if len(merged) > maxResults {
		merged = merged[:maxResults]
	}
	return merged, buildSummary(query, merged), nil
}
