package model

type WorkingMemory struct {
	SessionID           string           `json:"session_id"`
	UserID              string           `json:"user_id"`
	ConversationSummary string           `json:"conversation_summary"`
	ExtractedElements   TeachingElements `json:"extracted_elements"`
	RecentTopics        []string         `json:"recent_topics"`
	UpdatedAt           int64            `json:"updated_at"`
}

type TeachingElements struct {
	KnowledgePoints []string `json:"knowledge_points"`
	TeachingGoals   []string `json:"teaching_goals"`
	KeyDifficulties []string `json:"key_difficulties"`
	TargetAudience  string   `json:"target_audience"`
	Duration        string   `json:"duration"`
	OutputStyle     string   `json:"output_style"`
}

type ConversationTurn struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
