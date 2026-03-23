package task

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/google/uuid"
)

func UTCMS() int64 {
	return time.Now().UTC().UnixMilli()
}

type Page struct {
	PageID     string `json:"page_id"`
	SlideIndex int    `json:"slide_index"`
	Status     string `json:"status"`
	RenderURL  string `json:"render_url"`
	PyCode     string `json:"py_code"`
	Version    int    `json:"version"`
	UpdatedAt  int64  `json:"updated_at"`
}

// SuspendedPage 对齐 Python SuspendedPageState。
type SuspendedPage struct {
	PageID           string           `json:"page_id"`
	ContextID        string           `json:"context_id"`
	Reason           string           `json:"reason"`
	QuestionForUser  string           `json:"question_for_user"`
	SuspendedAt      int64            `json:"suspended_at"`
	LastAskedAt      int64            `json:"last_asked_at"`
	AskCount         int              `json:"ask_count"`
	PendingFeedbacks []map[string]any `json:"pending_feedbacks"`
}

// PageMerge 对齐 Python PageMergeState（IsRunning 仅内存，持久化时写 false）。
type PageMerge struct {
	IsRunning          bool              `json:"-"`
	PendingIntents     []map[string]any  `json:"pending_intents"`
	ChainBaselinePages map[string]string `json:"chain_baseline_pages"`
	ChainVADTimestamp  int64             `json:"chain_vad_timestamp"`
}

type Task struct {
	TaskID               string
	UserID               string
	Topic                string
	Description          string
	TotalPages           int
	Audience             string
	GlobalStyle          string
	SessionID            string
	Status               string
	Version              int
	LastUpdate           int64
	PageOrder            []string
	CurrentViewingPageID string
	Pages                map[string]*Page
	OutputPptxPath       string
	ReferenceFiles       []map[string]any
	TeachingElements     json.RawMessage
	ExtraContext         string
	RetrievalTrace       json.RawMessage
	ContextInjections    []map[string]any
	PendingFeedbackLines []string
	OpenConflictContexts map[string]string
	SuspendedPages       map[string]*SuspendedPage
	PageMerges           map[string]*PageMerge
}

func NewTask() *Task {
	return &Task{
		Pages:                make(map[string]*Page),
		OpenConflictContexts: make(map[string]string),
		SuspendedPages:       make(map[string]*SuspendedPage),
		PageMerges:           make(map[string]*PageMerge),
		PendingFeedbackLines: make([]string, 0),
		PageOrder:            make([]string, 0),
		ReferenceFiles:       make([]map[string]any, 0),
		ContextInjections:    make([]map[string]any, 0),
		Status:               "pending",
		Version:              1,
		LastUpdate:           UTCMS(),
	}
}

func NewTaskID() string   { return "task_" + uuid.New().String() }
func NewPageID() string   { return "page_" + uuid.New().String() }
func NewExportID() string { return "file_" + uuid.New().String() }

type Export struct {
	ExportID    string
	TaskID      string
	Format      string
	Status      string
	DownloadURL string
	FileSize    int64
	LastUpdate  int64
}

type Store struct {
	mu      sync.RWMutex
	tasks   map[string]*Task
	exports map[string]*Export
	RunsDir string
}

func NewStore(runsDir string) *Store {
	return &Store{
		tasks:   make(map[string]*Task),
		exports: make(map[string]*Export),
		RunsDir: runsDir,
	}
}

func (s *Store) TryGet(taskID string) *Task {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tasks[taskID]
}

func (s *Store) Set(taskID string, t *Task) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks[taskID] = t
}

func (s *Store) TaskDir(taskID string) string {
	return s.RunsDir + "/" + taskID
}

func (s *Store) GetExport(id string) *Export {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.exports[id]
}

func (s *Store) SetExport(e *Export) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.exports[e.ExportID] = e
}

// TakePendingFeedbackLines 取出并清空队列（对齐 Python take_pending_feedback_lines）。
func (s *Store) TakePendingFeedbackLines(taskID string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.tasks[taskID]
	if t == nil {
		return nil
	}
	lines := t.PendingFeedbackLines
	t.PendingFeedbackLines = nil
	return lines
}

// AppendPendingFeedback 在生成中排队说明（对齐 Python append_pending_feedback）。
func (s *Store) AppendPendingFeedback(taskID string, lines []string) {
	if len(lines) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.tasks[taskID]
	if t == nil {
		return
	}
	t.PendingFeedbackLines = append(t.PendingFeedbackLines, lines...)
}
