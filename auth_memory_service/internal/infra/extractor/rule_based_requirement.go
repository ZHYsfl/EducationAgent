package extractor

import (
	"regexp"
	"strings"

	"auth_memory_service/internal/model"
)

var (
	reDuration = regexp.MustCompile(`(?i)\b(\d+\s*(minute|minutes|min|hour|hours))\b`)
)

func extractRequirementDialogue(userID string, messages []model.ConversationTurn) normalizedExtraction {
	facts := make([]model.MemoryEntry, 0, 4)
	preferences := make([]model.MemoryEntry, 0, 4)
	elems := model.TeachingElements{}
	collected := make([]string, 0, 8)
	summaryNotes := make([]string, 0, 8)

	for _, msg := range messages {
		if strings.ToLower(strings.TrimSpace(msg.Role)) != "user" {
			continue
		}
		text := normalizeWhitespace(msg.Content)
		if text == "" {
			continue
		}
		lower := strings.ToLower(text)

		if subject := detectTeacherSubject(lower, text); subject != "" {
			facts = append(facts, makeFact(userID, "subject", subject, "general", 0.95))
		}
		if school := detectSchool(text); school != "" {
			facts = append(facts, makeFact(userID, "school_context", school, "general", 0.9))
		}
		if pref := detectVisualPreference(lower, text); pref != "" {
			preferences = append(preferences, makePreference(userID, "output_style", pref, "visual_preferences", 0.9))
		}
		if pref := detectTeachingStyle(lower, text); pref != "" {
			preferences = append(preferences, makePreference(userID, "teaching_style", pref, "general", 0.85))
		}

		if topic := detectTopic(text); topic != "" {
			summaryNotes = append(summaryNotes, "topic: "+topic)
			collected = append(collected, "topic")
		}
		if points := detectListValue(lower, text, []string{"knowledge points", "knowledge point", "cover", "include", "focus on"}); len(points) > 0 {
			elems.KnowledgePoints = mergeStringLists(elems.KnowledgePoints, points)
			collected = append(collected, "knowledge_points")
		}
		if goals := detectListValue(lower, text, []string{"teaching goal", "teaching goals", "goal is", "goals are", "students should"}); len(goals) > 0 {
			elems.TeachingGoals = mergeStringLists(elems.TeachingGoals, goals)
			collected = append(collected, "teaching_goals")
		}
		if difficulties := detectListValue(lower, text, []string{"difficult", "difficulty", "key difficulty", "challenging"}); len(difficulties) > 0 {
			elems.KeyDifficulties = mergeStringLists(elems.KeyDifficulties, difficulties)
			collected = append(collected, "key_difficulties")
		}
		if audience := detectAudience(lower, text); audience != "" {
			elems.TargetAudience = audience
			collected = append(collected, "target_audience")
		}
		if duration := detectDuration(text); duration != "" {
			elems.Duration = duration
			collected = append(collected, "duration")
		}
		if style := detectOutputStyle(lower, text); style != "" {
			elems.OutputStyle = style
			collected = append(collected, "output_style")
		}

		if logic := detectPlanningCue(lower, text, []string{"teach", "first", "then", "finally", "teaching logic", "sequence"}); logic != "" {
			summaryNotes = append(summaryNotes, "teaching logic: "+logic)
		}
		if refs := detectPlanningCue(lower, text, []string{"reference", "pdf", "ppt", "lesson plan", "textbook"}); refs != "" {
			summaryNotes = append(summaryNotes, "reference usage: "+refs)
		}
		if interactions := detectPlanningCue(lower, text, []string{"interaction", "quiz", "game", "animation"}); interactions != "" {
			summaryNotes = append(summaryNotes, "interaction: "+interactions)
		}
	}

	return normalizedExtraction{
		facts:            dedupeEntries(facts),
		preferences:      dedupeEntries(preferences),
		teachingElements: normalizeTeachingElements(elems),
		summary:          buildRequirementSummary(messages, dedupeStrings(collected), dedupeStrings(summaryNotes), normalizeTeachingElements(elems)),
	}
}

func makeFact(userID, key, value, ctx string, confidence float64) model.MemoryEntry {
	return model.MemoryEntry{
		UserID:     userID,
		Category:   "fact",
		Key:        key,
		Value:      value,
		Context:    ctx,
		Confidence: confidence,
		Source:     "inferred",
	}
}

func makePreference(userID, key, value, ctx string, confidence float64) model.MemoryEntry {
	return model.MemoryEntry{
		UserID:     userID,
		Category:   "preference",
		Key:        key,
		Value:      value,
		Context:    ctx,
		Confidence: confidence,
		Source:     "inferred",
	}
}

func normalizeWhitespace(text string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
}

func detectTeacherSubject(lower, original string) string {
	patterns := []string{
		"i teach ", "i'm teaching ", "i am teaching ", "my subject is ", "for my ",
	}
	for _, pattern := range patterns {
		idx := strings.Index(lower, pattern)
		if idx >= 0 {
			value := strings.TrimSpace(original[idx+len(pattern):])
			return trimSentence(value)
		}
	}
	return ""
}

func detectSchool(text string) string {
	lower := strings.ToLower(text)
	for _, marker := range []string{"school", "college", "university"} {
		if strings.Contains(lower, marker) {
			return trimSentence(text)
		}
	}
	return ""
}

func detectVisualPreference(lower, text string) string {
	if !(strings.Contains(lower, "prefer") || strings.Contains(lower, "like")) {
		return ""
	}
	for _, marker := range []string{"minimal", "clean", "simple", "academic", "visual", "style", "ppt"} {
		if strings.Contains(lower, marker) {
			return trimSentence(text)
		}
	}
	return ""
}

func detectTeachingStyle(lower, text string) string {
	if strings.Contains(lower, "interactive") || strings.Contains(lower, "rigorous") || strings.Contains(lower, "narrative") {
		return trimSentence(text)
	}
	return ""
}

func detectTopic(text string) string {
	lower := strings.ToLower(text)
	for _, marker := range []string{"topic is", "topic:", "lesson on", "ppt on", "courseware on"} {
		if idx := strings.Index(lower, marker); idx >= 0 {
			return trimSentence(text[idx+len(marker):])
		}
	}
	return ""
}

func detectListValue(lower, text string, markers []string) []string {
	for _, marker := range markers {
		if idx := strings.Index(lower, marker); idx >= 0 {
			value := trimSentence(text[idx+len(marker):])
			return splitList(value)
		}
	}
	return nil
}

func detectAudience(lower, text string) string {
	for _, marker := range []string{"audience", "for ", "students", "grade", "freshmen", "beginners"} {
		if strings.Contains(lower, marker) {
			return trimSentence(text)
		}
	}
	return ""
}

func detectDuration(text string) string {
	match := reDuration.FindString(text)
	return strings.TrimSpace(match)
}

func detectOutputStyle(lower, text string) string {
	for _, marker := range []string{"style", "minimalist", "minimal", "academic", "tech style", "presentation style"} {
		if strings.Contains(lower, marker) {
			return trimSentence(text)
		}
	}
	return ""
}

func detectPlanningCue(lower, text string, markers []string) string {
	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			return trimSentence(text)
		}
	}
	return ""
}

func trimSentence(text string) string {
	text = strings.TrimSpace(text)
	text = strings.Trim(text, " .,:;")
	if idx := strings.IndexAny(text, ".!?"); idx >= 0 {
		text = text[:idx]
	}
	return strings.TrimSpace(text)
}

func splitList(value string) []string {
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ';' || r == '|' || r == '/'
	})
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(strings.TrimPrefix(part, "and "))
		part = strings.TrimSpace(strings.TrimPrefix(part, "to "))
		if part != "" {
			out = append(out, part)
		}
	}
	if len(out) == 0 && strings.TrimSpace(value) != "" {
		return []string{strings.TrimSpace(value)}
	}
	return dedupeStrings(out)
}

func mergeStringLists(existing, incoming []string) []string {
	return dedupeStrings(append(existing, incoming...))
}

func dedupeStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, item := range in {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		key := strings.ToLower(item)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	return out
}

func normalizeTeachingElements(in model.TeachingElements) model.TeachingElements {
	return model.TeachingElements{
		KnowledgePoints: dedupeStrings(in.KnowledgePoints),
		TeachingGoals:   dedupeStrings(in.TeachingGoals),
		KeyDifficulties: dedupeStrings(in.KeyDifficulties),
		TargetAudience:  strings.TrimSpace(in.TargetAudience),
		Duration:        strings.TrimSpace(in.Duration),
		OutputStyle:     strings.TrimSpace(in.OutputStyle),
	}
}

func mergeTeachingElements(existing, incoming model.TeachingElements) model.TeachingElements {
	return model.TeachingElements{
		KnowledgePoints: mergeStringLists(existing.KnowledgePoints, incoming.KnowledgePoints),
		TeachingGoals:   mergeStringLists(existing.TeachingGoals, incoming.TeachingGoals),
		KeyDifficulties: mergeStringLists(existing.KeyDifficulties, incoming.KeyDifficulties),
		TargetAudience:  chooseNonEmpty(existing.TargetAudience, incoming.TargetAudience),
		Duration:        chooseNonEmpty(existing.Duration, incoming.Duration),
		OutputStyle:     chooseNonEmpty(existing.OutputStyle, incoming.OutputStyle),
	}
}

func chooseNonEmpty(existing, incoming string) string {
	if strings.TrimSpace(incoming) != "" {
		return strings.TrimSpace(incoming)
	}
	return strings.TrimSpace(existing)
}

func buildRequirementSummary(messages []model.ConversationTurn, collected []string, notes []string, elems model.TeachingElements) string {
	parts := make([]string, 0, 6)
	if len(collected) > 0 {
		parts = append(parts, "Requirement collection progress: "+strings.Join(collected, ", "))
	}
	elementNotes := make([]string, 0, 6)
	if len(elems.KnowledgePoints) > 0 {
		elementNotes = append(elementNotes, "knowledge points="+strings.Join(elems.KnowledgePoints, ", "))
	}
	if len(elems.TeachingGoals) > 0 {
		elementNotes = append(elementNotes, "teaching goals="+strings.Join(elems.TeachingGoals, "; "))
	}
	if len(elems.KeyDifficulties) > 0 {
		elementNotes = append(elementNotes, "key difficulties="+strings.Join(elems.KeyDifficulties, ", "))
	}
	if elems.TargetAudience != "" {
		elementNotes = append(elementNotes, "target audience="+elems.TargetAudience)
	}
	if elems.Duration != "" {
		elementNotes = append(elementNotes, "duration="+elems.Duration)
	}
	if elems.OutputStyle != "" {
		elementNotes = append(elementNotes, "output style="+elems.OutputStyle)
	}
	if len(elementNotes) > 0 {
		parts = append(parts, "Teaching elements: "+strings.Join(elementNotes, "; "))
	}
	if len(notes) > 0 {
		parts = append(parts, "Planning notes: "+strings.Join(notes, "; "))
	}
	fallback := buildFallbackSummary(messages)
	if fallback != "" {
		parts = append(parts, "Recent dialogue: "+fallback)
	}
	return strings.TrimSpace(strings.Join(parts, " | "))
}

func buildFallbackSummary(messages []model.ConversationTurn) string {
	chunks := make([]string, 0, 3)
	for i := len(messages) - 1; i >= 0 && len(chunks) < 3; i-- {
		content := strings.TrimSpace(messages[i].Content)
		if content == "" {
			continue
		}
		chunks = append([]string{normalizeWhitespace(content)}, chunks...)
	}
	return strings.Join(chunks, " ")
}
