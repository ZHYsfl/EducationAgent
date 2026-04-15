package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSearchWeb_SummaryFromAbstract(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"AbstractText":"导数表示函数瞬时变化率。","RelatedTopics":[]}`))
	}))
	defer srv.Close()

	s := &DefaultSearchService{
		httpClient: &http.Client{Timeout: 3 * time.Second},
		apiURL:     srv.URL,
		maxItems:   5,
	}

	got, err := s.SearchWeb(context.Background(), "导数")
	if err != nil {
		t.Fatalf("SearchWeb err=%v", err)
	}
	if !strings.Contains(got, "搜索结果摘要") {
		t.Fatalf("unexpected summary: %s", got)
	}
}

func TestSearchWeb_SummaryFromRelatedTopics(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"AbstractText":"","RelatedTopics":[{"Text":"切线斜率与导数等价"},{"Text":"导数可用于最优化"}]}`))
	}))
	defer srv.Close()

	s := &DefaultSearchService{
		httpClient: &http.Client{Timeout: 3 * time.Second},
		apiURL:     srv.URL,
		maxItems:   5,
	}

	got, err := s.SearchWeb(context.Background(), "导数")
	if err != nil {
		t.Fatalf("SearchWeb err=%v", err)
	}
	if !strings.Contains(got, "检索要点") {
		t.Fatalf("unexpected summary: %s", got)
	}
}
