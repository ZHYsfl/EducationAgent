package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

// SearchService defines the web search interface.
type SearchService interface {
	SearchWeb(ctx context.Context, query string) (string, error)
}

const (
	defaultSearchAPIURL    = "https://api.tavily.com/search"
	defaultSearchLLMModel  = "gpt-4o-mini"
	maxSearchResultSnippet = 10
)

// DefaultSearchService integrates a third-party search API and an LLM summarizer.
type DefaultSearchService struct {
	httpClient   *http.Client
	searchAPI    string
	searchAPIKey string
	llmClient    openai.Client
	llmModel     string
	llmEnabled   bool
}

// NewSearchService creates a new default search service.
func NewSearchService() SearchService {
	searchAPI := strings.TrimSpace(os.Getenv("SEARCH_API_URL"))
	if searchAPI == "" {
		searchAPI = defaultSearchAPIURL
	}

	llmModel := strings.TrimSpace(os.Getenv("SEARCH_SUMMARY_MODEL"))
	if llmModel == "" {
		llmModel = defaultSearchLLMModel
	}

	var llmClient openai.Client
	openAIKey := strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	if openAIKey != "" {
		opts := []option.RequestOption{option.WithAPIKey(openAIKey)}
		if baseURL := strings.TrimSpace(os.Getenv("OPENAI_BASE_URL")); baseURL != "" {
			opts = append(opts, option.WithBaseURL(baseURL))
		}
		llmClient = openai.NewClient(opts...)
	}

	return &DefaultSearchService{
		httpClient: &http.Client{
			Timeout: 20 * time.Second,
		},
		searchAPI:    searchAPI,
		searchAPIKey: strings.TrimSpace(os.Getenv("TAVILY_API_KEY")),
		llmClient:    llmClient,
		llmModel:     llmModel,
		llmEnabled:   openAIKey != "",
	}
}

func (s *DefaultSearchService) SearchWeb(ctx context.Context, query string) (string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "", errors.New("query must not be empty")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

	// When no external search API key is configured, fall back to local KB.
	if s.searchAPIKey == "" {
		return s.searchLocalKB(ctx, query)
	}

	results, err := s.search(ctx, query)
	if err != nil {
		return "", fmt.Errorf("search api failed: %w", err)
	}
	if len(results) == 0 {
		return "No relevant web results found for: " + query, nil
	}

	summary, err := s.summarize(ctx, query, results)
	if err != nil {
		// Keep service available even if LLM is temporarily unavailable.
		return fallbackSummary(query, results), nil
	}
	return summary, nil
}

func (s *DefaultSearchService) searchLocalKB(ctx context.Context, query string) (string, error) {
	kb := NewKBService()
	chunks, total, err := kb.QueryChunks(ctx, query)
	if err != nil {
		return "", err
	}
	if total == 0 {
		return fmt.Sprintf("No relevant results found for: %s", query), nil
	}

	maxItems := 3
	if len(chunks) < maxItems {
		maxItems = len(chunks)
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Found %d relevant results for: %s\n", total, query))
	for i := 0; i < maxItems; i++ {
		content := strings.TrimSpace(chunks[i].Content)
		content = strings.ReplaceAll(content, "\r", " ")
		content = strings.ReplaceAll(content, "\n", " ")
		content = strings.Join(strings.Fields(content), " ")
		if len(content) > 140 {
			content = content[:140] + "..."
		}
		b.WriteString(fmt.Sprintf("%d) %s", i+1, content))
		if i != maxItems-1 {
			b.WriteString("\n")
		}
	}
	return b.String(), nil
}

type tavilySearchRequest struct {
	APIKey        string `json:"api_key"`
	Query         string `json:"query"`
	SearchDepth   string `json:"search_depth"`
	MaxResults    int    `json:"max_results"`
	IncludeAnswer bool   `json:"include_answer"`
}

type tavilySearchResponse struct {
	Results []struct {
		Title   string `json:"title"`
		URL     string `json:"url"`
		Content string `json:"content"`
	} `json:"results"`
}

type searchResult struct {
	Title   string
	URL     string
	Content string
}

func (s *DefaultSearchService) search(ctx context.Context, query string) ([]searchResult, error) {
	if s.searchAPIKey == "" {
		return nil, errors.New("missing TAVILY_API_KEY")
	}

	payload := tavilySearchRequest{
		APIKey:        s.searchAPIKey,
		Query:         query,
		SearchDepth:   "advanced",
		MaxResults:    maxSearchResultSnippet,
		IncludeAnswer: false,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.searchAPI, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("status=%d body=%s", resp.StatusCode, string(raw))
	}

	var parsed tavilySearchResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, err
	}

	out := make([]searchResult, 0, len(parsed.Results))
	for _, r := range parsed.Results {
		out = append(out, searchResult{
			Title:   strings.TrimSpace(r.Title),
			URL:     strings.TrimSpace(r.URL),
			Content: strings.TrimSpace(r.Content),
		})
	}
	return out, nil
}

func (s *DefaultSearchService) summarize(ctx context.Context, query string, results []searchResult) (string, error) {
	if !s.llmEnabled {
		return "", errors.New("llm client not configured")
	}

	var b strings.Builder
	for i, r := range results {
		fmt.Fprintf(&b, "[%d] %s\nURL: %s\nSnippet: %s\n\n", i+1, r.Title, r.URL, r.Content)
	}

	prompt := fmt.Sprintf("User query: %s\n\nSearch results:\n%s", query, b.String())
	resp, err := s.llmClient.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model: openai.ChatModel(s.llmModel),
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage("You are a factual web-search summarizer. Write a concise answer in Chinese. Use only provided snippets, avoid fabrication, and mention uncertainty when evidence is weak."),
			openai.UserMessage(prompt),
		},
	})
	if err != nil {
		return "", err
	}
	if len(resp.Choices) == 0 {
		return "", errors.New("empty llm response")
	}
	return strings.TrimSpace(resp.Choices[0].Message.Content), nil
}

func fallbackSummary(query string, results []searchResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Search results for \"%s\":\n", query)
	limit := len(results)
	if limit > 3 {
		limit = 3
	}
	for i := 0; i < limit; i++ {
		r := results[i]
		fmt.Fprintf(&b, "%d) %s - %s\n", i+1, nonEmpty(r.Title, "Untitled"), r.URL)
	}
	return strings.TrimSpace(b.String())
}

func nonEmpty(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return strings.TrimSpace(v)
}
