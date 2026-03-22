package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func (a *App) runSearchAsync(requestID, userID, query string, maxResults int, language, searchType string) {
	start := time.Now()
	if a.kbDedupEnabled {
		hit, err := a.kbLikelyHasAnswer(userID, query)
		if err == nil && hit {
			a.finishSearch(requestID, "completed", nil, "知识库已有高相关内容，本次不重复回注搜索结果。", time.Since(start).Milliseconds(), "")
			return
		}
	}

	results, summary, err := a.fetchSearchResults(query, maxResults, language, searchType)
	if err != nil {
		a.finishSearch(requestID, "failed", nil, "", time.Since(start).Milliseconds(), err.Error())
		return
	}
	a.finishSearch(requestID, "completed", results, summary, time.Since(start).Milliseconds(), "")
}

func (a *App) finishSearch(requestID, status string, results []SearchResultItem, summary string, duration int64, lastErr string) {
	resultsJSON := ""
	if len(results) > 0 {
		b, _ := json.Marshal(results)
		resultsJSON = string(b)
	}
	updates := map[string]interface{}{"status": status, "results": resultsJSON, "summary": summary, "duration": duration, "updated_at": nowMs()}
	if lastErr != "" {
		updates["summary"] = lastErr
	}
	_ = a.db.Model(&SearchRequestModel{}).Where("request_id = ?", requestID).Updates(updates).Error
}

func (a *App) fetchSearchResults(query string, maxResults int, language, searchType string) ([]SearchResultItem, string, error) {
	switch a.searchProvider {
	case "serpapi":
		return a.searchBySerpAPI(query, maxResults, language, searchType)
	case "metaso":
		return a.searchByMetaso(query, maxResults, searchType)
	default:
		return a.searchByDuckDuckGo(query, maxResults, searchType)
	}
}

func (a *App) searchBySerpAPI(query string, maxResults int, language, searchType string) ([]SearchResultItem, string, error) {
	if a.serpAPIKey == "" {
		return nil, "", errors.New("SERPAPI_KEY 未配置")
	}
	u := "https://serpapi.com/search.json?q=" + url.QueryEscape(query) + "&api_key=" + url.QueryEscape(a.serpAPIKey) + "&hl=" + url.QueryEscape(language)
	if searchType == "news" {
		u += "&tbm=nws"
	}
	resp, err := http.Get(u)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("serpapi 返回状态码 %d", resp.StatusCode)
	}
	var raw map[string]interface{}
	if err = json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, "", err
	}
	items := make([]SearchResultItem, 0, maxResults)
	if arr, ok := raw["organic_results"].([]interface{}); ok {
		for _, v := range arr {
			if len(items) >= maxResults {
				break
			}
			m, _ := v.(map[string]interface{})
			items = append(items, SearchResultItem{Title: toString(m["title"]), URL: toString(m["link"]), Snippet: refineSnippet(toString(m["snippet"]), query), Source: hostOf(toString(m["link"]))})
		}
	}
	if len(items) == 0 {
		return nil, "", errors.New("未检索到有效结果")
	}
	return items, buildSummary(query, items), nil
}

func (a *App) searchByDuckDuckGo(query string, maxResults int, searchType string) ([]SearchResultItem, string, error) {
	q := query
	if searchType == "news" {
		q = query + " 最新 新闻"
	} else if searchType == "academic" {
		q = query + " 学术 论文"
	}
	u := "https://api.duckduckgo.com/?q=" + url.QueryEscape(q) + "&format=json&no_html=1&skip_disambig=1"
	resp, err := http.Get(u)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, "", fmt.Errorf("duckduckgo 返回状态码 %d", resp.StatusCode)
	}
	var raw struct {
		AbstractText  string `json:"AbstractText"`
		AbstractURL   string `json:"AbstractURL"`
		Heading       string `json:"Heading"`
		RelatedTopics []struct {
			Text     string `json:"Text"`
			FirstURL string `json:"FirstURL"`
		} `json:"RelatedTopics"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, "", err
	}
	items := make([]SearchResultItem, 0, maxResults)
	if raw.AbstractText != "" {
		items = append(items, SearchResultItem{Title: raw.Heading, URL: raw.AbstractURL, Snippet: refineSnippet(raw.AbstractText, query), Source: hostOf(raw.AbstractURL)})
	}
	for _, t := range raw.RelatedTopics {
		if len(items) >= maxResults {
			break
		}
		if t.Text == "" {
			continue
		}
		items = append(items, SearchResultItem{Title: titleFromSnippet(t.Text), URL: t.FirstURL, Snippet: refineSnippet(t.Text, query), Source: hostOf(t.FirstURL)})
	}
	if len(items) == 0 {
		return nil, "", errors.New("未检索到有效结果")
	}
	return items, buildSummary(query, items), nil
}

func (a *App) searchByMetaso(query string, maxResults int, searchType string) ([]SearchResultItem, string, error) {
	if a.metasoAPIKey == "" {
		return nil, "", errors.New("METASO_API_KEY 未配置")
	}

	metaType := "web"
	if searchType == "news" {
		metaType = "news"
	} else if searchType == "academic" {
		metaType = "academic"
	}

	body := map[string]interface{}{"query": query, "count": maxResults, "search_type": metaType}
	payload, _ := json.Marshal(body)

	req, err := http.NewRequest(http.MethodPost, a.metasoAPIURL, strings.NewReader(string(payload)))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.metasoAPIKey)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		rawErr, _ := io.ReadAll(resp.Body)
		return nil, "", fmt.Errorf("metaso 返回状态码 %d: %s", resp.StatusCode, string(rawErr))
	}

	var raw map[string]interface{}
	if err = json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, "", err
	}
	items := make([]SearchResultItem, 0, maxResults)
	for _, key := range []string{"results", "data", "items"} {
		arr, ok := raw[key].([]interface{})
		if !ok {
			continue
		}
		for _, v := range arr {
			if len(items) >= maxResults {
				break
			}
			m, _ := v.(map[string]interface{})
			title := toString(m["title"])
			link := toString(m["url"])
			if link == "" {
				link = toString(m["link"])
			}
			snippet := toString(m["snippet"])
			if snippet == "" {
				snippet = toString(m["summary"])
			}
			source := toString(m["source"])
			if source == "" {
				source = hostOf(link)
			}
			if title == "" && snippet == "" {
				continue
			}
			items = append(items, SearchResultItem{Title: titleOrDefault(title, snippet), URL: link, Snippet: refineSnippet(snippet, query), Source: source})
		}
		if len(items) > 0 {
			break
		}
	}
	if len(items) == 0 {
		return nil, "", errors.New("秘塔 API 未返回可用结果")
	}
	return items, buildSummary(query, items), nil
}

func (a *App) kbLikelyHasAnswer(userID, query string) (bool, error) {
	body := map[string]interface{}{"user_id": userID, "query": query, "top_k": 1, "score_threshold": a.kbScoreThreshold}
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, a.kbQueryURL, strings.NewReader(string(b)))
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return false, fmt.Errorf("kb query status=%d", resp.StatusCode)
	}
	var raw struct {
		Code int `json:"code"`
		Data struct {
			Chunks []struct {
				Score float64 `json:"score"`
			} `json:"chunks"`
		} `json:"data"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return false, err
	}
	if raw.Code != 200 || len(raw.Data.Chunks) == 0 {
		return false, nil
	}
	return raw.Data.Chunks[0].Score >= a.kbScoreThreshold, nil
}
