package agent

// WSMessage is the JSON envelope for all WebSocket messages.
type WSMessage struct {
	Type  string `json:"type"`
	Text  string `json:"text,omitempty"`
	State string `json:"state,omitempty"`

	// task management
	TaskID       string            `json:"task_id,omitempty"`
	Topic        string            `json:"topic,omitempty"`
	Status       string            `json:"status,omitempty"`
	Progress     int               `json:"progress,omitempty"`
	ActiveTaskID string            `json:"active_task_id,omitempty"`
	Tasks        map[string]string `json:"tasks,omitempty"`

	// page
	PageID    string `json:"page_id,omitempty"`
	PageIndex int    `json:"page_index,omitempty"`
	RenderURL string `json:"render_url,omitempty"`

	// ppt preview
	PageOrder []string        `json:"page_order,omitempty"`
	PagesInfo []PageInfoBrief `json:"pages_info,omitempty"`

	// conflict
	ContextID string `json:"context_id,omitempty"`
	Question  string `json:"question,omitempty"`

	// export
	DownloadURL string `json:"download_url,omitempty"`
	Format      string `json:"format,omitempty"`

	// form init
	Description string `json:"description,omitempty"`
	TotalPages  int    `json:"total_pages,omitempty"`
	Audience    string `json:"audience,omitempty"`
	GlobalStyle string `json:"global_style,omitempty"`

	// requirements collection
	CollectedFields []string          `json:"collected_fields,omitempty"`
	MissingFields   []string          `json:"missing_fields,omitempty"`
	SummaryText     string            `json:"summary_text,omitempty"`
	Requirements    *TaskRequirements `json:"requirements,omitempty"`
	Confirmed       *bool             `json:"confirmed,omitempty"`
	Modifications   string            `json:"modifications,omitempty"`

	// files (add_reference_files)
	Files []FileAttachment `json:"files,omitempty"`

	// error
	Code    int    `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

type PageInfoBrief struct {
	PageID     string `json:"page_id"`
	Status     string `json:"status"`
	LastUpdate int64  `json:"last_update"`
	RenderURL  string `json:"render_url"`
}

type FileAttachment struct {
	FileID      string `json:"file_id"`
	FileURL     string `json:"file_url"`
	FileType    string `json:"file_type"`
	Instruction string `json:"instruction"`
}

// PPTMessageRequest is the body of POST /api/v1/voice/ppt_message from backend services.
type PPTMessageRequest struct {
	TaskID      string          `json:"task_id"`
	SessionID   string          `json:"session_id"`
	RequestID   string          `json:"request_id,omitempty"`
	EventType   string          `json:"event_type"`
	PageID      string          `json:"page_id,omitempty"`
	ContextID   string          `json:"context_id,omitempty"`
	Priority    string          `json:"priority,omitempty"`
	TTSText     string          `json:"tts_text,omitempty"`
	Summary     string          `json:"summary,omitempty"`
	Status      string          `json:"status,omitempty"`
	Progress    int             `json:"progress,omitempty"`
	RenderURL   string          `json:"render_url,omitempty"`
	PageIndex   int             `json:"page_index,omitempty"`
	PageOrder   []string        `json:"page_order,omitempty"`
	PagesInfo   []PageInfoBrief `json:"pages_info,omitempty"`
	DownloadURL string          `json:"download_url,omitempty"`
	Format      string          `json:"format,omitempty"`
	ErrorCode   int             `json:"error_code,omitempty"`
	ConflictResolved bool       `json:"conflict_resolved,omitempty"`
}
