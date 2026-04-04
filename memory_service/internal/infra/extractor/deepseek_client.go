package extractor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"memory_service/internal/model"
)

const (
	DefaultDeepSeekBaseURL = "https://api.deepseek.com"
	DefaultDeepSeekModel   = "deepseek-chat"
)

type DeepSeekClient struct {
	apiKey     string
	baseURL    string
	model      string
	httpClient *http.Client
}

type deepSeekChatRequest struct {
	Model          string                `json:"model"`
	Messages       []deepSeekChatMessage `json:"messages"`
	ResponseFormat deepSeekResponseFmt   `json:"response_format"`
	MaxTokens      int                   `json:"max_tokens,omitempty"`
	Stream         bool                  `json:"stream"`
}

type deepSeekResponseFmt struct {
	Type string `json:"type"`
}

type deepSeekChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type deepSeekChatResponse struct {
	Choices []struct {
		FinishReason string `json:"finish_reason"`
		Message      struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    any    `json:"code"`
	} `json:"error,omitempty"`
}

type deepSeekExtractionPayload struct {
	Facts               []deepSeekMemoryCandidate `json:"facts"`
	Preferences         []deepSeekMemoryCandidate `json:"preferences"`
	TeachingElements    deepSeekTeachingElements  `json:"teaching_elements"`
	ConversationSummary string                    `json:"conversation_summary"`
}

type deepSeekMemoryCandidate struct {
	Key        string  `json:"key"`
	Value      string  `json:"value"`
	Context    string  `json:"context"`
	Confidence float64 `json:"confidence"`
	Source     string  `json:"source"`
}

type deepSeekTeachingElements struct {
	KnowledgePoints []string `json:"knowledge_points"`
	TeachingGoals   []string `json:"teaching_goals"`
	KeyDifficulties []string `json:"key_difficulties"`
	TargetAudience  string   `json:"target_audience"`
	Duration        string   `json:"duration"`
	OutputStyle     string   `json:"output_style"`
}

func NewDeepSeekClient(apiKey, baseURL, model string, timeout time.Duration) (*DeepSeekClient, error) {
	if strings.TrimSpace(apiKey) == "" {
		return nil, errors.New("deepseek api key is required")
	}
	if strings.TrimSpace(baseURL) == "" {
		baseURL = DefaultDeepSeekBaseURL
	}
	if strings.TrimSpace(model) == "" {
		model = DefaultDeepSeekModel
	}
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	return &DeepSeekClient{
		apiKey:  strings.TrimSpace(apiKey),
		baseURL: strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		model:   strings.TrimSpace(model),
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}, nil
}

func (c *DeepSeekClient) ExtractRequirementDialogue(ctx context.Context, req LLMRequest) (LLMResponse, error) {
	body := deepSeekChatRequest{
		Model: c.model,
		Messages: []deepSeekChatMessage{
			{Role: "system", Content: buildDeepSeekSystemPrompt()},
			{Role: "user", Content: buildDeepSeekUserPrompt(req)},
		},
		ResponseFormat: deepSeekResponseFmt{Type: "json_object"},
		MaxTokens:      1200,
		Stream:         false,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return LLMResponse{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return LLMResponse{}, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return LLMResponse{}, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return LLMResponse{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return LLMResponse{}, fmt.Errorf("deepseek request failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var chatResp deepSeekChatResponse
	if err := json.Unmarshal(raw, &chatResp); err != nil {
		return LLMResponse{}, err
	}
	if chatResp.Error != nil {
		return LLMResponse{}, fmt.Errorf("deepseek api error: %s", strings.TrimSpace(chatResp.Error.Message))
	}
	if len(chatResp.Choices) == 0 {
		return LLMResponse{}, errors.New("deepseek returned no choices")
	}
	content := strings.TrimSpace(chatResp.Choices[0].Message.Content)
	if content == "" {
		return LLMResponse{}, errors.New("deepseek returned empty content")
	}

	var extracted deepSeekExtractionPayload
	if err := json.Unmarshal([]byte(content), &extracted); err != nil {
		return LLMResponse{}, err
	}
	return LLMResponse{
		Facts:               toMemoryEntries(extracted.Facts),
		Preferences:         toMemoryEntries(extracted.Preferences),
		TeachingElements:    toTeachingElements(extracted.TeachingElements),
		ConversationSummary: strings.TrimSpace(extracted.ConversationSummary),
	}, nil
}

func buildDeepSeekSystemPrompt() string {
	return strings.TrimSpace(`
You are extracting memory from a teacher's lesson and PPT requirement-collection dialogue.
The dialogue comes from a Voice Agent collecting TaskRequirements for courseware preparation.

Return JSON only.

Goals:
1. Extract stable teacher facts that are durable beyond the current lesson task.
2. Extract stable teacher preferences that are durable beyond the current lesson task.
3. Extract current-session teaching_elements for working memory.
4. Produce a concise conversation_summary that preserves requirement-collection progress, teacher planning intent, and unresolved ambiguities.

Persistence policy:
- Do NOT treat one-off task requirements as durable teacher preferences unless the dialogue clearly says they are standing preferences across tasks.
- Current topic, current lesson goals, current audience, current duration, current key difficulties, and current output style usually belong in teaching_elements, not long-term memory.
- Teaching logic, reference-file usage instructions, interaction ideas, and one-off constraints should primarily be preserved through conversation_summary.

Output JSON schema:
{
  "facts": [
    {
      "key": "string",
      "value": "string",
      "context": "general",
      "confidence": 0.0,
      "source": "explicit|inferred"
    }
  ],
  "preferences": [
    {
      "key": "string",
      "value": "string",
      "context": "general|visual_preferences",
      "confidence": 0.0,
      "source": "explicit|inferred"
    }
  ],
  "teaching_elements": {
    "knowledge_points": ["string"],
    "teaching_goals": ["string"],
    "key_difficulties": ["string"],
    "target_audience": "string",
    "duration": "string",
    "output_style": "string"
  },
  "conversation_summary": "string"
}

Use empty arrays or empty strings when unknown. Include the word json in your reasoning target by strictly returning valid JSON.
`)
}

func buildDeepSeekUserPrompt(req LLMRequest) string {
	var b strings.Builder
	b.WriteString("Extract memory from this teacher requirement-collection dialogue and return json.\n")
	b.WriteString("user_id: " + strings.TrimSpace(req.UserID) + "\n")
	b.WriteString("session_id: " + strings.TrimSpace(req.SessionID) + "\n")
	b.WriteString("dialogue:\n")
	for _, msg := range req.Messages {
		role := strings.ToLower(strings.TrimSpace(msg.Role))
		if role == "" {
			role = "user"
		}
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		b.WriteString("- " + role + ": " + content + "\n")
	}
	return b.String()
}

func toMemoryEntries(in []deepSeekMemoryCandidate) []model.MemoryEntry {
	out := make([]model.MemoryEntry, 0, len(in))
	for _, item := range in {
		out = append(out, model.MemoryEntry{
			Key:        strings.TrimSpace(item.Key),
			Value:      strings.TrimSpace(item.Value),
			Context:    strings.TrimSpace(item.Context),
			Confidence: item.Confidence,
			Source:     strings.TrimSpace(item.Source),
		})
	}
	return out
}

func toTeachingElements(in deepSeekTeachingElements) model.TeachingElements {
	return model.TeachingElements{
		KnowledgePoints: in.KnowledgePoints,
		TeachingGoals:   in.TeachingGoals,
		KeyDifficulties: in.KeyDifficulties,
		TargetAudience:  strings.TrimSpace(in.TargetAudience),
		Duration:        strings.TrimSpace(in.Duration),
		OutputStyle:     strings.TrimSpace(in.OutputStyle),
	}
}
