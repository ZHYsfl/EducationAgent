package service

import (
	"math"
	"sort"
	"strings"

	"memory_service/internal/model"
)

const (
	recallMinScore      = 2
	profileSummaryMax   = 320
	lowConfidencePrefix = "[Low Confidence] "
)

type QueryFeatures struct {
	Raw        string
	Normalized string
	Tokens     []string
}

type IntentHints struct {
	Continuation        bool
	RequirementGather   bool
	PreferenceFocus     bool
	CurrentLessonOrTask bool
}

type WorkingSignals struct {
	SessionID string
	Terms     map[string]struct{}
}

type ScoredCandidate struct {
	Entry  model.MemoryEntry
	Score  int
	Reason []string
}

type BudgetPlan struct {
	Total       int
	FactsTarget int
	PrefsTarget int
}

func normalizeQuery(query string) QueryFeatures {
	raw := strings.TrimSpace(query)
	normalized := strings.ToLower(strings.Join(strings.Fields(raw), " "))
	tokens := tokenizeText(normalized)
	return QueryFeatures{Raw: raw, Normalized: normalized, Tokens: tokens}
}

func detectIntentHints(q QueryFeatures, hasSession bool) IntentHints {
	text := q.Normalized
	return IntentHints{
		Continuation: hasSession && containsAny(text,
			"continue", "carry on", "as before", "same as", "pick up", "follow up", "resume"),
		RequirementGather: containsAny(text,
			"requirement", "collect", "missing", "knowledge point", "teaching goal", "target audience", "lesson prep", "ppt"),
		PreferenceFocus: containsAny(text,
			"prefer", "preference", "style", "color", "theme", "font", "layout", "content depth", "teaching style"),
		CurrentLessonOrTask: hasSession && containsAny(text,
			"this lesson", "current lesson", "this task", "current task", "current session", "for this class", "for this ppt"),
	}
}

func extractWorkingSignals(wm *model.WorkingMemory, sessionID string) WorkingSignals {
	out := WorkingSignals{SessionID: sessionID, Terms: map[string]struct{}{}}
	if wm == nil {
		return out
	}
	for _, v := range wm.ExtractedElements.KnowledgePoints {
		addSignalTerms(out.Terms, v)
	}
	for _, v := range wm.ExtractedElements.TeachingGoals {
		addSignalTerms(out.Terms, v)
	}
	for _, v := range wm.ExtractedElements.KeyDifficulties {
		addSignalTerms(out.Terms, v)
	}
	addSignalTerms(out.Terms, wm.ExtractedElements.TargetAudience)
	addSignalTerms(out.Terms, wm.ExtractedElements.Duration)
	addSignalTerms(out.Terms, wm.ExtractedElements.OutputStyle)
	for _, v := range wm.RecentTopics {
		addSignalTerms(out.Terms, v)
	}
	return out
}

func applyPreferenceDecay(entry model.MemoryEntry, nowMs int64) model.MemoryEntry {
	if entry.Confidence <= 0 {
		entry.Confidence = 0
		return entry
	}
	days := float64(nowMs-entry.UpdatedAt) / float64(24*60*60*1000)
	if days >= 30 {
		steps := math.Floor(days / 30)
		entry.Confidence = entry.Confidence * math.Pow(0.9, steps)
	}
	if entry.Confidence < 0.3 && !strings.HasPrefix(entry.Value, lowConfidencePrefix) {
		entry.Value = lowConfidencePrefix + entry.Value
	}
	return entry
}

func scoreCandidate(entry model.MemoryEntry, category string, q QueryFeatures, hints IntentHints, ws WorkingSignals, nowMs int64) ScoredCandidate {
	key := strings.ToLower(strings.TrimSpace(entry.Key))
	value := strings.ToLower(strings.TrimSpace(entry.Value))
	ctx := strings.ToLower(strings.TrimSpace(entry.Context))
	score := 0
	reasons := make([]string, 0, 8)
	baseSignal := 0

	if q.Normalized != "" {
		if strings.Contains(key, q.Normalized) {
			score += 12
			baseSignal += 12
			reasons = append(reasons, "key_phrase")
		}
		if strings.Contains(value, q.Normalized) {
			score += 6
			baseSignal += 6
			reasons = append(reasons, "value_phrase")
		}
	}

	for _, t := range q.Tokens {
		if strings.Contains(key, t) {
			score += 4
			baseSignal += 4
			reasons = append(reasons, "key_token")
		}
		if strings.Contains(value, t) {
			score += 2
			baseSignal += 2
			reasons = append(reasons, "value_token")
		}
		if strings.Contains(ctx, t) {
			score += 1
			baseSignal += 1
			reasons = append(reasons, "context_token")
		}
	}

	overlap := countSignalOverlap(key+" "+value, ws.Terms)
	if overlap > 0 {
		boost := overlap * 2
		if boost > 6 {
			boost = 6
		}
		score += boost
		baseSignal += boost
		reasons = append(reasons, "working_signal")
	}

	if ws.SessionID != "" && entry.SourceSessionID != nil && strings.TrimSpace(*entry.SourceSessionID) == ws.SessionID {
		if hints.Continuation || hints.CurrentLessonOrTask {
			score += 3
			baseSignal += 3
			reasons = append(reasons, "session_source")
		} else {
			score += 1
			baseSignal += 1
			reasons = append(reasons, "session_source_light")
		}
	}

	if category == "preference" {
		if entry.Confidence < 0.3 {
			score -= 2
			reasons = append(reasons, "low_confidence")
		} else if entry.Confidence >= 0.8 {
			score += 1
			reasons = append(reasons, "high_confidence")
		}
	}

	if baseSignal > 0 && category == "preference" && hints.PreferenceFocus {
		score += 4
		reasons = append(reasons, "intent_preference")
	}
	if baseSignal > 0 && category == "fact" && (hints.RequirementGather || hints.Continuation || hints.CurrentLessonOrTask) {
		score += 3
		reasons = append(reasons, "intent_requirement")
	}

	if baseSignal > 0 && entry.UpdatedAt > 0 && nowMs > entry.UpdatedAt {
		days := (nowMs - entry.UpdatedAt) / (24 * 60 * 60 * 1000)
		switch {
		case days <= 7:
			score += 2
			reasons = append(reasons, "recent_7d")
		case days <= 30:
			score += 1
			reasons = append(reasons, "recent_30d")
		}
	}

	return ScoredCandidate{Entry: entry, Score: score, Reason: reasons}
}

func filterByMinScore(items []ScoredCandidate, minScore int) []ScoredCandidate {
	if minScore <= 0 {
		return items
	}
	out := make([]ScoredCandidate, 0, len(items))
	for _, item := range items {
		if item.Score >= minScore {
			out = append(out, item)
		}
	}
	return out
}

func rankCandidates(cands []ScoredCandidate) []ScoredCandidate {
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].Score == cands[j].Score {
			if cands[i].Entry.UpdatedAt == cands[j].Entry.UpdatedAt {
				if cands[i].Entry.Key == cands[j].Entry.Key {
					return cands[i].Entry.Value < cands[j].Entry.Value
				}
				return cands[i].Entry.Key < cands[j].Entry.Key
			}
			return cands[i].Entry.UpdatedAt > cands[j].Entry.UpdatedAt
		}
		return cands[i].Score > cands[j].Score
	})
	return cands
}

func buildBudget(topK int, hints IntentHints, factN int, prefN int) BudgetPlan {
	if topK <= 0 {
		topK = 5
	}
	factsTarget := (topK + 1) / 2
	prefsTarget := topK - factsTarget

	if hints.PreferenceFocus && !hints.RequirementGather {
		prefsTarget = int(math.Ceil(float64(topK) * 0.7))
		factsTarget = topK - prefsTarget
	}
	if hints.RequirementGather || hints.Continuation || hints.CurrentLessonOrTask {
		factsTarget = int(math.Ceil(float64(topK) * 0.65))
		prefsTarget = topK - factsTarget
	}

	if topK > 1 && factN > 0 && prefN > 0 {
		if factsTarget == 0 {
			factsTarget = 1
			prefsTarget = topK - factsTarget
		}
		if prefsTarget == 0 {
			prefsTarget = 1
			factsTarget = topK - prefsTarget
		}
	}

	if factN == 0 {
		factsTarget = 0
		prefsTarget = topK
	}
	if prefN == 0 {
		prefsTarget = 0
		factsTarget = topK
	}
	if factsTarget > factN {
		factsTarget = factN
	}
	if prefsTarget > prefN {
		prefsTarget = prefN
	}
	remaining := topK - factsTarget - prefsTarget
	for remaining > 0 {
		moved := false
		if factN > factsTarget {
			factsTarget++
			remaining--
			moved = true
		}
		if remaining == 0 {
			break
		}
		if prefN > prefsTarget {
			prefsTarget++
			remaining--
			moved = true
		}
		if !moved {
			break
		}
	}
	return BudgetPlan{Total: topK, FactsTarget: factsTarget, PrefsTarget: prefsTarget}
}

func selectWithBudget(facts []ScoredCandidate, prefs []ScoredCandidate, plan BudgetPlan) ([]model.MemoryEntry, []model.MemoryEntry) {
	factsOut := make([]model.MemoryEntry, 0, plan.FactsTarget)
	prefsOut := make([]model.MemoryEntry, 0, plan.PrefsTarget)

	for i := 0; i < len(facts) && i < plan.FactsTarget; i++ {
		factsOut = append(factsOut, facts[i].Entry)
	}
	for i := 0; i < len(prefs) && i < plan.PrefsTarget; i++ {
		prefsOut = append(prefsOut, prefs[i].Entry)
	}
	return factsOut, prefsOut
}

func composeProfileSummary(p UserProfile, wm *model.WorkingMemory, hints IntentHints) string {
	parts := make([]string, 0, 6)
	if strings.TrimSpace(p.Subject) != "" {
		parts = append(parts, "Subject: "+strings.TrimSpace(p.Subject))
	}
	if strings.TrimSpace(p.TeachingStyle) != "" {
		parts = append(parts, "Teaching style: "+strings.TrimSpace(p.TeachingStyle))
	}
	if strings.TrimSpace(p.ContentDepth) != "" {
		parts = append(parts, "Content depth: "+strings.TrimSpace(p.ContentDepth))
	}
	parts = append(parts, summarizePreferences(p)...)
	if strings.TrimSpace(p.HistorySummary) != "" {
		parts = append(parts, "History: "+truncateText(strings.TrimSpace(p.HistorySummary), 120))
	}
	if wm != nil && (hints.Continuation || hints.CurrentLessonOrTask) {
		if addon := summarizeSessionSnapshot(wm); addon != "" {
			parts = append(parts, addon)
		}
	}
	return truncateText(strings.Join(parts, " | "), profileSummaryMax)
}

func summarizePreferences(p UserProfile) []string {
	parts := make([]string, 0, 2)
	visualKeys := sortedMapKeys(p.VisualPreferences)
	if len(visualKeys) > 0 {
		k := visualKeys[0]
		parts = append(parts, "Visual preference: "+k+"="+strings.TrimSpace(p.VisualPreferences[k]))
	}
	generalKeys := sortedMapKeys(p.Preferences)
	if len(generalKeys) > 0 {
		k := generalKeys[0]
		parts = append(parts, "Preference: "+k+"="+strings.TrimSpace(p.Preferences[k]))
	}
	return parts
}

func summarizeSessionSnapshot(wm *model.WorkingMemory) string {
	parts := make([]string, 0, 5)
	if strings.TrimSpace(wm.ExtractedElements.TargetAudience) != "" {
		parts = append(parts, "audience="+strings.TrimSpace(wm.ExtractedElements.TargetAudience))
	}
	if strings.TrimSpace(wm.ExtractedElements.Duration) != "" {
		parts = append(parts, "duration="+strings.TrimSpace(wm.ExtractedElements.Duration))
	}
	if len(wm.ExtractedElements.KnowledgePoints) > 0 {
		parts = append(parts, "focus="+strings.TrimSpace(wm.ExtractedElements.KnowledgePoints[0]))
	}
	if len(wm.ExtractedElements.TeachingGoals) > 0 {
		parts = append(parts, "goal="+strings.TrimSpace(wm.ExtractedElements.TeachingGoals[0]))
	}
	if len(wm.RecentTopics) > 0 {
		parts = append(parts, "recent="+strings.TrimSpace(wm.RecentTopics[0]))
	}
	if len(parts) == 0 {
		return ""
	}
	return "Current session: " + strings.Join(parts, "; ")
}

func tokenizeText(in string) []string {
	if strings.TrimSpace(in) == "" {
		return nil
	}
	stopWords := map[string]struct{}{
		"the": {}, "a": {}, "an": {}, "to": {}, "for": {}, "of": {}, "and": {}, "or": {}, "is": {}, "are": {}, "with": {},
		"my": {}, "me": {}, "it": {}, "this": {}, "that": {}, "what": {}, "how": {}, "about": {}, "please": {},
	}
	fields := strings.Fields(in)
	out := make([]string, 0, len(fields))
	seen := map[string]struct{}{}
	for _, f := range fields {
		f = strings.TrimSpace(strings.Trim(f, ".,!?;:\"'()[]{}"))
		if f == "" {
			continue
		}
		if _, ok := stopWords[f]; ok {
			continue
		}
		if _, ok := seen[f]; ok {
			continue
		}
		seen[f] = struct{}{}
		out = append(out, f)
	}
	return out
}

func addSignalTerms(terms map[string]struct{}, text string) {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return
	}
	terms[text] = struct{}{}
	for _, token := range tokenizeText(text) {
		terms[token] = struct{}{}
	}
}

func countSignalOverlap(text string, terms map[string]struct{}) int {
	if len(terms) == 0 || strings.TrimSpace(text) == "" {
		return 0
	}
	lower := strings.ToLower(text)
	count := 0
	for term := range terms {
		if term == "" {
			continue
		}
		if strings.Contains(lower, term) {
			count++
		}
	}
	return count
}

func containsAny(text string, phrases ...string) bool {
	for _, phrase := range phrases {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	return false
}

func sortedMapKeys(m map[string]string) []string {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k, v := range m {
		if strings.TrimSpace(k) == "" || strings.TrimSpace(v) == "" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func truncateText(s string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(strings.TrimSpace(s))
	if len(runes) <= max {
		return string(runes)
	}
	if max <= 3 {
		return string(runes[:max])
	}
	return string(runes[:max-3]) + "..."
}
