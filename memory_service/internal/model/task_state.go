package model

const (
	SlotStatusMissing = "missing"
	SlotStatusPartial = "partial"
	SlotStatusFilled  = "filled"

	SlotProvenanceExplicit = "explicit"
	SlotProvenanceInferred = "inferred"
	SlotProvenanceDerived  = "derived"

	ContinuityActive = "active"
	ContinuityStale  = "stale"
)

const (
	TaskSlotLessonTopic            = "lesson_topic"
	TaskSlotKnowledgePoints        = "knowledge_points"
	TaskSlotTeachingGoals          = "teaching_goals"
	TaskSlotKeyDifficulties        = "key_difficulties"
	TaskSlotTargetAudience         = "target_audience"
	TaskSlotDuration               = "duration"
	TaskSlotOutputStyle            = "output_style"
	TaskSlotTeachingLogic          = "teaching_logic"
	TaskSlotConstraints            = "constraints"
	TaskSlotReferenceMaterialUsage = "reference_material_usage"
)

type WorkingTaskState struct {
	LessonTopic            string   `json:"lesson_topic"`
	KnowledgePoints        []string `json:"knowledge_points"`
	TeachingGoals          []string `json:"teaching_goals"`
	KeyDifficulties        []string `json:"key_difficulties"`
	TargetAudience         string   `json:"target_audience"`
	Duration               string   `json:"duration"`
	OutputStyle            string   `json:"output_style"`
	TeachingLogic          string   `json:"teaching_logic"`
	Constraints            []string `json:"constraints"`
	ReferenceMaterialUsage []string `json:"reference_material_usage"`
}

type TaskStateSignals struct {
	LessonTopic            string            `json:"lesson_topic"`
	KnowledgePoints        []string          `json:"knowledge_points"`
	TeachingGoals          []string          `json:"teaching_goals"`
	KeyDifficulties        []string          `json:"key_difficulties"`
	TargetAudience         string            `json:"target_audience"`
	Duration               string            `json:"duration"`
	OutputStyle            string            `json:"output_style"`
	TeachingLogic          string            `json:"teaching_logic"`
	Constraints            []string          `json:"constraints"`
	ReferenceMaterialUsage []string          `json:"reference_material_usage"`
	Provenance             map[string]string `json:"-"`
}

type TaskSlotMetadata struct {
	Status     string `json:"status"`
	Provenance string `json:"provenance"`
	UpdatedAt  int64  `json:"updated_at"`
}

type WorkingMemoryRecord struct {
	SessionID           string                      `json:"session_id"`
	UserID              string                      `json:"user_id"`
	ConversationSummary string                      `json:"conversation_summary"`
	ExtractedElements   TeachingElements            `json:"extracted_elements"`
	RecentTopics        []string                    `json:"recent_topics"`
	UpdatedAt           int64                       `json:"updated_at"`
	TaskState           WorkingTaskState            `json:"task_state"`
	SlotMetadata        map[string]TaskSlotMetadata `json:"slot_metadata,omitempty"`
	Continuity          string                      `json:"continuity"`
}
