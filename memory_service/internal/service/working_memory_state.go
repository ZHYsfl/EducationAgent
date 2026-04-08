package service

import (
	"strings"

	"memory_service/internal/model"
)

const maxWorkingSummaryLength = 320

func projectWorkingMemoryRecord(record model.WorkingMemoryRecord) model.WorkingMemory {
	return model.WorkingMemory{
		SessionID:           record.SessionID,
		UserID:              record.UserID,
		ConversationSummary: buildProjectedWorkingSummary(record.TaskState, record.ConversationSummary),
		ExtractedElements:   projectTeachingElements(record.TaskState),
		RecentTopics:        projectRecentTopics(record),
		UpdatedAt:           record.UpdatedAt,
	}
}

func projectTeachingElements(state model.WorkingTaskState) model.TeachingElements {
	return model.TeachingElements{
		KnowledgePoints: append([]string{}, state.KnowledgePoints...),
		TeachingGoals:   append([]string{}, state.TeachingGoals...),
		KeyDifficulties: append([]string{}, state.KeyDifficulties...),
		TargetAudience:  strings.TrimSpace(state.TargetAudience),
		Duration:        strings.TrimSpace(state.Duration),
		OutputStyle:     strings.TrimSpace(state.OutputStyle),
	}
}

func mergeWorkingMemoryRecord(existing *model.WorkingMemoryRecord, sessionID, userID string, signals model.TaskStateSignals, summary string, recentTopics []string, nowMs int64) model.WorkingMemoryRecord {
	record := model.WorkingMemoryRecord{
		SessionID:  strings.TrimSpace(sessionID),
		UserID:     strings.TrimSpace(userID),
		Continuity: model.ContinuityActive,
		UpdatedAt:  nowMs,
	}
	if existing != nil {
		record = *existing
		record.SessionID = strings.TrimSpace(sessionID)
		record.UserID = strings.TrimSpace(userID)
		record.Continuity = model.ContinuityActive
		record.UpdatedAt = nowMs
	}

	provenance := currentSlotProvenance(record)
	record.TaskState.LessonTopic = chooseTaskValue(record.TaskState.LessonTopic, signals.LessonTopic)
	record.TaskState.KnowledgePoints = mergeTaskList(record.TaskState.KnowledgePoints, signals.KnowledgePoints)
	record.TaskState.TeachingGoals = mergeTaskList(record.TaskState.TeachingGoals, signals.TeachingGoals)
	record.TaskState.KeyDifficulties = mergeTaskList(record.TaskState.KeyDifficulties, signals.KeyDifficulties)
	record.TaskState.TargetAudience = chooseTaskValue(record.TaskState.TargetAudience, signals.TargetAudience)
	record.TaskState.Duration = chooseTaskValue(record.TaskState.Duration, signals.Duration)
	record.TaskState.OutputStyle = chooseTaskValue(record.TaskState.OutputStyle, signals.OutputStyle)
	record.TaskState.TeachingLogic = chooseTaskValue(record.TaskState.TeachingLogic, signals.TeachingLogic)
	record.TaskState.Constraints = mergeTaskList(record.TaskState.Constraints, signals.Constraints)
	record.TaskState.ReferenceMaterialUsage = mergeTaskList(record.TaskState.ReferenceMaterialUsage, signals.ReferenceMaterialUsage)

	applyProvenance(provenance, signals)
	record.RecentTopics = mergeRecentTopics(record.RecentTopics, recentTopics, record.TaskState)
	record.ExtractedElements = projectTeachingElements(record.TaskState)
	record.ConversationSummary = buildWorkingSummary(record.TaskState, summary)
	record.SlotMetadata = recomputeSlotMetadata(record.TaskState, provenance, nowMs)
	return record
}

func signalsFromTeachingElements(elems model.TeachingElements, provenance string) model.TaskStateSignals {
	if strings.TrimSpace(provenance) == "" {
		provenance = model.SlotProvenanceDerived
	}
	return model.TaskStateSignals{
		KnowledgePoints: elems.KnowledgePoints,
		TeachingGoals:   elems.TeachingGoals,
		KeyDifficulties: elems.KeyDifficulties,
		TargetAudience:  elems.TargetAudience,
		Duration:        elems.Duration,
		OutputStyle:     elems.OutputStyle,
		Provenance: map[string]string{
			model.TaskSlotKnowledgePoints: provenance,
			model.TaskSlotTeachingGoals:   provenance,
			model.TaskSlotKeyDifficulties: provenance,
			model.TaskSlotTargetAudience:  provenance,
			model.TaskSlotDuration:        provenance,
			model.TaskSlotOutputStyle:     provenance,
		},
	}
}

func mergeTaskSignals(base, incoming model.TaskStateSignals) model.TaskStateSignals {
	out := base
	out.LessonTopic = chooseTaskValue(out.LessonTopic, incoming.LessonTopic)
	out.KnowledgePoints = mergeTaskList(out.KnowledgePoints, incoming.KnowledgePoints)
	out.TeachingGoals = mergeTaskList(out.TeachingGoals, incoming.TeachingGoals)
	out.KeyDifficulties = mergeTaskList(out.KeyDifficulties, incoming.KeyDifficulties)
	out.TargetAudience = chooseTaskValue(out.TargetAudience, incoming.TargetAudience)
	out.Duration = chooseTaskValue(out.Duration, incoming.Duration)
	out.OutputStyle = chooseTaskValue(out.OutputStyle, incoming.OutputStyle)
	out.TeachingLogic = chooseTaskValue(out.TeachingLogic, incoming.TeachingLogic)
	out.Constraints = mergeTaskList(out.Constraints, incoming.Constraints)
	out.ReferenceMaterialUsage = mergeTaskList(out.ReferenceMaterialUsage, incoming.ReferenceMaterialUsage)
	if len(base.Provenance) == 0 && len(incoming.Provenance) == 0 {
		return out
	}
	out.Provenance = map[string]string{}
	for k, v := range base.Provenance {
		out.Provenance[k] = v
	}
	for k, v := range incoming.Provenance {
		if strings.TrimSpace(v) != "" {
			out.Provenance[k] = v
		}
	}
	return out
}

func buildWorkingSummary(state model.WorkingTaskState, fallback string) string {
	parts := make([]string, 0, 6)
	if strings.TrimSpace(state.LessonTopic) != "" {
		parts = append(parts, "lesson topic="+strings.TrimSpace(state.LessonTopic))
	}
	if strings.TrimSpace(state.TeachingLogic) != "" {
		parts = append(parts, "teaching logic="+strings.TrimSpace(state.TeachingLogic))
	}
	if len(state.Constraints) > 0 {
		parts = append(parts, "constraints="+strings.Join(state.Constraints, ", "))
	}
	if len(state.ReferenceMaterialUsage) > 0 {
		parts = append(parts, "reference usage="+strings.Join(state.ReferenceMaterialUsage, ", "))
	}
	summary := strings.Join(parts, " | ")
	fallback = strings.TrimSpace(fallback)
	if summary == "" {
		summary = fallback
	} else if fallback != "" {
		lowerFallback := strings.ToLower(fallback)
		if strings.Contains(lowerFallback, "interaction") || strings.Contains(lowerFallback, "互动") || strings.Contains(lowerFallback, "unresolved") || strings.Contains(lowerFallback, "if possible") {
			addon := extractSummaryAddon(fallback, lowerFallback)
			if addon != "" {
				summary = summary + " | " + addon
			}
		}
	}
	if len([]rune(summary)) > maxWorkingSummaryLength {
		return string([]rune(summary)[:maxWorkingSummaryLength])
	}
	return summary
}

func buildProjectedWorkingSummary(state model.WorkingTaskState, stored string) string {
	return buildWorkingSummary(state, stored)
}

func projectRecentTopics(record model.WorkingMemoryRecord) []string {
	return mergeRecentTopics(nil, record.RecentTopics, record.TaskState)
}

func mergeRecentTopics(existing, incoming []string, state model.WorkingTaskState) []string {
	out := make([]string, 0, len(existing)+len(incoming)+1)
	seen := map[string]struct{}{}
	add := func(items ...string) {
		for _, item := range items {
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
	}
	add(state.LessonTopic)
	add(existing...)
	add(incoming...)
	if len(out) > 5 {
		return out[:5]
	}
	return out
}

func recomputeSlotMetadata(state model.WorkingTaskState, provenance map[string]string, nowMs int64) map[string]model.TaskSlotMetadata {
	out := map[string]model.TaskSlotMetadata{}
	writeMeta := func(slot string, hasValue bool) {
		meta := model.TaskSlotMetadata{
			Status:     model.SlotStatusMissing,
			Provenance: strings.TrimSpace(provenance[slot]),
			UpdatedAt:  nowMs,
		}
		if meta.Provenance == "" {
			meta.Provenance = model.SlotProvenanceDerived
		}
		if hasValue {
			if meta.Provenance == model.SlotProvenanceExplicit {
				meta.Status = model.SlotStatusFilled
			} else {
				meta.Status = model.SlotStatusPartial
			}
		}
		out[slot] = meta
	}
	writeMeta(model.TaskSlotLessonTopic, strings.TrimSpace(state.LessonTopic) != "")
	writeMeta(model.TaskSlotKnowledgePoints, len(state.KnowledgePoints) > 0)
	writeMeta(model.TaskSlotTeachingGoals, len(state.TeachingGoals) > 0)
	writeMeta(model.TaskSlotKeyDifficulties, len(state.KeyDifficulties) > 0)
	writeMeta(model.TaskSlotTargetAudience, strings.TrimSpace(state.TargetAudience) != "")
	writeMeta(model.TaskSlotDuration, strings.TrimSpace(state.Duration) != "")
	writeMeta(model.TaskSlotOutputStyle, strings.TrimSpace(state.OutputStyle) != "")
	writeMeta(model.TaskSlotTeachingLogic, strings.TrimSpace(state.TeachingLogic) != "")
	writeMeta(model.TaskSlotConstraints, len(state.Constraints) > 0)
	writeMeta(model.TaskSlotReferenceMaterialUsage, len(state.ReferenceMaterialUsage) > 0)
	return out
}

func currentSlotProvenance(record model.WorkingMemoryRecord) map[string]string {
	out := map[string]string{}
	for k, meta := range record.SlotMetadata {
		if strings.TrimSpace(meta.Provenance) != "" {
			out[k] = strings.TrimSpace(meta.Provenance)
		}
	}
	return out
}

func applyProvenance(out map[string]string, signals model.TaskStateSignals) {
	if out == nil {
		return
	}
	for k, v := range signals.Provenance {
		if strings.TrimSpace(v) != "" {
			out[k] = strings.TrimSpace(v)
		}
	}
}

func chooseTaskValue(existing, incoming string) string {
	existing = strings.TrimSpace(existing)
	incoming = strings.TrimSpace(incoming)
	switch {
	case incoming == "":
		return existing
	case existing == "":
		return incoming
	case len([]rune(incoming)) >= len([]rune(existing)):
		return incoming
	default:
		return existing
	}
}

func mergeTaskList(existing, incoming []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(existing)+len(incoming))
	for _, item := range append(append([]string{}, existing...), incoming...) {
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

func supportedDurableFact(entry model.MemoryEntry) bool {
	return entry.Key == "subject" || entry.Key == "school_context"
}

func supportedDurablePreference(entry model.MemoryEntry) bool {
	if entry.Key == "teaching_style" || entry.Key == "content_depth" {
		return true
	}
	return entry.Context == "visual_preferences"
}

func classifyDurableWrites(messages []model.ConversationTurn, facts []model.MemoryEntry, prefs []model.MemoryEntry) ([]model.MemoryEntry, []model.MemoryEntry) {
	durableFacts := make([]model.MemoryEntry, 0, len(facts))
	durablePrefs := make([]model.MemoryEntry, 0, len(prefs))
	for _, fact := range facts {
		if strings.TrimSpace(fact.Source) != model.SlotProvenanceExplicit || !supportedDurableFact(fact) {
			continue
		}
		durableFacts = append(durableFacts, fact)
	}
	for _, pref := range prefs {
		if !supportedDurablePreference(pref) {
			continue
		}
		if pref.Confidence < 0.8 || strings.TrimSpace(pref.Source) != model.SlotProvenanceExplicit {
			continue
		}
		if !hasStandingPreferenceEvidence(pref, messages) {
			continue
		}
		if hasTaskScopedPreferenceEvidence(pref, messages) {
			continue
		}
		durablePrefs = append(durablePrefs, pref)
	}
	return durableFacts, durablePrefs
}

func hasStandingPreferenceEvidence(entry model.MemoryEntry, messages []model.ConversationTurn) bool {
	for _, msg := range messages {
		if strings.ToLower(strings.TrimSpace(msg.Role)) != "user" {
			continue
		}
		text := normalizedPolicyText(msg.Content)
		if !preferenceMentionMatches(entry, text) {
			continue
		}
		if hasAnyCue(text, standingPolicyCues()...) {
			return true
		}
		if !hasAnyCue(text, taskScopedPolicyCues()...) && (strings.Contains(text, "prefer") || strings.Contains(text, "like") || strings.Contains(text, "喜欢") || strings.Contains(text, "偏好")) {
			return true
		}
	}
	return false
}

func hasTaskScopedPreferenceEvidence(entry model.MemoryEntry, messages []model.ConversationTurn) bool {
	for _, msg := range messages {
		if strings.ToLower(strings.TrimSpace(msg.Role)) != "user" {
			continue
		}
		text := normalizedPolicyText(msg.Content)
		if !preferenceMentionMatches(entry, text) {
			continue
		}
		if hasAnyCue(text, taskScopedPolicyCues()...) {
			return true
		}
	}
	return false
}

func preferenceMentionMatches(entry model.MemoryEntry, text string) bool {
	value := strings.ToLower(strings.TrimSpace(entry.Value))
	key := strings.ToLower(strings.TrimSpace(entry.Key))
	if value != "" && strings.Contains(text, value) {
		return true
	}
	switch key {
	case "output_style":
		return hasAnyCue(text, "style", "slides", "ppt", "风格", "课件", "配色", "版式")
	case "teaching_style":
		return hasAnyCue(text, "teaching style", "teach", "教学风格", "上课风格", "讲课风格", "互动", "严谨")
	case "content_depth":
		return hasAnyCue(text, "depth", "detailed", "advanced", "深度", "详细", "深入")
	default:
		return false
	}
}

func normalizedPolicyText(text string) string {
	text = strings.ToLower(strings.TrimSpace(text))
	text = strings.NewReplacer(
		"，", ",",
		"。", ".",
		"；", ";",
		"：", ":",
		"、", ",",
		"　", " ",
	).Replace(text)
	return strings.Join(strings.Fields(text), " ")
}

func standingPolicyCues() []string {
	return []string{
		"usually", "generally", "normally", "across my lessons", "my normal style", "in general",
		"平时", "通常", "一般", "一贯", "我的习惯", "我平时", "我通常", "一直都是",
	}
}

func taskScopedPolicyCues() []string {
	return []string{
		"for this lesson", "for this ppt", "this time", "this lesson only", "current lesson", "current task", "for this class",
		"这节课", "这次课", "这次", "本次", "当前", "这份ppt", "这个课件", "这堂课", "仅这次", "只在这次",
	}
}

func hasAnyCue(text string, cues ...string) bool {
	for _, cue := range cues {
		if strings.Contains(text, cue) {
			return true
		}
	}
	return false
}

func extractSummaryAddon(fallback, lowerFallback string) string {
	for _, marker := range []string{"interaction:", "interaction ", "互动", "unresolved", "if possible"} {
		if idx := strings.Index(lowerFallback, marker); idx >= 0 {
			addon := strings.TrimSpace(fallback[idx:])
			if cut := strings.Index(addon, " | "); cut >= 0 {
				addon = strings.TrimSpace(addon[:cut])
			}
			return addon
		}
	}
	return strings.TrimSpace(fallback)
}
