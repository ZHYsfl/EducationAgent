package agent

// WSMessage represents a WebSocket message exchanged between client and server.
type WSMessage struct {
	Type  string `json:"type"`
	Text  string `json:"text,omitempty"`
	State string `json:"state,omitempty"`

	// 任务相关
	TaskID   string `json:"task_id,omitempty"`
	Topic    string `json:"topic,omitempty"`
	Status   string `json:"status,omitempty"`
	Progress int    `json:"progress,omitempty"`

	// 页面相关
	PageID    string `json:"page_id,omitempty"`
	PageIndex int    `json:"page_index,omitempty"`
	RenderURL string `json:"render_url,omitempty"`

	// 预览
	PageOrder []string        `json:"page_order,omitempty"`
	PagesInfo []PageInfoBrief `json:"pages_info,omitempty"`

	// 冲突
	ContextID string `json:"context_id,omitempty"`
	Question  string `json:"question,omitempty"`

	// 导出
	DownloadURL string `json:"download_url,omitempty"`
	Format      string `json:"format,omitempty"`

	// 表单初始化
	Description string `json:"description,omitempty"`
	TotalPages  int    `json:"total_pages,omitempty"`
	Audience    string `json:"audience,omitempty"`
	GlobalStyle string `json:"global_style,omitempty"`

	// 需求收集
	CollectedFields []string          `json:"collected_fields,omitempty"`
	MissingFields   []string          `json:"missing_fields,omitempty"`
	SummaryText     string            `json:"summary_text,omitempty"`
	Requirements    *TaskRequirements `json:"requirements,omitempty"`
	Confirmed       *bool             `json:"confirmed,omitempty"`
	Modifications   string            `json:"modifications,omitempty"`

	// 错误
	Code    int    `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

// PageInfoBrief contains brief information about a PPT page.
type PageInfoBrief struct {
	PageID     string `json:"page_id"`
	Status     string `json:"status"`
	LastUpdate int64  `json:"last_update"`
	RenderURL  string `json:"render_url"`
}

// PPTMessageRequest represents a message to be sent to the PPT agent.
type PPTMessageRequest struct {
	TaskID    string `json:"task_id"`
	PageID    string `json:"page_id"`
	Priority  string `json:"priority"`
	ContextID string `json:"context_id"`
	TTSText   string `json:"tts_text"`
	MsgType   string `json:"msg_type"`

	RenderURL   string          `json:"render_url,omitempty"`
	PageIndex   int             `json:"page_index,omitempty"`
	DownloadURL string          `json:"download_url,omitempty"`
	Format      string          `json:"format,omitempty"`
	Progress    int             `json:"progress,omitempty"`
	Status      string          `json:"status,omitempty"`
	PageOrder   []string        `json:"page_order,omitempty"`
	PagesInfo   []PageInfoBrief `json:"pages_info,omitempty"`
	ErrorCode   int             `json:"error_code,omitempty"`
}
