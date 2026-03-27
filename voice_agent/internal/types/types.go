package types

import (
	"encoding/json"

	"github.com/google/uuid"
)

type APIResponse struct {
	Code    int             `json:"code"`
	Message string          `json:"message,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func NewID(prefix string) string {
	return prefix + uuid.NewString()
}

type ContextMessage struct {
	ID         string            `json:"id"`
	ActionType string            `json:"action_type"`
	Priority   string            `json:"priority"`
	MsgType    string            `json:"msg_type"`
	Content    string            `json:"content"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	Timestamp  int64             `json:"timestamp"`
}

type ReferenceFileReq struct {
	FileID      string `json:"file_id"`
	FileURL     string `json:"file_url"`
	FileType    string `json:"file_type"`
	Instruction string `json:"instruction"`
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

type PPTInitRequest struct {
	UserID           string                `json:"user_id"`
	Topic            string                `json:"topic"`
	Description      string                `json:"description"`
	TotalPages       int                   `json:"total_pages"`
	Audience         string                `json:"audience"`
	GlobalStyle      string                `json:"global_style"`
	SessionID        string                `json:"session_id"`
	TeachingElements *InitTeachingElements `json:"teaching_elements,omitempty"`
	ReferenceFiles   []ReferenceFile       `json:"reference_files,omitempty"`
}

type PPTInitResponse struct {
	TaskID string `json:"task_id"`
	Status string `json:"status"`
}

type PPTFeedbackRequest struct {
	TaskID        string   `json:"task_id"`
	BaseTimestamp int64    `json:"base_timestamp"`
	ViewingPageID string   `json:"viewing_page_id"`
	RawText       string   `json:"raw_text"`
	Intents       []Intent `json:"intents"`
}

type Intent struct {
	ActionType   string `json:"action_type"`
	TargetPageID string `json:"target_page_id"`
	Instruction  string `json:"instruction"`
}

type CanvasStatusResponse struct {
	TaskID               string           `json:"task_id"`
	PageOrder            []string         `json:"page_order"`
	CurrentViewingPageID string           `json:"current_viewing_page_id"`
	PagesInfo            []PageStatusInfo `json:"pages_info"`
}

type PageStatusInfo struct {
	PageID     string `json:"page_id"`
	Status     string `json:"status"`
	LastUpdate int64  `json:"last_update"`
	RenderURL  string `json:"render_url"`
}

type KBQueryRequest struct {
	Subject        string  `json:"subject,omitempty"`
	UserID         string  `json:"user_id"`
	Query          string  `json:"query"`
	TopK           int     `json:"top_k"`
	ScoreThreshold float64 `json:"score_threshold,omitempty"`
}

type KBQueryResponse struct {
	Summary string `json:"summary"`
}

type MemoryRecallRequest struct {
	UserID    string `json:"user_id"`
	SessionID string `json:"session_id,omitempty"`
	Query     string `json:"query"`
	TopK      int    `json:"top_k"`
}

type MemoryRecallResponse struct {
	Facts          []MemoryEntry  `json:"facts"`
	Preferences    []MemoryEntry  `json:"preferences"`
	WorkingMemory  *WorkingMemory `json:"working_memory"`
	ProfileSummary string         `json:"profile_summary"`
}

type MemoryEntry struct {
	Key        string  `json:"key,omitempty"`
	Content    string  `json:"content,omitempty"`
	Value      string  `json:"value,omitempty"`
	Confidence float64 `json:"confidence,omitempty"`
}

type WorkingMemory struct {
	SessionID           string            `json:"session_id,omitempty"`
	UserID              string            `json:"user_id,omitempty"`
	ConversationSummary string            `json:"conversation_summary,omitempty"`
	ExtractedElements   map[string]any    `json:"extracted_elements,omitempty"`
	RecentTopics        []string          `json:"recent_topics,omitempty"`
	Metadata            map[string]string `json:"metadata,omitempty"`
}

type UserProfile struct {
	UserID            string            `json:"user_id"`
	DisplayName       string            `json:"display_name,omitempty"`
	Subject           string            `json:"subject,omitempty"`
	School            string            `json:"school,omitempty"`
	TeachingStyle     string            `json:"teaching_style,omitempty"`
	ContentDepth      string            `json:"content_depth,omitempty"`
	Preferences       map[string]string `json:"preferences,omitempty"`
	VisualPreferences map[string]string `json:"visual_preferences,omitempty"`
	HistorySummary    string            `json:"history_summary,omitempty"`
	LastActiveAt      int64             `json:"last_active_at,omitempty"`
}

type SearchRequest struct {
	RequestID  string `json:"request_id,omitempty"`
	UserID     string `json:"user_id"`
	Query      string `json:"query"`
	MaxResults int    `json:"max_results,omitempty"`
	Language   string `json:"language,omitempty"`
}

type SearchResponse struct {
	RequestID string         `json:"request_id"`
	Status    string         `json:"status"`
	Results   []SearchResult `json:"results,omitempty"`
	Summary   string         `json:"summary"`
	Duration  int64          `json:"duration,omitempty"`
}

type SearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
	Source  string `json:"source"`
}

type IngestFromSearchRequest struct {
	UserID       string             `json:"user_id"`
	CollectionID string             `json:"collection_id,omitempty"`
	Items        []SearchIngestItem `json:"items"`
}

type SearchIngestItem struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Content string `json:"content"`
	Source  string `json:"source"`
}

type MemoryExtractRequest struct {
	UserID    string             `json:"user_id"`
	SessionID string             `json:"session_id"`
	Messages  []ConversationTurn `json:"messages"`
}

type ConversationTurn struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type MemoryExtractResponse struct {
	ExtractedFacts       []string `json:"extracted_facts,omitempty"`
	ExtractedPreferences []string `json:"extracted_preferences,omitempty"`
	ConversationSummary  string   `json:"conversation_summary,omitempty"`
}

type WorkingMemorySaveRequest struct {
	SessionID           string         `json:"session_id"`
	UserID              string         `json:"user_id"`
	ConversationSummary string         `json:"conversation_summary,omitempty"`
	ExtractedElements   map[string]any `json:"extracted_elements,omitempty"`
	RecentTopics        []string       `json:"recent_topics,omitempty"`
}

type VADEvent struct {
	TaskID        string `json:"task_id"`
	Timestamp     int64  `json:"timestamp"`
	ViewingPageID string `json:"viewing_page_id"`
}

type FileUploadData struct {
	FileID     string `json:"file_id"`
	Filename   string `json:"filename"`
	FileType   string `json:"file_type"`
	FileSize   int64  `json:"file_size"`
	StorageURL string `json:"storage_url"`
	Purpose    string `json:"purpose"`
}
