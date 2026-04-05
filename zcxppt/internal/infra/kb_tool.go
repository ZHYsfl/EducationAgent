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

// doQuery calls the KB Service /api/v1/kb/query-chunks endpoint.
func (kb *KBAgent) doQuery(ctx context.Context, query, subject, userID string, topK int) ([]KBResult, error) {
	if strings.TrimSpace(kb.cfg.BaseURL) == "" {
		return nil, nil // KB not configured
	}

	// Split query into keywords (simple space/comma split)
	keywords := strings.FieldsFunc(query, func(r rune) bool {
		return r == ' ' || r == ',' || r == '，' || r == '、'
	})
	if len(keywords) == 0 {
		keywords = []string{query}
	}

	payload := map[string]any{
		"keywords":        keywords,
		"user_id":         strings.TrimSpace(userID),
		"top_k":           topK,
		"score_threshold": 0.35,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimSuffix(kb.cfg.BaseURL, "/")+"/api/v1/kb/query-chunks",
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

	if len(env.Data) > 0 && (env.Code == 0 || env.Code == 200) {
		// Parse chunks array from data
		var dataObj struct {
			Chunks []struct {
				ChunkID  string                 `json:"chunk_id"`
				Content  string                 `json:"content"`
				Source   string                 `json:"source"`
				Score    float64                `json:"score"`
				Metadata map[string]interface{} `json:"metadata"`
			} `json:"chunks"`
			Total int `json:"total"`
		}
		if err := json.Unmarshal(env.Data, &dataObj); err == nil && len(dataObj.Chunks) > 0 {
			results := make([]KBResult, 0, len(dataObj.Chunks))
			for _, c := range dataObj.Chunks {
				results = append(results, KBResult{
					Summary:  c.Content,
					ChunkID:  c.ChunkID,
					Score:    c.Score,
					DocTitle: c.Source,
				})
			}
			return results, nil
		}
	}

	return nil, nil
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
			topK := 5
			if v, ok := args["top_k"].(float64); ok {
				topK = int(v)
			}

			if strings.TrimSpace(baseURL) == "" {
				return `{"chunks":[],"total":0}`, nil
			}

			// Split query into keywords
			keywords := strings.FieldsFunc(query, func(r rune) bool {
				return r == ' ' || r == ',' || r == '，' || r == '、'
			})
			if len(keywords) == 0 {
				keywords = []string{query}
			}

			payload := map[string]any{
				"keywords":        keywords,
				"top_k":           topK,
				"score_threshold": 0.35,
			}
			body, _ := json.Marshal(payload)
			req, err := http.NewRequestWithContext(ctx, http.MethodPost,
				strings.TrimSuffix(baseURL, "/")+"/api/v1/kb/query-chunks",
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
