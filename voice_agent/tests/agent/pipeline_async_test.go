package agent_test

import (
	agent "voiceagent/agent"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
	"toolcalling"
)

func mockOpenAIChatServer(status int, content string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if status >= 400 {
			_, _ = w.Write([]byte(`{"error":{"message":"upstream error"}}`))
			return
		}
		body := fmt.Sprintf(`{
			"id":"test",
			"object":"chat.completion",
			"choices":[{"index":0,"message":{"role":"assistant","content":%q},"finish_reason":"stop"}]
		}`, content)
		_, _ = w.Write([]byte(body))
	}))
}

func newInterruptAgent(baseURL string) *toolcalling.Agent {
	return toolcalling.NewAgent(toolcalling.LLMConfig{
		APIKey:  "test-key",
		Model:   "test-model",
		BaseURL: baseURL,
	})
}

func TestIsInterrupt_LabelParsing(t *testing.T) {
	cases := []struct {
		name    string
		resp    string
		wantInt bool
	}{
		{name: "exact interrupt", resp: "interrupt", wantInt: true},
		{name: "exact do not interrupt", resp: "do not interrupt", wantInt: false},
		{name: "dash form", resp: "do-not-interrupt", wantInt: false},
		{name: "contains interrupt", resp: "this should interrupt.", wantInt: true},
		{name: "think wrapped", resp: "<think>reason</think>do not interrupt", wantInt: false},
		{name: "unknown fallback", resp: "not sure", wantInt: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := mockOpenAIChatServer(http.StatusOK, tc.resp)
			defer srv.Close()
			got := agent.IsInterrupt(context.Background(), newInterruptAgent(srv.URL), "test utterance")
			if got != tc.wantInt {
				t.Fatalf("isInterrupt=%v, want %v (resp=%q)", got, tc.wantInt, tc.resp)
			}
		})
	}
}

func TestIsInterrupt_OnChatErrorFallsBackTrue(t *testing.T) {
	srv := mockOpenAIChatServer(http.StatusInternalServerError, "")
	defer srv.Close()
	if !agent.IsInterrupt(context.Background(), newInterruptAgent(srv.URL), "test") {
		t.Fatal("expected fallback true when chat errors")
	}
}

func TestLaunchAsyncContextQueries_AllSources(t *testing.T) {
	m := &agent.MockServices{
		QueryKBFn: func(ctx context.Context, req agent.KBQueryRequest) (agent.KBQueryResponse, error) {
			return agent.KBQueryResponse{
				Summary: "知识库总结：chunk-a",
			}, nil
		},
		RecallMemoryFn: func(ctx context.Context, req agent.MemoryRecallRequest) (agent.MemoryRecallResponse, error) {
			return agent.MemoryRecallResponse{
				Facts: []agent.MemoryEntry{{Content: "fact-a"}},
			}, nil
		},
		SearchWebFn: func(ctx context.Context, req agent.SearchRequest) (agent.SearchResponse, error) {
			return agent.SearchResponse{
				Results: []agent.SearchResult{{Title: "r1", URL: "https://example.com", Snippet: "s1", Source: "web"}},
			}, nil
		},
	}
	s := agent.NewTestSession(m)
	p := agent.NewTestPipeline(s, m)

	p.LaunchAsyncContextQueries(context.Background(), "牛顿第二定律")
	time.Sleep(300 * time.Millisecond)

	msgs := p.DrainContextQueue()
	if len(msgs) < 2 {
		t.Fatalf("expected >=2 context messages, got %d", len(msgs))
	}
}

func TestLaunchAsyncContextQueries_IngestWhenKBScoreLow(t *testing.T) {
	var called bool
	var mu sync.Mutex
	m := &agent.MockServices{
		QueryKBFn: func(ctx context.Context, req agent.KBQueryRequest) (agent.KBQueryResponse, error) {
			return agent.KBQueryResponse{Summary: "低分总结"}, nil
		},
		RecallMemoryFn: func(ctx context.Context, req agent.MemoryRecallRequest) (agent.MemoryRecallResponse, error) {
			return agent.MemoryRecallResponse{}, nil
		},
		SearchWebFn: func(ctx context.Context, req agent.SearchRequest) (agent.SearchResponse, error) {
			return agent.SearchResponse{Results: []agent.SearchResult{{Title: "w", URL: "https://e.com", Snippet: "x"}}}, nil
		},
		IngestFromSearchFn: func(ctx context.Context, req agent.IngestFromSearchRequest) error {
			mu.Lock()
			called = true
			mu.Unlock()
			return nil
		},
	}
	s := agent.NewTestSession(m)
	p := agent.NewTestPipeline(s, m)
	p.LaunchAsyncContextQueries(context.Background(), "稀有问题")
	time.Sleep(400 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if !called {
		t.Fatal("expected IngestFromSearch to be called when KB top score < 0.5")
	}
}

func TestLaunchAsyncContextQueries_NoIngestWhenKBScoreHigh(t *testing.T) {
	var called bool
	var mu sync.Mutex
	m := &agent.MockServices{
		QueryKBFn: func(ctx context.Context, req agent.KBQueryRequest) (agent.KBQueryResponse, error) {
			return agent.KBQueryResponse{Summary: "高分总结"}, nil
		},
		RecallMemoryFn: func(ctx context.Context, req agent.MemoryRecallRequest) (agent.MemoryRecallResponse, error) {
			return agent.MemoryRecallResponse{}, nil
		},
		SearchWebFn: func(ctx context.Context, req agent.SearchRequest) (agent.SearchResponse, error) {
			return agent.SearchResponse{Results: []agent.SearchResult{{Title: "w", URL: "https://e.com", Snippet: "x"}}}, nil
		},
		IngestFromSearchFn: func(ctx context.Context, req agent.IngestFromSearchRequest) error {
			mu.Lock()
			called = true
			mu.Unlock()
			return nil
		},
	}
	s := agent.NewTestSession(m)
	p := agent.NewTestPipeline(s, m)
	p.LaunchAsyncContextQueries(context.Background(), "常见问题")
	time.Sleep(400 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if called {
		t.Fatal("did not expect IngestFromSearch when KB top score >= 0.5")
	}
}
