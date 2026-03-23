package agent

import (
	clients "voiceagent/internal/clients"
	cfg "voiceagent/internal/config"
	types "voiceagent/internal/types"
)

// ---------------------------------------------------------------------------
// Type aliases from internal/types — package agent code uses these directly.
// ---------------------------------------------------------------------------

type APIResponse = types.APIResponse
type ContextMessage = types.ContextMessage
type ReferenceFileReq = types.ReferenceFileReq
type InitTeachingElements = types.InitTeachingElements
type ReferenceFile = types.ReferenceFile
type PPTInitRequest = types.PPTInitRequest
type PPTInitResponse = types.PPTInitResponse
type PPTFeedbackRequest = types.PPTFeedbackRequest
type Intent = types.Intent
type CanvasStatusResponse = types.CanvasStatusResponse
type PageStatusInfo = types.PageStatusInfo
type KBQueryRequest = types.KBQueryRequest
type KBQueryResponse = types.KBQueryResponse
type RetrievedChunk = types.RetrievedChunk
type MemoryRecallRequest = types.MemoryRecallRequest
type MemoryRecallResponse = types.MemoryRecallResponse
type MemoryEntry = types.MemoryEntry
type WorkingMemory = types.WorkingMemory
type UserProfile = types.UserProfile
type SearchRequest = types.SearchRequest
type SearchResponse = types.SearchResponse
type SearchResult = types.SearchResult
type IngestFromSearchRequest = types.IngestFromSearchRequest
type SearchIngestItem = types.SearchIngestItem
type MemoryExtractRequest = types.MemoryExtractRequest
type ConversationTurn = types.ConversationTurn
type MemoryExtractResponse = types.MemoryExtractResponse
type WorkingMemorySaveRequest = types.WorkingMemorySaveRequest
type VADEvent = types.VADEvent
type FileUploadData = types.FileUploadData

// NewID wraps types.NewID so package agent code can call it without qualification.
func NewID(prefix string) string { return types.NewID(prefix) }

// decodeAPIData wraps clients.DecodeAPIData so package agent (and its tests)
// can call the function without qualification.
func decodeAPIData(raw []byte, out any) error { return clients.DecodeAPIData(raw, out) }

// ---------------------------------------------------------------------------
// Type aliases from internal/clients and internal/config.
// ---------------------------------------------------------------------------

type ExternalServices = clients.ExternalServices
type Config = cfg.Config

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

type PageInfoBrief struct {
	PageID     string `json:"page_id"`
	Status     string `json:"status"`
	LastUpdate int64  `json:"last_update"`
	RenderURL  string `json:"render_url"`
}

type TaskRequirements struct {
	SessionID string `json:"session_id"`
	UserID    string `json:"user_id"`

	Topic           string   `json:"topic"`
	KnowledgePoints []string `json:"knowledge_points"`
	TeachingGoals   []string `json:"teaching_goals"`
	TeachingLogic   string   `json:"teaching_logic"`
	TargetAudience  string   `json:"target_audience"`

	KeyDifficulties   []string `json:"key_difficulties"`
	Duration          string   `json:"duration"`
	TotalPages        int      `json:"total_pages"`
	GlobalStyle       string   `json:"global_style"`
	InteractionDesign string   `json:"interaction_design"`
	OutputFormats     []string `json:"output_formats"`
	AdditionalNotes   string   `json:"additional_notes"`

	ReferenceFiles []ReferenceFileReq `json:"reference_files"`

	CollectedFields []string `json:"collected_fields"`
	Status          string   `json:"status"`
	CreatedAt       int64    `json:"created_at"`
	UpdatedAt       int64    `json:"updated_at"`
}

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
