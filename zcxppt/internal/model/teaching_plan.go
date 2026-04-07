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
