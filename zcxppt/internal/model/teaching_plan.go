package model

// TeachingPlanRequest for Word教案生成.
type TeachingPlanRequest struct {
	TaskID      string `json:"task_id"`
	Topic       string `json:"topic"`
	Subject     string `json:"subject"`
	Description string `json:"description"`
	Audience    string `json:"audience"`
	Duration    string `json:"duration"`

	TeachingElements *InitTeachingElements `json:"teaching_elements,omitempty"`
	StyleGuide       string                `json:"style_guide,omitempty"`
	// PlanContentJSON 当从 PyCode 生成教案时传入（Feedback 联动更新场景）
	PlanContentJSON string `json:"plan_content_json,omitempty"`
	// PageContents 当从 PyCode 生成教案时传入，格式: []PageContent
	PageContents []PageContent `json:"page_contents,omitempty"`
	// BaseTimestamp 当进行教案更新时传入，用于三路合并的基线时间戳
	BaseTimestamp int64 `json:"base_timestamp,omitempty"`
	// Action 指定操作类型: "generate"=首次生成, "update_chapter"=更新章节, "regenerate"=全量覆盖
	Action string `json:"action,omitempty"`
	// UpdateHint 当 action=update_chapter 时指定要更新的章节
	UpdateHint *ChapterUpdateHint `json:"update_hint,omitempty"`
}

// PageContent 描述一页 PPT 的文本内容，用于生成/更新教案。
type PageContent struct {
	PageID    string `json:"page_id"`
	PageIndex int    `json:"page_index"` // 1-based 页码
	Title     string `json:"title"`
	BodyText  string `json:"body_text"` // 提取的正文文本
	PyCode    string `json:"py_code,omitempty"`
}

// ChapterUpdateHint 描述对教案某个章节的更新意图。
type ChapterUpdateHint struct {
	ChapterKey  string `json:"chapter_key"`   // e.g. "new_teaching[1]", "teaching_goals"
	PageID      string `json:"page_id"`      // 触发更新的页面 ID
	RawText     string `json:"raw_text"`      // 用户的原始修改指令
	Intents     []Intent `json:"intents,omitempty"`
}

// PlanContent 是教案内容的完整 JSON 结构，也是三路合并的单位。
type PlanContent struct {
	Title              string          `json:"title"`
	Subject            string          `json:"subject"`
	Grade              string          `json:"grade"`
	Duration           string          `json:"duration"`
	TeachingGoals      []string        `json:"teaching_goals"`
	TeachingFocus      []string        `json:"teaching_focus"`
	TeachingDifficulties []string      `json:"teaching_difficulties"`
	TeachingMethods    []string        `json:"teaching_methods"`
	TeachingAids       []string        `json:"teaching_aids,omitempty"`
	TeachingProcess    TeachingProcess `json:"teaching_process"`
	ClassroomActivities []Activity     `json:"classroom_activities,omitempty"`
	TeachingReflection string          `json:"teaching_reflection,omitempty"`
}

// TeachingProcess 包含教学过程的各个阶段。
type TeachingProcess struct {
	WarmUp     *ProcessStep `json:"warm_up,omitempty"`
	Introduction *ProcessStep `json:"introduction,omitempty"`
	NewTeaching []TeachStep  `json:"new_teaching"`    // 每个 TeachStep 对应 PPT 的一个内容页
	Practice   *ProcessStep `json:"practice,omitempty"`
	Summary    *ProcessStep `json:"summary,omitempty"`
	Homework   []string     `json:"homework,omitempty"`
}

// ProcessStep 描述一个通用教学环节（热身/导入/练习/总结）。
type ProcessStep struct {
	Duration string `json:"duration,omitempty"`
	Content  string `json:"content"`
}

// TeachStep 描述一个新授环节步骤，包含页码映射。
type TeachStep struct {
	Step      int       `json:"step"`               // 步骤编号（1-based）
	Title     string    `json:"title"`
	Duration  string    `json:"duration,omitempty"`
	Content   string    `json:"content"`
	Activities []string  `json:"activities,omitempty"`
	// PPT 页面映射：表示此步骤对应 PPT 的哪些页面
	MappedPages []string `json:"mapped_pages,omitempty"` // pageID 列表
}

// Activity 描述一个课堂活动。
type Activity struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Duration    string `json:"duration,omitempty"`
	Description string `json:"description,omitempty"`
	Purpose     string `json:"purpose,omitempty"`
}

// TeachingPlanResponse is the result of teaching plan generation.
type TeachingPlanResponse struct {
	TaskID      string `json:"task_id"`
	PlanID      string `json:"plan_id"`
	Status      string `json:"status"` // generating | completed | failed
	DownloadURL string `json:"download_url,omitempty"`
	PlanContent string `json:"plan_content,omitempty"` // 纯文本预览
	Error       string `json:"error,omitempty"`
}

// TeachingPlanStatus is the polling status response.
type TeachingPlanStatus struct {
	PlanID      string `json:"plan_id"`
	Status      string `json:"status"`
	DownloadURL string `json:"download_url,omitempty"`
	PlanContent string `json:"plan_content,omitempty"`
	Error       string `json:"error,omitempty"`
}

// ContentDiversityRequest for 动画创意 / 互动小游戏生成.
type ContentDiversityRequest struct {
	TaskID     string `json:"task_id"`
	PageID     string `json:"page_id,omitempty"`
	Topic      string `json:"topic"`
	Subject    string `json:"subject"`
	PageCode   string `json:"page_code,omitempty"`
	KBSummary  string `json:"kb_summary,omitempty"`

	// Type 指定生成内容的类型: "animation" | "game" | "both"
	// 默认为 "both"
	Type string `json:"type,omitempty"`

	// GameType 指定小游戏的类型: "quiz" | "matching" | "ordering" | "fill_blank" | "random"
	// 默认为 "quiz"
	GameType string `json:"game_type,omitempty"`

	// AnimationStyle 指定动画风格: "slide_in" | "fade" | "zoom" | "draw" | "all"
	// 默认为 "all"
	AnimationStyle string `json:"animation_style,omitempty"`
}

// ContentDiversityResponse is the result of content diversity generation.
type ContentDiversityResponse struct {
	TaskID     string             `json:"task_id"`
	ResultID   string             `json:"result_id"`
	Status     string             `json:"status"` // generating | completed | failed
	Animations []AnimationResult  `json:"animations,omitempty"`
	Games      []GameResult       `json:"games,omitempty"`
	Error      string             `json:"error,omitempty"`
}

// AnimationResult represents a single animation creative.
type AnimationResult struct {
	AnimationID   string   `json:"animation_id"`
	Title         string   `json:"title"`
	Description   string   `json:"description"`
	HTMLContent   string   `json:"html_content,omitempty"`
	HTMLURL       string   `json:"html_url,omitempty"`
	GIFPath       string   `json:"gif_path,omitempty"`
	MP4Path       string   `json:"mp4_path,omitempty"`
	GIFURL        string   `json:"gif_url,omitempty"`
	MP4URL        string   `json:"mp4_url,omitempty"`
	ExportFormats []string `json:"export_formats,omitempty"` // ["html5", "gif", "mp4"]
}

// GameResult represents a single interactive game.
type GameResult struct {
	GameID     string `json:"game_id"`
	Title      string `json:"title"`
	GameType   string `json:"game_type"`
	HTMLContent string `json:"html_content,omitempty"`
	HTMLURL     string `json:"html_url,omitempty"`
	ExportFormats []string `json:"export_formats,omitempty"` // ["html5", "gif"]
}

// ExportContentRequest for exporting animation/game in specific format.
type ExportContentRequest struct {
	TaskID      string `json:"task_id"`
	ResultID    string `json:"result_id"`
	ContentType string `json:"content_type"` // "animation" | "game"
	Format      string `json:"format"`       // "html5" | "gif" | "mp4"
}

// ExportContentResponse is the result of content export.
type ExportContentResponse struct {
	ResultID    string `json:"result_id"`
	Format      string `json:"format"`
	Status      string `json:"status"`
	DownloadURL string `json:"download_url,omitempty"`
	Error       string `json:"error,omitempty"`
}

// IntegrationRequest for embedding animation/game into PPT page.
type IntegrationRequest struct {
	TaskID       string   `json:"task_id"`
	PageID       string   `json:"page_id"`
	AnimationIDs []string `json:"animation_ids,omitempty"`
	GameIDs      []string `json:"game_ids,omitempty"`
	Position     string   `json:"position,omitempty"` // "footer" | "sidebar" | "fullscreen"
}

// IntegrationResponse is the result of integration.
type IntegrationResponse struct {
	TaskID    string   `json:"task_id"`
	PageID    string   `json:"page_id"`
	Status    string   `json:"status"`
	UpdatedPyCode string `json:"updated_py_code,omitempty"`
	Error     string   `json:"error,omitempty"`
}
