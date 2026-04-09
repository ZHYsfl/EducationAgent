package agent

import (
	"fmt"
	"strings"
	"time"
)

// TaskRequirements holds all fields needed to generate a PPT.
// Status: "collecting" → "ready"
type TaskRequirements struct {
	SessionID string `json:"session_id"`
	UserID    string `json:"user_id"`
	Status    string `json:"status"` // "collecting" | "ready"
	UpdatedAt int64  `json:"updated_at"`

	Topic             string             `json:"topic"`
	Description       string             `json:"description"`
	TargetAudience    string             `json:"audience"`
	KnowledgePoints   []string           `json:"knowledge_points"`
	TeachingGoals     []string           `json:"teaching_goals"`
	TeachingLogic     string             `json:"teaching_logic"`
	KeyDifficulties   []string           `json:"key_difficulties"`
	Duration          string             `json:"duration"`
	TotalPages        int                `json:"total_pages"`
	GlobalStyle       string             `json:"global_style"`
	InteractionDesign string             `json:"interaction_design"`
	OutputFormats     []string           `json:"output_formats"`
	AdditionalNotes   string             `json:"additional_notes,omitempty"`
	ReferenceFiles    []ReferenceFileReq `json:"reference_files,omitempty"`

	CollectedFields []string `json:"collected_fields"`
}

func NewTaskRequirements(sessionID, userID string) *TaskRequirements {
	return &TaskRequirements{
		SessionID: sessionID,
		UserID:    userID,
		Status:    "collecting",
		UpdatedAt: time.Now().UnixMilli(),
	}
}

// GetMissingFields returns all required fields that are not yet filled.
func (r *TaskRequirements) GetMissingFields() []string {
	if r == nil {
		return []string{"topic", "description", "audience", "knowledge_points", "teaching_goals",
			"teaching_logic", "key_difficulties", "duration", "total_pages",
			"global_style", "interaction_design", "output_formats"}
	}
	var missing []string
	if r.Topic == "" {
		missing = append(missing, "topic")
	}
	if r.Description == "" {
		missing = append(missing, "description")
	}
	if r.TargetAudience == "" {
		missing = append(missing, "audience")
	}
	if len(r.KnowledgePoints) == 0 {
		missing = append(missing, "knowledge_points")
	}
	if len(r.TeachingGoals) == 0 {
		missing = append(missing, "teaching_goals")
	}
	if r.TeachingLogic == "" {
		missing = append(missing, "teaching_logic")
	}
	if len(r.KeyDifficulties) == 0 {
		missing = append(missing, "key_difficulties")
	}
	if r.Duration == "" {
		missing = append(missing, "duration")
	}
	if r.TotalPages == 0 {
		missing = append(missing, "total_pages")
	}
	if r.GlobalStyle == "" {
		missing = append(missing, "global_style")
	}
	if r.InteractionDesign == "" {
		missing = append(missing, "interaction_design")
	}
	if len(r.OutputFormats) == 0 {
		missing = append(missing, "output_formats")
	}
	return missing
}

func (r *TaskRequirements) IsReady() bool {
	return len(r.GetMissingFields()) == 0
}

func (r *TaskRequirements) RefreshCollectedFields() {
	var collected []string
	if r.Topic != "" {
		collected = append(collected, "topic")
	}
	if r.Description != "" {
		collected = append(collected, "description")
	}
	if r.TargetAudience != "" {
		collected = append(collected, "audience")
	}
	if len(r.KnowledgePoints) > 0 {
		collected = append(collected, "knowledge_points")
	}
	if len(r.TeachingGoals) > 0 {
		collected = append(collected, "teaching_goals")
	}
	if r.TeachingLogic != "" {
		collected = append(collected, "teaching_logic")
	}
	if len(r.KeyDifficulties) > 0 {
		collected = append(collected, "key_difficulties")
	}
	if r.Duration != "" {
		collected = append(collected, "duration")
	}
	if r.TotalPages > 0 {
		collected = append(collected, "total_pages")
	}
	if r.GlobalStyle != "" {
		collected = append(collected, "global_style")
	}
	if r.InteractionDesign != "" {
		collected = append(collected, "interaction_design")
	}
	if len(r.OutputFormats) > 0 {
		collected = append(collected, "output_formats")
	}
	r.CollectedFields = collected
}

func (r *TaskRequirements) Clone() *TaskRequirements {
	if r == nil {
		return nil
	}
	c := *r
	c.KnowledgePoints = append([]string{}, r.KnowledgePoints...)
	c.TeachingGoals = append([]string{}, r.TeachingGoals...)
	c.KeyDifficulties = append([]string{}, r.KeyDifficulties...)
	c.OutputFormats = append([]string{}, r.OutputFormats...)
	c.CollectedFields = append([]string{}, r.CollectedFields...)
	c.ReferenceFiles = append([]ReferenceFileReq{}, r.ReferenceFiles...)
	return &c
}

// BuildCollectionPrompt generates the system prompt for the requirements-gathering phase.
func (r *TaskRequirements) BuildCollectionPrompt() string {
	var sb strings.Builder
	sb.WriteString("你是一个专业的教学课件制作助手，正在帮助用户收集制作PPT所需的信息。\n\n")

	missing := r.GetMissingFields()
	if len(missing) > 0 {
		sb.WriteString(fmt.Sprintf("还需要收集以下信息：%s\n\n", strings.Join(missing, "、")))
	}

	if len(r.CollectedFields) > 0 {
		sb.WriteString("已收集的信息：\n")
		if r.Topic != "" {
			fmt.Fprintf(&sb, "- topic: %s\n", r.Topic)
		}
		if r.Description != "" {
			fmt.Fprintf(&sb, "- description: %s\n", r.Description)
		}
		if r.TargetAudience != "" {
			fmt.Fprintf(&sb, "- audience: %s\n", r.TargetAudience)
		}
		if r.TotalPages > 0 {
			fmt.Fprintf(&sb, "- total_pages: %d\n", r.TotalPages)
		}
		if r.Duration != "" {
			fmt.Fprintf(&sb, "- duration: %s\n", r.Duration)
		}
		if r.GlobalStyle != "" {
			fmt.Fprintf(&sb, "- global_style: %s\n", r.GlobalStyle)
		}
		if len(r.KnowledgePoints) > 0 {
			fmt.Fprintf(&sb, "- knowledge_points: %s\n", strings.Join(r.KnowledgePoints, "、"))
		}
		if len(r.TeachingGoals) > 0 {
			fmt.Fprintf(&sb, "- teaching_goals: %s\n", strings.Join(r.TeachingGoals, "、"))
		}
		if r.TeachingLogic != "" {
			fmt.Fprintf(&sb, "- teaching_logic: %s\n", r.TeachingLogic)
		}
		if len(r.KeyDifficulties) > 0 {
			fmt.Fprintf(&sb, "- key_difficulties: %s\n", strings.Join(r.KeyDifficulties, "、"))
		}
		if r.InteractionDesign != "" {
			fmt.Fprintf(&sb, "- interaction_design: %s\n", r.InteractionDesign)
		}
		if len(r.OutputFormats) > 0 {
			fmt.Fprintf(&sb, "- output_formats: %s\n", strings.Join(r.OutputFormats, "、"))
		}

	}

	sb.WriteString("\n请自然地与用户对话，逐步收集缺失的信息。每次只问1-2个问题。")
	return sb.String()
}
