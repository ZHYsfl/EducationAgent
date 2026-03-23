package agent

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

func NewTaskRequirements(sessionID, userID string) *TaskRequirements {
	now := time.Now().UnixMilli()
	return &TaskRequirements{
		SessionID:       sessionID,
		UserID:          userID,
		Status:          "collecting",
		OutputFormats:   []string{"pptx"},
		ReferenceFiles:  make([]ReferenceFileReq, 0),
		CollectedFields: make([]string, 0),
		CreatedAt:       now,
		UpdatedAt:       now,
	}
}

func CloneTaskRequirements(in *TaskRequirements) *TaskRequirements {
	if in == nil {
		return nil
	}
	out := *in
	out.KnowledgePoints = append([]string(nil), in.KnowledgePoints...)
	out.TeachingGoals = append([]string(nil), in.TeachingGoals...)
	out.KeyDifficulties = append([]string(nil), in.KeyDifficulties...)
	out.OutputFormats = append([]string(nil), in.OutputFormats...)
	out.CollectedFields = append([]string(nil), in.CollectedFields...)
	if len(in.ReferenceFiles) > 0 {
		out.ReferenceFiles = make([]ReferenceFileReq, len(in.ReferenceFiles))
		copy(out.ReferenceFiles, in.ReferenceFiles)
	} else {
		out.ReferenceFiles = nil
	}
	return &out
}

func (r *TaskRequirements) GetMissingFields() []string {
	if r == nil {
		return []string{"topic", "knowledge_points", "teaching_goals", "teaching_logic", "target_audience"}
	}
	var missing []string
	if strings.TrimSpace(r.Topic) == "" {
		missing = append(missing, "topic")
	}
	if len(r.KnowledgePoints) == 0 {
		missing = append(missing, "knowledge_points")
	}
	if len(r.TeachingGoals) == 0 {
		missing = append(missing, "teaching_goals")
	}
	if strings.TrimSpace(r.TeachingLogic) == "" {
		missing = append(missing, "teaching_logic")
	}
	if strings.TrimSpace(r.TargetAudience) == "" {
		missing = append(missing, "target_audience")
	}
	return missing
}

func (r *TaskRequirements) IsReadyForConfirm() bool {
	return len(r.GetMissingFields()) == 0
}

func (r *TaskRequirements) RefreshCollectedFields() {
	if r == nil {
		return
	}
	collected := make([]string, 0, 16)
	add := func(name string, ok bool) {
		if ok {
			collected = append(collected, name)
		}
	}
	add("topic", strings.TrimSpace(r.Topic) != "")
	add("knowledge_points", len(r.KnowledgePoints) > 0)
	add("teaching_goals", len(r.TeachingGoals) > 0)
	add("teaching_logic", strings.TrimSpace(r.TeachingLogic) != "")
	add("target_audience", strings.TrimSpace(r.TargetAudience) != "")
	add("key_difficulties", len(r.KeyDifficulties) > 0)
	add("duration", strings.TrimSpace(r.Duration) != "")
	add("total_pages", r.TotalPages > 0)
	add("global_style", strings.TrimSpace(r.GlobalStyle) != "")
	add("interaction_design", strings.TrimSpace(r.InteractionDesign) != "")
	add("output_formats", len(r.OutputFormats) > 0)
	add("additional_notes", strings.TrimSpace(r.AdditionalNotes) != "")
	add("reference_files", len(r.ReferenceFiles) > 0)
	r.CollectedFields = collected
}

func (r *TaskRequirements) BuildRequirementsSystemPrompt(profile *UserProfile) string {
	if r == nil {
		return ""
	}
	r.RefreshCollectedFields()
	missing := r.GetMissingFields()

	var sb strings.Builder
	sb.WriteString("你是一位专业的教学助手，正在帮助教师设计课件。你需要通过对话收集信息来制作高质量的PPT课件。\n\n")

	sb.WriteString("【已收集的信息】\n")
	if len(r.CollectedFields) == 0 {
		sb.WriteString("- 暂无\n")
	} else {
		for _, f := range r.CollectedFields {
			sb.WriteString(fmt.Sprintf("- %s\n", f))
		}
	}

	sb.WriteString("\n【待收集信息 Checklist】\n")
	if len(missing) == 0 {
		sb.WriteString("- 所有 P0 字段已收集完成，可进入确认\n")
	} else {
		for _, f := range missing {
			sb.WriteString(fmt.Sprintf("- [待收集] %s\n", f))
		}
	}

	sb.WriteString("\n【用户画像（来自记忆模块）】\n")
	sb.WriteString(formatProfileSummary(profile))

	sb.WriteString("\n【行为准则】\n")
	sb.WriteString("1. 自然地融入对话，不要机械地逐条追问\n")
	sb.WriteString("2. 每轮最多问1-2个问题\n")
	sb.WriteString("3. 如果教师一句话涵盖了多个信息，全部提取\n")
	sb.WriteString("4. 教师上传文件时，主动询问如何使用该资料\n")
	sb.WriteString("5. 所有P0字段收集完毕后，生成一份结构化摘要让教师确认\n")
	sb.WriteString("6. 确认后输出特殊标记: [REQUIREMENTS_CONFIRMED]\n")
	return sb.String()
}

func formatStringMap(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+m[k])
	}
	return strings.Join(parts, ",")
}

// formatProfileSummary 将 Memory 返回的 UserProfile 全部非空字段写入系统提示，供需求收集 LLM 参考。
// 字段定义见 types.go UserProfile。
func formatProfileSummary(profile *UserProfile) string {
	if profile == nil {
		return "暂无画像信息。"
	}
	var items []string
	if profile.UserID != "" {
		items = append(items, "用户ID="+profile.UserID)
	}
	if profile.DisplayName != "" {
		items = append(items, "姓名="+profile.DisplayName)
	}
	if profile.Subject != "" {
		items = append(items, "学科="+profile.Subject)
	}
	if profile.School != "" {
		items = append(items, "学校="+profile.School)
	}
	if profile.TeachingStyle != "" {
		items = append(items, "授课风格="+profile.TeachingStyle)
	}
	if profile.ContentDepth != "" {
		items = append(items, "内容深度="+profile.ContentDepth)
	}
	if s := formatStringMap(profile.Preferences); s != "" {
		items = append(items, "偏好="+s)
	}
	if s := formatStringMap(profile.VisualPreferences); s != "" {
		items = append(items, "视觉偏好="+s)
	}
	if profile.HistorySummary != "" {
		items = append(items, "历史摘要="+profile.HistorySummary)
	}
	if profile.LastActiveAt != 0 {
		items = append(items, fmt.Sprintf("最近活跃(ms)=%d", profile.LastActiveAt))
	}
	if len(items) == 0 {
		return "暂无画像信息。"
	}
	return strings.Join(items, "；")
}

func (r *TaskRequirements) BuildSummaryText() string {
	if r == nil {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("我整理一下您的需求：\n")
	if r.Topic != "" {
		sb.WriteString(fmt.Sprintf("- 主题：%s\n", r.Topic))
	}
	if len(r.KnowledgePoints) > 0 {
		sb.WriteString(fmt.Sprintf("- 知识点：%s\n", strings.Join(r.KnowledgePoints, "、")))
	}
	if len(r.TeachingGoals) > 0 {
		sb.WriteString(fmt.Sprintf("- 教学目标：%s\n", strings.Join(r.TeachingGoals, "；")))
	}
	if r.TeachingLogic != "" {
		sb.WriteString(fmt.Sprintf("- 讲授逻辑：%s\n", r.TeachingLogic))
	}
	if r.TargetAudience != "" {
		sb.WriteString(fmt.Sprintf("- 目标受众：%s\n", r.TargetAudience))
	}
	if len(r.KeyDifficulties) > 0 {
		sb.WriteString(fmt.Sprintf("- 重点难点：%s\n", strings.Join(r.KeyDifficulties, "、")))
	}
	if r.TotalPages > 0 {
		sb.WriteString(fmt.Sprintf("- 页数：%d 页\n", r.TotalPages))
	}
	if r.GlobalStyle != "" {
		sb.WriteString(fmt.Sprintf("- 风格：%s\n", r.GlobalStyle))
	}
	sb.WriteString("\n您看这样理解对吗？有需要调整的地方吗？")
	return sb.String()
}

func (r *TaskRequirements) ToPPTInitRequest() PPTInitRequest {
	description := buildDetailedDescription(r)
	return PPTInitRequest{
		UserID:      r.UserID,
		Topic:       r.Topic,
		Description: description,
		TotalPages:  r.TotalPages,
		Audience:    r.TargetAudience,
		GlobalStyle: r.GlobalStyle,
		SessionID:   r.SessionID,
		TeachingElements: &InitTeachingElements{
			KnowledgePoints:   r.KnowledgePoints,
			TeachingGoals:     r.TeachingGoals,
			TeachingLogic:     r.TeachingLogic,
			KeyDifficulties:   r.KeyDifficulties,
			Duration:          r.Duration,
			InteractionDesign: r.InteractionDesign,
			OutputFormats:     r.OutputFormats,
		},
		ReferenceFiles: toReferenceFiles(r.ReferenceFiles),
	}
}

func toReferenceFiles(in []ReferenceFileReq) []ReferenceFile {
	out := make([]ReferenceFile, 0, len(in))
	for _, f := range in {
		out = append(out, ReferenceFile{
			FileID:      f.FileID,
			FileURL:     f.FileURL,
			FileType:    f.FileType,
			Instruction: f.Instruction,
		})
	}
	return out
}

func buildDetailedDescription(r *TaskRequirements) string {
	if r == nil {
		return ""
	}
	var sb strings.Builder
	if r.Topic != "" {
		sb.WriteString(fmt.Sprintf("【课程主题】%s\n", r.Topic))
	}
	if len(r.TeachingGoals) > 0 {
		sb.WriteString(fmt.Sprintf("【教学目标】%s\n", strings.Join(r.TeachingGoals, "；")))
	}
	if len(r.KnowledgePoints) > 0 {
		sb.WriteString(fmt.Sprintf("【核心知识点】%s\n", strings.Join(r.KnowledgePoints, "、")))
	}
	if r.TeachingLogic != "" {
		sb.WriteString(fmt.Sprintf("【讲授逻辑】%s\n", r.TeachingLogic))
	}
	if r.TargetAudience != "" {
		sb.WriteString(fmt.Sprintf("【目标受众】%s\n", r.TargetAudience))
	}
	if len(r.KeyDifficulties) > 0 {
		sb.WriteString(fmt.Sprintf("【重点难点】%s\n", strings.Join(r.KeyDifficulties, "；")))
	}
	if r.Duration != "" {
		sb.WriteString(fmt.Sprintf("【课时长度】%s\n", r.Duration))
	}
	if r.TotalPages > 0 {
		sb.WriteString(fmt.Sprintf("【期望页数】%d 页\n", r.TotalPages))
	}
	if r.GlobalStyle != "" {
		sb.WriteString(fmt.Sprintf("【风格偏好】%s\n", r.GlobalStyle))
	}
	if r.InteractionDesign != "" {
		sb.WriteString(fmt.Sprintf("【互动设计】%s\n", r.InteractionDesign))
	}
	if len(r.OutputFormats) > 0 {
		sb.WriteString(fmt.Sprintf("【输出格式】%s\n", strings.Join(r.OutputFormats, ", ")))
	}
	if r.AdditionalNotes != "" {
		sb.WriteString(fmt.Sprintf("【补充要求】%s\n", r.AdditionalNotes))
	}
	if len(r.ReferenceFiles) > 0 {
		sb.WriteString("【参考资料】\n")
		for _, f := range r.ReferenceFiles {
			sb.WriteString(fmt.Sprintf("- %s (%s): %s\n", f.FileID, f.FileType, f.Instruction))
		}
	}
	return strings.TrimSpace(sb.String())
}
