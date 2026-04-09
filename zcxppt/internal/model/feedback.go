package model

// Intent represents a parsed user intent for PPT manipulation.
type Intent struct {
	ActionType   string `json:"action_type"`
	TargetPageID string `json:"target_page_id"`
	Instruction  string `json:"instruction"`
	// AnimationStyle 指定动画风格: "slide_in" | "fade" | "zoom" | "draw" | "all"
	// 仅在 action_type 为 "generate_animation" 时使用
	AnimationStyle string `json:"animation_style,omitempty"`
	// GameType 指定小游戏类型: "quiz" | "matching" | "ordering" | "fill_blank" | "random"
	// 仅在 action_type 为 "generate_game" 时使用
	GameType string `json:"game_type,omitempty"`
	// ResultID 仅在 action_type 为 "generate_animation" 或 "generate_game" 时填充
	ResultID string `json:"result_id,omitempty"`
	// WordSection 当 action_type 为 "modify" 时，标记此修改影响的教案章节
	// 格式如 "new_teaching[1]" 表示 new_teaching 的第1个步骤，"teaching_goals" 表示教学目标
	WordSection string `json:"word_section,omitempty"`
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
	// Topic 用于内容多样性生成的上下文（当 intents 包含 generate_animation/game 时使用）
	Topic string `json:"topic,omitempty"`
	// Subject 用于内容多样性生成
	Subject string `json:"subject,omitempty"`
	// KBSummary 知识库摘要（Init 时已获取，Feedback 时透传）
	KBSummary string `json:"kb_summary,omitempty"`
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
	// ContentDiversityResults 承载 generate_animation/generate_game intent 的处理结果
	ContentDiversityResults []ContentDiversityResult `json:"content_diversity_results,omitempty"`
}

// ContentDiversityResult 承载单个 generate_animation 或 generate_game intent 的处理结果。
type ContentDiversityResult struct {
	IntentIndex int    `json:"intent_index"` // 对应 req.Intents 的下标
	ActionType  string `json:"action_type"`  // "generate_animation" | "generate_game"
	ResultID    string `json:"result_id"`    // ContentDiversityService 返回的 result_id
	Status      string `json:"status"`       // "generating" | "failed"
	Error       string `json:"error,omitempty"`
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
	// 三路合并冲突信息
	BaseCode     string `json:"base_code,omitempty"`
	CurrentCode  string `json:"current_code,omitempty"`
	IncomingCode string `json:"incoming_code,omitempty"`
	ConflictDesc string `json:"conflict_desc,omitempty"`
	ConflictOpts []string `json:"conflict_opts,omitempty"`
}

type MergeResult struct {
	PageID          string `json:"page_id"`
	MergeStatus     string `json:"merge_status"` // "auto_resolved" | "ask_human"
	MergedPyCode    string `json:"merged_pycode,omitempty"`
	QuestionForUser string `json:"question_for_user,omitempty"`
	// 三路合并信息：冲突时供挂起展示用
	BaseCode       string   `json:"base_code,omitempty"`       // 基础版本
	CurrentCode    string   `json:"current_code,omitempty"`    // 当前版本
	IncomingCode   string   `json:"incoming_code,omitempty"`   // LLM 新生成的版本
	ConflictDesc   string   `json:"conflict_desc,omitempty"`   // 冲突描述
	ConflictOpts   []string `json:"conflict_opts,omitempty"`   // 供用户选择的选项
}

// ThreeWayMergeInput 是传递给三路合并算法的输入参数。
type ThreeWayMergeInput struct {
	BaseCode      string
	CurrentCode   string
	IncomingCode  string
	BaseTimestamp int64
	PageID        string
}
