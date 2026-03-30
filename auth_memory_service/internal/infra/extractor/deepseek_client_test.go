package extractor

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"auth_memory_service/internal/model"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestDeepSeekClientCallsChatCompletionsAndParsesResponse(t *testing.T) {
	client, err := NewDeepSeekClient("test-key", "https://api.deepseek.com", DefaultDeepSeekModel, time.Second)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	client.httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("unexpected auth header: %q", got)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body: io.NopCloser(strings.NewReader(`{
			"choices": [{
				"message": {
					"content": "{\"facts\":[{\"key\":\"subject\",\"value\":\"mathematics\",\"context\":\"general\",\"confidence\":0.95,\"source\":\"explicit\"}],\"preferences\":[{\"key\":\"output_style\",\"value\":\"minimalist blue slides\",\"context\":\"visual_preferences\",\"confidence\":0.9,\"source\":\"explicit\"}],\"teaching_elements\":{\"knowledge_points\":[\"limits\",\"derivatives\"],\"teaching_goals\":[\"help students explain limit intuition\"],\"key_difficulties\":[\"epsilon-delta intuition\"],\"target_audience\":\"first-year calculus students\",\"duration\":\"45 minutes\",\"output_style\":\"minimalist blue slides\"},\"conversation_summary\":\"Requirement collection progress: knowledge_points, teaching_goals, target_audience\"}"
				},
				"finish_reason": "stop"
			}]
		}`)),
			Request: r,
		}, nil
	})
	resp, err := client.ExtractRequirementDialogue(context.Background(), LLMRequest{
		UserID:    "user_u1",
		SessionID: "sess_1",
		Messages: []model.ConversationTurn{
			{Role: "user", Content: "The lesson covers limits and derivatives for first-year calculus students."},
		},
	})
	if err != nil {
		t.Fatalf("extract dialogue: %v", err)
	}
	if len(resp.Facts) != 1 || resp.Facts[0].Key != "subject" {
		t.Fatalf("unexpected facts: %#v", resp.Facts)
	}
	if len(resp.TeachingElements.KnowledgePoints) != 2 {
		t.Fatalf("unexpected teaching elements: %#v", resp.TeachingElements)
	}
	if !strings.Contains(resp.ConversationSummary, "knowledge_points") {
		t.Fatalf("unexpected summary: %q", resp.ConversationSummary)
	}
}

func TestDeepSeekClientTimeoutReturnsError(t *testing.T) {
	client, err := NewDeepSeekClient("test-key", "https://api.deepseek.com", DefaultDeepSeekModel, 5*time.Millisecond)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	client.httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		select {
		case <-time.After(50 * time.Millisecond):
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"{}"}}]}`)),
				Request:    r,
			}, nil
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
	})
	_, err = client.ExtractRequirementDialogue(context.Background(), LLMRequest{
		UserID:    "user_u1",
		SessionID: "sess_1",
		Messages: []model.ConversationTurn{
			{Role: "user", Content: "Need a lesson on limits."},
		},
	})
	if err == nil {
		t.Fatalf("expected timeout error")
	}
}

func TestDeepSeekClientReturnsErrorOnAuthFailure(t *testing.T) {
	client, err := NewDeepSeekClient("bad-key", "https://api.deepseek.com", DefaultDeepSeekModel, time.Second)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	client.httpClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusUnauthorized,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"invalid api key"}}`)),
			Request:    r,
		}, nil
	})
	_, err = client.ExtractRequirementDialogue(context.Background(), LLMRequest{
		UserID:    "user_u1",
		SessionID: "sess_1",
		Messages: []model.ConversationTurn{
			{Role: "user", Content: "Need a lesson on limits."},
		},
	})
	if err == nil {
		t.Fatalf("expected auth failure")
	}
}
