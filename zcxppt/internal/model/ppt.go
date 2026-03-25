package model

type PPTInitRequest struct {
	UserID      string `json:"user_id"`
	Topic       string `json:"topic"`
	Description string `json:"description"`
	TotalPages  int    `json:"total_pages"`
	Audience    string `json:"audience"`
	GlobalStyle string `json:"global_style"`
	SessionID   string `json:"session_id"`

	TeachingElements *InitTeachingElements `json:"teaching_elements,omitempty"`
	ReferenceFiles   []ReferenceFile       `json:"reference_files,omitempty"`
}

type InitTeachingElements struct {
	KnowledgePoints   []string `json:"knowledge_points"`
	TeachingGoals     []string `json:"teaching_goals"`
	TeachingLogic     string   `json:"teaching_logic"`
	KeyDifficulties   []string `json:"key_difficulties"`
	Duration          string   `json:"duration"`
	InteractionDesign string   `json:"interaction_design"`
	OutputFormats     []string `json:"output_formats"`
}

type ReferenceFile struct {
	FileID      string `json:"file_id"`
	FileURL     string `json:"file_url"`
	FileType    string `json:"file_type"`
	Instruction string `json:"instruction"`
}

type PageStatusInfo struct {
	PageID     string `json:"page_id"`
	Status     string `json:"status"`
	LastUpdate int64  `json:"last_update"`
	RenderURL  string `json:"render_url"`
}

type CanvasStatusResponse struct {
	TaskID                string           `json:"task_id"`
	PageOrder             []string         `json:"page_order"`
	CurrentViewingPageID  string           `json:"current_viewing_page_id"`
	PagesInfo             []PageStatusInfo `json:"pages_info"`
}

type PageRenderResponse struct {
	PageID    string `json:"page_id"`
	TaskID    string `json:"task_id"`
	Status    string `json:"status"`
	RenderURL string `json:"render_url"`
	PyCode    string `json:"py_code"`
	Version   int    `json:"version"`
	UpdatedAt int64  `json:"updated_at"`
}
