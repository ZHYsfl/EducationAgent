package extractor

import (
	"strings"

	"auth_memory_service/internal/model"
)

type Result struct {
	Facts               []model.MemoryEntry
	Preferences         []model.MemoryEntry
	ConversationSummary string
}

type Extractor interface {
	Extract(userID string, sessionID string, messages []model.ConversationTurn) (Result, error)
}

type RuleBasedExtractor struct{}

func (RuleBasedExtractor) Extract(userID string, sessionID string, messages []model.ConversationTurn) (Result, error) {
	facts := make([]model.MemoryEntry, 0)
	prefs := make([]model.MemoryEntry, 0)
	lastUser := ""
	for _, m := range messages {
		if strings.ToLower(strings.TrimSpace(m.Role)) != "user" {
			continue
		}
		text := strings.TrimSpace(m.Content)
		if text == "" {
			continue
		}
		lastUser = text
		lower := strings.ToLower(text)
		if strings.Contains(lower, "i teach") || strings.Contains(lower, "subject") {
			facts = append(facts, model.MemoryEntry{UserID: userID, Category: "fact", Key: "subject", Value: text, Context: "general", Confidence: 1.0, Source: "inferred"})
		}
		if strings.Contains(lower, "prefer") || strings.Contains(lower, "like") {
			prefs = append(prefs, model.MemoryEntry{UserID: userID, Category: "preference", Key: "teaching_style", Value: text, Context: "general", Confidence: 0.8, Source: "inferred"})
		}
	}
	if len(facts) == 0 && lastUser != "" {
		facts = append(facts, model.MemoryEntry{UserID: userID, Category: "fact", Key: "recent_fact", Value: lastUser, Context: "general", Confidence: 0.6, Source: "inferred"})
	}
	summary := ""
	if len(messages) > 0 {
		chunks := make([]string, 0, 2)
		for i := len(messages) - 1; i >= 0 && len(chunks) < 2; i-- {
			if strings.TrimSpace(messages[i].Content) != "" {
				chunks = append([]string{strings.TrimSpace(messages[i].Content)}, chunks...)
			}
		}
		summary = strings.Join(chunks, " ")
	}
	return Result{Facts: facts, Preferences: prefs, ConversationSummary: summary}, nil
}
