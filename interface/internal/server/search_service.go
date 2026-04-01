package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

func (a *App) runSearchPipeline(ctx context.Context, userID, query string, maxResults int, language string) (results []SearchResultItem, summary string, duration int64, status string) {
	start := time.Now()
	elapsed := func() int64 { return time.Since(start).Milliseconds() }

	if a.kbDedupEnabled {
		hit, err := a.kbLikelyHasAnswer(userID, query)
		if err == nil && hit {
			summary = "知识库已有高相关内容，本次不重复回注搜索结果。"
			return nil, summary, elapsed(), "success"
		}
	}

	results, summary, err := a.fetchSearchResults(ctx, query, maxResults, language)
	if err != nil {
		return nil, err.Error(), elapsed(), "failed"
	}
	if len(results) == 0 {
		if strings.TrimSpace(summary) == "" {
			summary = "未检索到结果"
		}
		return nil, summary, elapsed(), "partial"
	}
	return results, summary, elapsed(), "success"
}

func (a *App) fetchSearchResults(ctx context.Context, query string, maxResults int, language string) ([]SearchResultItem, string, error) {
	return a.fetchSearchResultsParallel(ctx, query, maxResults, language)
}

// mapPipelineStatusToSection8 maps internal pipeline outcomes to §8 polling states:
// pending is only set at task creation; success/partial → completed; failed → failed.
func mapPipelineStatusToSection8(pipeline string) string {
	switch pipeline {
	case "failed":
		return "failed"
	default:
		return "completed"
	}
}

// NormalizeStoredStatusForSection8 maps persisted search_requests.status values to §8 GET
// response values. Legacy rows may still hold success/partial from pre-async storage.
func NormalizeStoredStatusForSection8(st string) string {
	switch strings.ToLower(strings.TrimSpace(st)) {
	case "pending":
		return "pending"
	case "failed":
		return "failed"
	case "completed", "success", "partial":
		return "completed"
	case "":
		return "failed"
	default:
		return "completed"
	}
}

func truncateRunes(s string, maxRunes int) string {
	r := []rune(s)
	if maxRunes <= 0 || len(r) <= maxRunes {
		return s
	}
	return string(r[:maxRunes]) + "…"
}

func (a *App) markSearchJobPersistFailed(requestID string, duration int64, cause error) {
	msg := "保存搜索结果失败"
	if cause != nil {
		msg = fmt.Sprintf("%s: %v", msg, cause)
	}
	msg = truncateRunes(msg, 2000)
	fallback := map[string]interface{}{
		"status":     "failed",
		"results":    "[]",
		"summary":    msg,
		"duration":   duration,
		"updated_at": nowMs(),
	}
	if err := a.db.Model(&SearchRequestModel{}).Where("request_id = ?", requestID).Updates(fallback).Error; err != nil {
		log.Printf("search job persist fallback failed request_id=%s: %v", requestID, err)
	}
}

func (a *App) runSearchJob(requestID, userID, query string, maxResults int, language string) {
	ctx, cancel := context.WithTimeout(context.Background(), a.searchTimeout)
	defer cancel()
	results, summary, duration, pipelineStatus := a.runSearchPipeline(ctx, userID, query, maxResults, language)
	status := mapPipelineStatusToSection8(pipelineStatus)
	resultsJSON := "[]"
	if len(results) > 0 {
		b, err := json.Marshal(results)
		if err != nil {
			resultsJSON = "[]"
		} else {
			resultsJSON = string(b)
		}
	}
	err := a.db.Model(&SearchRequestModel{}).Where("request_id = ?", requestID).Updates(map[string]interface{}{
		"status":     status,
		"results":    resultsJSON,
		"summary":    summary,
		"duration":   duration,
		"updated_at": nowMs(),
	}).Error
	if err != nil {
		a.markSearchJobPersistFailed(requestID, duration, err)
	}
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
