package model

type Intent struct {
	ActionType   string `json:"action_type"`
	TargetPageID string `json:"target_page_id"`
	Instruction  string `json:"instruction"`
}

type FeedbackRequest struct {
	TaskID           string               `json:"task_id"`
	BaseTimestamp    int64                `json:"base_timestamp"`
	ViewingPageID    string               `json:"viewing_page_id"`
	ReplyToContextID string               `json:"reply_to_context_id"`
	RawText          string               `json:"raw_text"`
	Intents          []Intent             `json:"intents"`
	// ReferenceFiles carries the original reference files into the feedback loop so that
	// the LLM can re-fuse and merge them against the current page code + instruction.
	ReferenceFiles []ReferenceFile `json:"reference_files,omitempty"`
	// RefFusionResult pre-computed fusion result from ReferenceFiles (optional;
	// if empty, the runtime will compute it on the fly).
	RefFusionResult *FusionResultPayload `json:"ref_fusion_result,omitempty"`
}

// FusionResultPayload is the serialized FusionResult carried in FeedbackRequest.
type FusionResultPayload struct {
	ExtractedText string   `json:"extracted_text,omitempty"`
	StyleGuide    string   `json:"style_guide,omitempty"`
	TopicHints    []string `json:"topic_hints,omitempty"`
}

type FeedbackResponse struct {
	AcceptedIntents int  `json:"accepted_intents"`
	Queued          bool `json:"queued"`
}

type PendingFeedback struct {
	TaskID         string            `json:"task_id"`
	PageID         string            `json:"page_id"`
	BaseTimestamp  int64             `json:"base_timestamp"`
	RawText        string            `json:"raw_text"`
	Intents        []Intent          `json:"intents"`
	CreatedAt      int64             `json:"created_at"`
	ReferenceFiles []ReferenceFile   `json:"reference_files,omitempty"`
}

type SuspendState struct {
	TaskID      string `json:"task_id"`
	PageID      string `json:"page_id"`
	ContextID   string `json:"context_id"`
	Question    string `json:"question"`
	RetryCount  int    `json:"retry_count"`
	ExpiresAt   int64  `json:"expires_at"`
	CreatedAt   int64  `json:"created_at"`
	Resolved    bool   `json:"resolved"`
}

type MergeResult struct {
	PageID          string `json:"page_id"`
	MergeStatus     string `json:"merge_status"`
	MergedPyCode    string `json:"merged_pycode,omitempty"`
	QuestionForUser string `json:"question_for_user,omitempty"`
}
