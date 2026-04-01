package infra

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	toolcalling "tool_calling_go"
)

// KBConfig configures the KB tool agent.
type KBConfig struct {
	BaseURL string // e.g. "http://localhost:9200"
	APIKey  string
	Model   string // e.g. "kimi-k2.5"
	LLMKey  string
}

// KBResult is the output of a KB query.
type KBResult struct {
	Summary  string  `json:"summary"`
	ChunkID  string  `json:"chunk_id,omitempty"`
	Score    float64 `json:"score,omitempty"`
	DocTitle string  `json:"doc_title,omitempty"`
}

// KBAgent wraps an OrchestrationAgent with a KB query tool.
// The LLM can call kb_query at any time during conversation to enrich context.
type KBAgent struct {
	orch   *toolcalling.OrchestrationAgent
	client *http.Client
	cfg    KBConfig
}

// NewKBAgent creates a KB agent that the PPT LLM can call on demand.
// Pass this to LLM's orchestration controller so the PPT LLM can
// spontaneously invoke kb_query when it needs domain knowledge.
func NewKBAgent(cfg KBConfig) (*KBAgent, error) {
	kb := &KBAgent{
		client: &http.Client{Timeout: 15 * time.Second},
		cfg:    cfg,
	}

	// Worker agent: performs HTTP KB calls, no tools needed.
	worker := toolcalling.NewAgent(toolcalling.LLMConfig{
		APIKey:  cfg.LLMKey,
		Model:   cfg.Model,
		BaseURL: "https://api.moonshot.cn/v1",
	})

	// Orchestrator: has the kb_query tool, can also call other tools later.
	orch := toolcalling.NewOrchestrationAgent(worker)
	orch.SetWorkerAgent(worker)

	// Register kb_query as an ordinary tool on the orchestrator.
	orch.AddTool(toolcalling.Tool{
		Name:        "kb_query",
		Description: "Query the Knowledge Base to retrieve relevant content. Use this when you need factual knowledge, definitions, or domain-specific information that you are not certain about. Returns structured KB results including summaries, source titles, and relevance scores.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Natural language search query (e.g. '什么是导数的几何意义')",
				},
				"subject": map[string]any{
					"type":        "string",
					"description": "Subject domain (e.g. '数学', '物理', '化学'). Helps narrow retrieval scope.",
				},
				"user_id": map[string]any{
					"type":        "string",
					"description": "User ID for personalized knowledge retrieval",
				},
				"top_k": map[string]any{
					"type":        "number",
					"description": "Maximum number of results to return (default 5, max 10)",
				},
			},
			"required": []any{"query"},
		},
		Function: kb.kbQueryFunc(),
	})

	kb.orch = orch
	return kb, nil
}

// kbQueryFunc returns the ToolFunc for kb_query.
func (kb *KBAgent) kbQueryFunc() toolcalling.ToolFunc {
	return func(ctx context.Context, args map[string]any) (string, error) {
		query, _ := args["query"].(string)
		query = strings.TrimSpace(query)
		if query == "" {
			return "", fmt.Errorf("query must not be empty")
		}

		subject, _ := args["subject"].(string)
		userID, _ := args["user_id"].(string)
		topK := 5
		if v, ok := args["top_k"].(float64); ok && int(v) > 0 {
			topK = int(v)
			if topK > 10 {
				topK = 10
			}
		}

		result, err := kb.doQuery(ctx, query, subject, userID, topK)
		if err != nil {
			return "", fmt.Errorf("kb query failed: %w", err)
		}

		out, err := json.Marshal(result)
		if err != nil {
			return "", fmt.Errorf("marshal result: %w", err)
		}
		return string(out), nil
	}
}

// doQuery calls the KB Service /api/v1/kb/query endpoint.
func (kb *KBAgent) doQuery(ctx context.Context, query, subject, userID string, topK int) ([]KBResult, error) {
	if strings.TrimSpace(kb.cfg.BaseURL) == "" {
		return nil, nil // KB not configured
	}

	payload := map[string]any{
		"query":           query,
		"subject":          strings.TrimSpace(subject),
		"user_id":          strings.TrimSpace(userID),
		"top_k":            topK,
		"score_threshold":  0.35,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimSuffix(kb.cfg.BaseURL, "/")+"/api/v1/kb/query",
		bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := kb.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("kb service returned status %d: %s", resp.StatusCode, string(b))
	}

	var env struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return nil, err
	}

	// Case 1: data is a plain string summary
	if len(env.Data) > 0 && env.Code == 0 {
		var summary string
		if err := json.Unmarshal(env.Data, &summary); err == nil {
			return []KBResult{{Summary: summary}}, nil
		}

		// Case 2: data is an array of chunks
		var chunks []struct {
			Content string  `json:"content"`
			Score   float64 `json:"score"`
			DocID   string  `json:"doc_id"`
		}
		if err := json.Unmarshal(env.Data, &chunks); err == nil && len(chunks) > 0 {
			results := make([]KBResult, 0, len(chunks))
			for _, c := range chunks {
				results = append(results, KBResult{
					Summary:  c.Content,
					Score:   c.Score,
					DocTitle: c.DocID,
				})
			}
			return results, nil
		}
	}

	// Fallback: treat raw body as summary
	b, _ := io.ReadAll(resp.Body)
	return []KBResult{{Summary: strings.TrimSpace(string(b))}}, nil
}

// QueryRaw performs a direct synchronous KB query without agent orchestration.
func (kb *KBAgent) QueryRaw(ctx context.Context, query, subject, userID string, topK int) ([]KBResult, error) {
	return kb.doQuery(ctx, query, subject, userID, topK)
}

// OrchestrationAgent returns the underlying orchestration agent so callers can
// register additional tools (e.g. web_search, memory_recall) or drive a
// multi-tool conversation.
func (kb *KBAgent) OrchestrationAgent() *toolcalling.OrchestrationAgent {
	return kb.orch
}

// ---------------------------------------------------------------------------
// Plain Tool for single-agent integration (no OrchestrationAgent needed)
// ---------------------------------------------------------------------------

// NewKBTool returns a standalone Tool that performs synchronous KB query.
func NewKBTool(baseURL string) toolcalling.Tool {
	client := &http.Client{Timeout: 15 * time.Second}

	return toolcalling.Tool{
		Name:        "kb_query",
		Description: "Query the Knowledge Base to retrieve relevant knowledge chunks. Returns structured results with summaries and relevance scores.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Search query in natural language",
				},
				"subject": map[string]any{
					"type":        "string",
					"description": "Subject domain (optional)",
				},
				"top_k": map[string]any{
					"type":        "number",
					"description": "Max results (default 5)",
				},
			},
			"required": []any{"query"},
		},
		Function: func(ctx context.Context, args map[string]any) (string, error) {
			query, _ := args["query"].(string)
			subject, _ := args["subject"].(string)
			topK := 5
			if v, ok := args["top_k"].(float64); ok {
				topK = int(v)
			}

			if strings.TrimSpace(baseURL) == "" {
				return `{"summary":"KB service not configured","results":[]}`, nil
			}

			payload := map[string]any{
				"query":           query,
				"subject":          subject,
				"top_k":            topK,
				"score_threshold":  0.35,
			}
			body, _ := json.Marshal(payload)
			req, err := http.NewRequestWithContext(ctx, http.MethodPost,
				strings.TrimSuffix(baseURL, "/")+"/api/v1/kb/query",
				bytes.NewReader(body))
			if err != nil {
				return "", err
			}
			req.Header.Set("Content-Type", "application/json")
			resp, err := client.Do(req)
			if err != nil {
				return "", err
			}
			defer resp.Body.Close()
			b, _ := io.ReadAll(resp.Body)
			if resp.StatusCode >= 300 {
				return "", fmt.Errorf("kb returned %d: %s", resp.StatusCode, string(b))
			}
			var env struct {
				Data json.RawMessage `json:"data"`
			}
			if json.Unmarshal(b, &env) == nil && env.Data != nil {
				return string(env.Data), nil
			}
			return string(b), nil
		},
	}
}

// AddKBToolToAgent registers kb_query on any Agent so the LLM can call it on demand.
func AddKBToolToAgent(a *toolcalling.Agent, kbBaseURL string) {
	a.AddTool(NewKBTool(kbBaseURL))
}

// CallKBTool directly invokes the KB query tool synchronously (for internal use).
func CallKBTool(ctx context.Context, kbBaseURL, query, subject string) ([]KBResult, error) {
	kb := &KBAgent{client: &http.Client{Timeout: 15 * time.Second}, cfg: KBConfig{BaseURL: kbBaseURL}}
	return kb.doQuery(ctx, query, subject, "", 5)
}
