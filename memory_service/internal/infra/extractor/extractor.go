package extractor

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"memory_service/internal/model"
)

type Result struct {
	Facts               []model.MemoryEntry
	Preferences         []model.MemoryEntry
	ConversationSummary string
	teachingElements    model.TeachingElements
<<<<<<< HEAD
=======
	taskStateSignals    model.TaskStateSignals
>>>>>>> origin/wang
}

func (r Result) TeachingElements() model.TeachingElements {
	return r.teachingElements
}

<<<<<<< HEAD
=======
func (r Result) TaskStateSignals() model.TaskStateSignals {
	return r.taskStateSignals
}

>>>>>>> origin/wang
type Extractor interface {
	Extract(userID string, sessionID string, messages []model.ConversationTurn) (Result, error)
}

type Config struct {
	EnableLLM bool
	LLMModel  string
	Timeout   time.Duration
	MaxTurns  int
}

type RuleBasedExtractor struct{}

type HybridExtractor struct {
	cfg       Config
	rules     RuleBasedExtractor
	llmClient LLMClient
}

type LLMClient interface {
	ExtractRequirementDialogue(ctx context.Context, req LLMRequest) (LLMResponse, error)
}

type LLMRequest struct {
	UserID    string
	SessionID string
	Messages  []model.ConversationTurn
}

type LLMResponse struct {
	Facts               []model.MemoryEntry
	Preferences         []model.MemoryEntry
	TeachingElements    model.TeachingElements
	ConversationSummary string
}

func NewHybridExtractor(cfg Config, client LLMClient) *HybridExtractor {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 2 * time.Second
	}
	if cfg.MaxTurns <= 0 {
		cfg.MaxTurns = 16
	}
	return &HybridExtractor{cfg: cfg, rules: RuleBasedExtractor{}, llmClient: client}
}

func (h *HybridExtractor) HasLLMClient() bool {
	return h != nil && h.llmClient != nil
}

func (h *HybridExtractor) LLMClientName() string {
	if h == nil || h.llmClient == nil {
		return ""
	}
	switch h.llmClient.(type) {
	case *DeepSeekClient:
		return "deepseek"
	default:
		return fmt.Sprintf("%T", h.llmClient)
	}
}

func (h *HybridExtractor) Extract(userID string, sessionID string, messages []model.ConversationTurn) (Result, error) {
	bounded := limitTurns(messages, h.cfg.MaxTurns)
	base, err := h.rules.Extract(userID, sessionID, bounded)
	if err != nil {
		return Result{}, err
	}
	if !h.cfg.EnableLLM || h.llmClient == nil {
		return base, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), h.cfg.Timeout)
	defer cancel()

	resp, err := h.llmClient.ExtractRequirementDialogue(ctx, LLMRequest{
		UserID:    userID,
		SessionID: sessionID,
		Messages:  bounded,
	})
	if err != nil {
		return base, nil
	}
	validated, err := validateLLMResponse(userID, resp)
	if err != nil {
		return base, nil
	}
	return mergeResults(base, validated), nil
}

func (RuleBasedExtractor) Extract(userID string, sessionID string, messages []model.ConversationTurn) (Result, error) {
	normalized := extractRequirementDialogue(userID, messages)
	if normalized.summary == "" && len(messages) > 0 {
		normalized.summary = buildFallbackSummary(messages)
	}
	return normalized.toResult(), nil
}

type normalizedExtraction struct {
	facts            []model.MemoryEntry
	preferences      []model.MemoryEntry
	teachingElements model.TeachingElements
<<<<<<< HEAD
=======
	taskStateSignals model.TaskStateSignals
>>>>>>> origin/wang
	summary          string
}

func (n normalizedExtraction) toResult() Result {
	return Result{
		Facts:               n.facts,
		Preferences:         n.preferences,
		ConversationSummary: n.summary,
		teachingElements:    n.teachingElements,
<<<<<<< HEAD
=======
		taskStateSignals:    n.taskStateSignals,
>>>>>>> origin/wang
	}
}

func validateLLMResponse(userID string, resp LLMResponse) (Result, error) {
	facts := sanitizeEntries(userID, "fact", resp.Facts)
	prefs := sanitizeEntries(userID, "preference", resp.Preferences)
	if len(resp.Facts) > 0 && len(facts) == 0 {
		return Result{}, errors.New("invalid facts")
	}
	if len(resp.Preferences) > 0 && len(prefs) == 0 {
		return Result{}, errors.New("invalid preferences")
	}
	elems := normalizeTeachingElements(resp.TeachingElements)
	summary := strings.TrimSpace(resp.ConversationSummary)
	return Result{
		Facts:               dedupeEntries(facts),
		Preferences:         dedupeEntries(prefs),
		ConversationSummary: summary,
		teachingElements:    elems,
<<<<<<< HEAD
=======
		taskStateSignals:    model.TaskStateSignals{},
>>>>>>> origin/wang
	}, nil
}

func mergeResults(base Result, llm Result) Result {
	out := base
	if len(llm.Facts) > 0 {
		out.Facts = dedupeEntries(append(out.Facts, llm.Facts...))
	}
	if len(llm.Preferences) > 0 {
		out.Preferences = dedupeEntries(append(out.Preferences, llm.Preferences...))
	}
	out.teachingElements = mergeTeachingElements(out.teachingElements, llm.teachingElements)
<<<<<<< HEAD
=======
	out.taskStateSignals = mergeTaskStateSignals(out.taskStateSignals, llm.taskStateSignals)
>>>>>>> origin/wang
	if strings.TrimSpace(llm.ConversationSummary) != "" {
		out.ConversationSummary = llm.ConversationSummary
	}
	return out
}

<<<<<<< HEAD
=======
func mergeTaskStateSignals(existing, incoming model.TaskStateSignals) model.TaskStateSignals {
	out := existing
	out.LessonTopic = chooseNonEmpty(out.LessonTopic, incoming.LessonTopic)
	out.KnowledgePoints = mergeStringLists(out.KnowledgePoints, incoming.KnowledgePoints)
	out.TeachingGoals = mergeStringLists(out.TeachingGoals, incoming.TeachingGoals)
	out.KeyDifficulties = mergeStringLists(out.KeyDifficulties, incoming.KeyDifficulties)
	out.TargetAudience = chooseNonEmpty(out.TargetAudience, incoming.TargetAudience)
	out.Duration = chooseNonEmpty(out.Duration, incoming.Duration)
	out.OutputStyle = chooseNonEmpty(out.OutputStyle, incoming.OutputStyle)
	out.TeachingLogic = chooseNonEmpty(out.TeachingLogic, incoming.TeachingLogic)
	out.Constraints = mergeStringLists(out.Constraints, incoming.Constraints)
	out.ReferenceMaterialUsage = mergeStringLists(out.ReferenceMaterialUsage, incoming.ReferenceMaterialUsage)
	if len(existing.Provenance) == 0 && len(incoming.Provenance) == 0 {
		return out
	}
	out.Provenance = map[string]string{}
	for k, v := range existing.Provenance {
		out.Provenance[k] = v
	}
	for k, v := range incoming.Provenance {
		if strings.TrimSpace(v) != "" {
			out.Provenance[k] = v
		}
	}
	return out
}

>>>>>>> origin/wang
func sanitizeEntries(userID, category string, in []model.MemoryEntry) []model.MemoryEntry {
	out := make([]model.MemoryEntry, 0, len(in))
	for _, entry := range in {
		key := strings.TrimSpace(entry.Key)
		value := strings.TrimSpace(entry.Value)
		if key == "" || value == "" {
			continue
		}
		conf := entry.Confidence
		if conf <= 0 || conf > 1 {
			conf = 0.8
		}
		source := strings.TrimSpace(entry.Source)
		if source != "explicit" && source != "inferred" {
			source = "inferred"
		}
		ctx := strings.TrimSpace(entry.Context)
		if ctx == "" {
			ctx = "general"
		}
		out = append(out, model.MemoryEntry{
			UserID:     userID,
			Category:   category,
			Key:        key,
			Value:      value,
			Context:    ctx,
			Confidence: conf,
			Source:     source,
		})
	}
	return out
}

func dedupeEntries(in []model.MemoryEntry) []model.MemoryEntry {
	if len(in) == 0 {
		return nil
	}
	sort.SliceStable(in, func(i, j int) bool {
		if in[i].Context != in[j].Context {
			return in[i].Context < in[j].Context
		}
		if in[i].Key != in[j].Key {
			return in[i].Key < in[j].Key
		}
		return in[i].Value < in[j].Value
	})
	seen := map[string]model.MemoryEntry{}
	for _, entry := range in {
		id := entry.Context + "|" + entry.Key
		existing, ok := seen[id]
		if !ok || entry.Confidence >= existing.Confidence {
			seen[id] = entry
		}
	}
	out := make([]model.MemoryEntry, 0, len(seen))
	for _, entry := range seen {
		out = append(out, entry)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Context != out[j].Context {
			return out[i].Context < out[j].Context
		}
		return out[i].Key < out[j].Key
	})
	return out
}

func limitTurns(messages []model.ConversationTurn, maxTurns int) []model.ConversationTurn {
	if maxTurns <= 0 || len(messages) <= maxTurns {
		return messages
	}
	return messages[len(messages)-maxTurns:]
}
