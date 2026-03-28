package agent

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// TaskRequirements represents the collected requirements for a teaching task.
type TaskRequirements struct {
	SessionID string `json:"session_id"`
	UserID    string `json:"user_id"`

	Topic           string   `json:"topic"`
	Subject         string   `json:"subject"`
	KnowledgePoints []string `json:"knowledge_points"`
	TeachingGoals   []string `json:"teaching_goals"`
	TeachingLogic   string   `json:"teaching_logic"`
	TargetAudience  string   `json:"target_audience"`

	KeyDifficulties   []string `json:"key_difficulties"`
	Duration          string   `json:"duration"`
	TotalPages        int      `json:"total_pages"`
	GlobalStyle       string   `json:"global_style"`
	InteractionDesign string   `json:"interaction_design"`
	OutputFormats     []string `json:"output_formats"`
	AdditionalNotes   string   `json:"additional_notes"`

	ReferenceFiles []ReferenceFileReq `json:"reference_files"`

	CollectedFields []string `json:"collected_fields"`
	Status          string   `json:"status"`
	CreatedAt       int64    `json:"created_at"`
	UpdatedAt       int64    `json:"updated_at"`
}

// GetCollectedFields returns the CollectedFields slice.
func (r *TaskRequirements) GetCollectedFields() []string { return r.CollectedFields }

// SetCollectedFields sets the CollectedFields slice.
func (r *TaskRequirements) SetCollectedFields(f []string) { r.CollectedFields = f }

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
		return []string{"topic", "subject", "audience", "total_pages", "knowledge_points", "teaching_goals", "teaching_logic", "key_difficulties", "duration", "global_style", "interaction_design", "output_formats"}
	}
	var missing []string
	if strings.TrimSpace(r.Topic) == "" {
		missing = append(missing, "topic")
	}
	if strings.TrimSpace(r.Subject) == "" {
		missing = append(missing, "subject")
	}
	if strings.TrimSpace(r.TargetAudience) == "" {
		missing = append(missing, "audience")
	}
	if r.TotalPages <= 0 {
		missing = append(missing, "total_pages")
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
	if len(r.KeyDifficulties) == 0 {
		missing = append(missing, "key_difficulties")
	}
	if strings.TrimSpace(r.Duration) == "" {
		missing = append(missing, "duration")
	}
	if strings.TrimSpace(r.GlobalStyle) == "" {
		missing = append(missing, "global_style")
	}
	if strings.TrimSpace(r.InteractionDesign) == "" {
		missing = append(missing, "interaction_design")
	}
	if len(r.OutputFormats) == 0 {
		missing = append(missing, "output_formats")
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
	collected := make([]string, 0, 13)
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
			fmt.Fprintf(&sb, "- %s\n", f)
		}
	}

	sb.WriteString("\n【待收集信息 Checklist】\n")
	if len(missing) == 0 {
		sb.WriteString("- 所有 P0 字段已收集完成，可进入确认\n")
	} else {
		for _, f := range missing {
			fmt.Fprintf(&sb, "- [待收集] %s\n", f)
		}
	}

	sb.WriteString("\n【用户画像（来自记忆模块）】\n")
	sb.WriteString(formatProfileSummary(profile))

	sb.WriteString("\n【行为准则】\n")
	sb.WriteString("1. 自然地融入对话，不要机械地逐条追问\n")
	sb.WriteString("2. 每轮最多问1-2个问题\n")
	sb.WriteString("3. 如果教师一句话涵盖了多个信息，全部提取\n")
	sb.WriteString("4. 教师上传文件时，主动询问如何使用该资料\n")
	sb.WriteString("5. 所有必填字段收集完毕后，前端会弹出卡片让用户确认，等待用户语音确认\n")
	sb.WriteString("6. 用户确认后调用 @{ppt_init|...} 工具开始制作PPT\n")
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
