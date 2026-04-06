package extractor

import (
	"regexp"
	"strings"

	"memory_service/internal/model"
)

var (
<<<<<<< HEAD
	reDuration = regexp.MustCompile(`(?i)\b(\d+\s*(minute|minutes|min|hour|hours))\b`)
=======
	reDuration = regexp.MustCompile(`(?i)\b(\d+\s*(minute|minutes|min|hour|hours))\b|(\d+\s*分钟)|(\d+\s*课时)`)
>>>>>>> origin/wang
)

func extractRequirementDialogue(userID string, messages []model.ConversationTurn) normalizedExtraction {
	facts := make([]model.MemoryEntry, 0, 4)
	preferences := make([]model.MemoryEntry, 0, 4)
	elems := model.TeachingElements{}
	collected := make([]string, 0, 8)
	summaryNotes := make([]string, 0, 8)
<<<<<<< HEAD
=======
	signals := model.TaskStateSignals{Provenance: map[string]string{}}
>>>>>>> origin/wang

	for _, msg := range messages {
		if strings.ToLower(strings.TrimSpace(msg.Role)) != "user" {
			continue
		}
<<<<<<< HEAD
		text := normalizeWhitespace(msg.Content)
=======
		text := normalizeRuleText(msg.Content)
>>>>>>> origin/wang
		if text == "" {
			continue
		}
		lower := strings.ToLower(text)

		if subject := detectTeacherSubject(lower, text); subject != "" {
<<<<<<< HEAD
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
=======
			facts = append(facts, makeFact(userID, "subject", subject, "general", 0.95, model.SlotProvenanceExplicit))
		}
		if school := detectSchool(text); school != "" {
			facts = append(facts, makeFact(userID, "school_context", school, "general", 0.9, model.SlotProvenanceExplicit))
		}
		if pref := detectVisualPreference(lower, text); pref != "" {
			preferences = append(preferences, makePreference(userID, "output_style", pref, "visual_preferences", 0.9, detectPreferenceSource(lower)))
		}
		if pref := detectTeachingStyle(lower, text); pref != "" {
			preferences = append(preferences, makePreference(userID, "teaching_style", pref, "general", 0.85, detectPreferenceSource(lower)))
		}

		if topic := detectTopic(text); topic != "" {
			signals.LessonTopic = chooseNonEmpty(signals.LessonTopic, topic)
			signals.Provenance[model.TaskSlotLessonTopic] = model.SlotProvenanceExplicit
			summaryNotes = append(summaryNotes, "topic: "+topic)
			collected = append(collected, model.TaskSlotLessonTopic)
		}
		if points := detectListValue(lower, text, []string{
			"knowledge points", "knowledge point", "cover", "include", "focus on",
			"知识点", "重点内容", "包括", "围绕", "讲",
		}); len(points) > 0 {
			signals.KnowledgePoints = mergeStringLists(signals.KnowledgePoints, points)
			signals.Provenance[model.TaskSlotKnowledgePoints] = model.SlotProvenanceExplicit
			elems.KnowledgePoints = mergeStringLists(elems.KnowledgePoints, points)
			collected = append(collected, model.TaskSlotKnowledgePoints)
		}
		if goals := detectListValue(lower, text, []string{
			"teaching goal", "teaching goals", "goal is", "goals are", "students should",
			"教学目标", "目标是", "目标", "希望学生", "让学生",
		}); len(goals) > 0 {
			signals.TeachingGoals = mergeStringLists(signals.TeachingGoals, goals)
			signals.Provenance[model.TaskSlotTeachingGoals] = model.SlotProvenanceExplicit
			elems.TeachingGoals = mergeStringLists(elems.TeachingGoals, goals)
			collected = append(collected, model.TaskSlotTeachingGoals)
		}
		if difficulties := detectListValue(lower, text, []string{
			"difficult", "difficulty", "key difficulty", "challenging",
			"重难点", "难点", "重点", "学生容易错", "易错点",
		}); len(difficulties) > 0 {
			signals.KeyDifficulties = mergeStringLists(signals.KeyDifficulties, difficulties)
			signals.Provenance[model.TaskSlotKeyDifficulties] = model.SlotProvenanceExplicit
			elems.KeyDifficulties = mergeStringLists(elems.KeyDifficulties, difficulties)
			collected = append(collected, model.TaskSlotKeyDifficulties)
		}
		if audience := detectAudience(lower, text); audience != "" {
			signals.TargetAudience = audience
			signals.Provenance[model.TaskSlotTargetAudience] = model.SlotProvenanceExplicit
			elems.TargetAudience = audience
			collected = append(collected, model.TaskSlotTargetAudience)
		}
		if duration := detectDuration(text); duration != "" {
			signals.Duration = duration
			signals.Provenance[model.TaskSlotDuration] = model.SlotProvenanceExplicit
			elems.Duration = duration
			collected = append(collected, model.TaskSlotDuration)
		}
		if style := detectOutputStyle(lower, text); style != "" {
			signals.OutputStyle = style
			signals.Provenance[model.TaskSlotOutputStyle] = model.SlotProvenanceExplicit
			elems.OutputStyle = style
			collected = append(collected, model.TaskSlotOutputStyle)
		}
		if logic := detectPlanningCue(lower, text, []string{
			"teach", "first", "then", "finally", "teaching logic", "sequence",
			"先", "再", "然后", "最后", "教学逻辑", "讲解顺序", "流程",
		}); logic != "" {
			signals.TeachingLogic = chooseNonEmpty(signals.TeachingLogic, logic)
			signals.Provenance[model.TaskSlotTeachingLogic] = model.SlotProvenanceExplicit
			summaryNotes = append(summaryNotes, "teaching logic: "+logic)
		}
		if refs := detectPlanningCue(lower, text, []string{
			"reference", "pdf", "ppt", "lesson plan", "textbook",
			"参考", "教材", "课本", "pdf", "讲义", "以前的ppt", "参考资料",
		}); refs != "" {
			signals.ReferenceMaterialUsage = mergeStringLists(signals.ReferenceMaterialUsage, []string{refs})
			signals.Provenance[model.TaskSlotReferenceMaterialUsage] = model.SlotProvenanceExplicit
			summaryNotes = append(summaryNotes, "reference usage: "+refs)
		}
		if constraints := detectConstraintNotes(lower, text); len(constraints) > 0 {
			signals.Constraints = mergeStringLists(signals.Constraints, constraints)
			signals.Provenance[model.TaskSlotConstraints] = model.SlotProvenanceExplicit
			summaryNotes = append(summaryNotes, "constraints: "+strings.Join(constraints, ", "))
		}
		if interactions := detectPlanningCue(lower, text, []string{
			"interaction", "quiz", "game", "animation",
			"互动", "测验", "小游戏", "动画",
		}); interactions != "" {
>>>>>>> origin/wang
			summaryNotes = append(summaryNotes, "interaction: "+interactions)
		}
	}

<<<<<<< HEAD
	return normalizedExtraction{
		facts:            dedupeEntries(facts),
		preferences:      dedupeEntries(preferences),
		teachingElements: normalizeTeachingElements(elems),
		summary:          buildRequirementSummary(messages, dedupeStrings(collected), dedupeStrings(summaryNotes), normalizeTeachingElements(elems)),
	}
}

func makeFact(userID, key, value, ctx string, confidence float64) model.MemoryEntry {
=======
	normalizedSignals := normalizeTaskStateSignals(signals)
	normalizedElems := normalizeTeachingElements(elems)
	return normalizedExtraction{
		facts:            dedupeEntries(facts),
		preferences:      dedupeEntries(preferences),
		teachingElements: normalizedElems,
		taskStateSignals: normalizedSignals,
		summary:          buildRequirementSummary(messages, dedupeStrings(collected), dedupeStrings(summaryNotes), normalizedSignals, normalizedElems),
	}
}

func makeFact(userID, key, value, ctx string, confidence float64, source string) model.MemoryEntry {
>>>>>>> origin/wang
	return model.MemoryEntry{
		UserID:     userID,
		Category:   "fact",
		Key:        key,
		Value:      value,
		Context:    ctx,
		Confidence: confidence,
<<<<<<< HEAD
		Source:     "inferred",
	}
}

func makePreference(userID, key, value, ctx string, confidence float64) model.MemoryEntry {
=======
		Source:     source,
	}
}

func makePreference(userID, key, value, ctx string, confidence float64, source string) model.MemoryEntry {
>>>>>>> origin/wang
	return model.MemoryEntry{
		UserID:     userID,
		Category:   "preference",
		Key:        key,
		Value:      value,
		Context:    ctx,
		Confidence: confidence,
<<<<<<< HEAD
		Source:     "inferred",
	}
}

func normalizeWhitespace(text string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
=======
		Source:     source,
	}
}

func detectPreferenceSource(lower string) string {
	if hasAnyCue(lower, standingPreferenceCues()...) || strings.Contains(lower, "prefer") || strings.Contains(lower, "喜欢") {
		return model.SlotProvenanceExplicit
	}
	return model.SlotProvenanceInferred
}

func normalizeRuleText(text string) string {
	replacer := strings.NewReplacer(
		"，", ", ",
		"。", ". ",
		"；", "; ",
		"：", ": ",
		"（", " (",
		"）", ") ",
		"、", ", ",
		"！", "! ",
		"？", "? ",
		"“", "\"",
		"”", "\"",
		"‘", "'",
		"’", "'",
		"　", " ",
	)
	text = replacer.Replace(strings.TrimSpace(text))
	return strings.Join(strings.Fields(text), " ")
>>>>>>> origin/wang
}

func detectTeacherSubject(lower, original string) string {
	patterns := []string{
		"i teach ", "i'm teaching ", "i am teaching ", "my subject is ", "for my ",
<<<<<<< HEAD
=======
		"我教", "我是", "我带", "我的学科是",
>>>>>>> origin/wang
	}
	for _, pattern := range patterns {
		idx := strings.Index(lower, pattern)
		if idx >= 0 {
			value := strings.TrimSpace(original[idx+len(pattern):])
<<<<<<< HEAD
			return trimSentence(value)
=======
			value = trimSentence(value)
			for _, suffix := range []string{"老师", "课", "课程", "的课件"} {
				value = strings.TrimSpace(strings.TrimSuffix(value, suffix))
			}
			return value
>>>>>>> origin/wang
		}
	}
	return ""
}

func detectSchool(text string) string {
	lower := strings.ToLower(text)
<<<<<<< HEAD
	for _, marker := range []string{"school", "college", "university"} {
=======
	for _, marker := range []string{"school", "college", "university", "高中", "初中", "小学", "大学"} {
>>>>>>> origin/wang
		if strings.Contains(lower, marker) {
			return trimSentence(text)
		}
	}
	return ""
}

func detectVisualPreference(lower, text string) string {
<<<<<<< HEAD
	if !(strings.Contains(lower, "prefer") || strings.Contains(lower, "like")) {
		return ""
	}
	for _, marker := range []string{"minimal", "clean", "simple", "academic", "visual", "style", "ppt"} {
=======
	if !(strings.Contains(lower, "prefer") || strings.Contains(lower, "like") || strings.Contains(lower, "喜欢") || strings.Contains(lower, "偏好")) {
		return ""
	}
	for _, marker := range []string{"minimal", "clean", "simple", "academic", "visual", "style", "ppt", "简洁", "极简", "学术", "风格", "版式", "配色"} {
>>>>>>> origin/wang
		if strings.Contains(lower, marker) {
			return trimSentence(text)
		}
	}
	return ""
}

func detectTeachingStyle(lower, text string) string {
<<<<<<< HEAD
	if strings.Contains(lower, "interactive") || strings.Contains(lower, "rigorous") || strings.Contains(lower, "narrative") {
		return trimSentence(text)
=======
	for _, marker := range []string{"interactive", "rigorous", "narrative", "互动", "严谨", "启发式", "讲故事"} {
		if strings.Contains(lower, marker) {
			return trimSentence(text)
		}
>>>>>>> origin/wang
	}
	return ""
}

func detectTopic(text string) string {
	lower := strings.ToLower(text)
<<<<<<< HEAD
	for _, marker := range []string{"topic is", "topic:", "lesson on", "ppt on", "courseware on"} {
=======
	for _, marker := range []string{"topic is", "topic:", "lesson on", "ppt on", "courseware on", "主题是", "课题是", "这节课讲", "这次课讲", "做一份关于"} {
>>>>>>> origin/wang
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
<<<<<<< HEAD
	for _, marker := range []string{"audience", "for ", "students", "grade", "freshmen", "beginners"} {
=======
	for _, marker := range []string{"audience", "for ", "students", "grade", "freshmen", "beginners", "面向", "给", "学生", "年级", "高一", "高二", "初一", "小学"} {
>>>>>>> origin/wang
		if strings.Contains(lower, marker) {
			return trimSentence(text)
		}
	}
	return ""
}

func detectDuration(text string) string {
<<<<<<< HEAD
	match := reDuration.FindString(text)
	return strings.TrimSpace(match)
}

func detectOutputStyle(lower, text string) string {
	for _, marker := range []string{"style", "minimalist", "minimal", "academic", "tech style", "presentation style"} {
=======
	match := strings.TrimSpace(reDuration.FindString(text))
	return match
}

func detectOutputStyle(lower, text string) string {
	for _, marker := range []string{"style", "minimalist", "minimal", "academic", "tech style", "presentation style", "风格", "简洁", "科技感", "学术风", "蓝色", "深蓝"} {
>>>>>>> origin/wang
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

<<<<<<< HEAD
=======
func detectConstraintNotes(lower, text string) []string {
	if !hasAnyCue(lower, "only", "must", "don't", "do not", "avoid", "控制在", "只用", "不要", "避免", "尽量") {
		return nil
	}
	return splitList(trimSentence(text))
}

>>>>>>> origin/wang
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
<<<<<<< HEAD
		return r == ',' || r == ';' || r == '|' || r == '/'
=======
		return r == ',' || r == ';' || r == '|' || r == '/' || r == '、'
>>>>>>> origin/wang
	})
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(strings.TrimPrefix(part, "and "))
		part = strings.TrimSpace(strings.TrimPrefix(part, "to "))
<<<<<<< HEAD
=======
		part = strings.TrimSpace(strings.TrimPrefix(part, "以及"))
		part = strings.TrimSpace(strings.TrimPrefix(part, "还有"))
		part = strings.TrimSpace(strings.TrimPrefix(part, "和"))
>>>>>>> origin/wang
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

<<<<<<< HEAD
=======
func normalizeTaskStateSignals(in model.TaskStateSignals) model.TaskStateSignals {
	out := in
	out.KnowledgePoints = dedupeStrings(in.KnowledgePoints)
	out.TeachingGoals = dedupeStrings(in.TeachingGoals)
	out.KeyDifficulties = dedupeStrings(in.KeyDifficulties)
	out.Constraints = dedupeStrings(in.Constraints)
	out.ReferenceMaterialUsage = dedupeStrings(in.ReferenceMaterialUsage)
	out.LessonTopic = strings.TrimSpace(in.LessonTopic)
	out.TargetAudience = strings.TrimSpace(in.TargetAudience)
	out.Duration = strings.TrimSpace(in.Duration)
	out.OutputStyle = strings.TrimSpace(in.OutputStyle)
	out.TeachingLogic = strings.TrimSpace(in.TeachingLogic)
	if out.Provenance == nil {
		out.Provenance = map[string]string{}
	}
	return out
}

>>>>>>> origin/wang
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

<<<<<<< HEAD
func buildRequirementSummary(messages []model.ConversationTurn, collected []string, notes []string, elems model.TeachingElements) string {
=======
func buildRequirementSummary(messages []model.ConversationTurn, collected []string, notes []string, signals model.TaskStateSignals, elems model.TeachingElements) string {
>>>>>>> origin/wang
	parts := make([]string, 0, 6)
	if len(collected) > 0 {
		parts = append(parts, "Requirement collection progress: "+strings.Join(collected, ", "))
	}
<<<<<<< HEAD
=======
	if signals.LessonTopic != "" {
		parts = append(parts, "Lesson topic: "+signals.LessonTopic)
	}
>>>>>>> origin/wang
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
<<<<<<< HEAD
		chunks = append([]string{normalizeWhitespace(content)}, chunks...)
	}
	return strings.Join(chunks, " ")
}
=======
		chunks = append([]string{normalizeRuleText(content)}, chunks...)
	}
	return strings.Join(chunks, " ")
}

func hasAnyCue(text string, cues ...string) bool {
	for _, cue := range cues {
		if strings.Contains(text, cue) {
			return true
		}
	}
	return false
}

func standingPreferenceCues() []string {
	return []string{
		"usually", "generally", "normally", "across my lessons", "my normal style", "i prefer", "i like",
		"平时", "通常", "一般", "一贯", "一直", "我的习惯", "我喜欢", "我更喜欢", "我通常会",
	}
}
>>>>>>> origin/wang
