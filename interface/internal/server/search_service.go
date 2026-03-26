package server

import (
	"context"
	"encoding/json"
	"fmt"
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

	ctx, cancel := context.WithTimeout(context.Background(), a.searchTimeout)
	defer cancel()
	results, summary, err := a.fetchSearchResults(ctx, query, maxResults, language, searchType)
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

func (a *App) fetchSearchResults(ctx context.Context, query string, maxResults int, language, searchType string) ([]SearchResultItem, string, error) {
	return a.fetchSearchResultsParallel(ctx, query, maxResults, language, searchType)
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
