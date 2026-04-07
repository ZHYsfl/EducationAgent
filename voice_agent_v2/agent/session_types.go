package agent

import (
	"context"
	"sync"

	"github.com/gorilla/websocket"
)

type SessionState int

const (
	StateIdle SessionState = iota
	StateListening
	StateProcessing
	StateSpeaking
)

func (s SessionState) String() string {
	switch s {
	case StateIdle:
		return "idle"
	case StateListening:
		return "listening"
	case StateProcessing:
		return "processing"
	case StateSpeaking:
		return "speaking"
	}
	return "unknown"
}

// PendingQuestion is a conflict question waiting for user resolution.
type PendingQuestion struct {
	TaskID        string
	PageID        string
	BaseTimestamp int64
	QuestionText  string
}

type Session struct {
	conn   *websocket.Conn
	config *Config

	SessionID string
	UserID    string

	// state machine
	state   SessionState
	stateMu sync.RWMutex

	// pipeline
	pipeline       *Pipeline
	pipelineCancel context.CancelFunc
	pipelineMu     sync.Mutex

	// task tracking
	ActiveTaskID     string
	ViewingPageID    string
	LastVADTimestamp int64
	OwnedTasks       map[string]string // task_id → topic
	PendingRequests  map[string]string // request_id → type
	activeTaskMu     sync.RWMutex

	// requirements collection
	Requirements *TaskRequirements
	reqMu        sync.RWMutex

	// conflict resolution
	PendingQuestions map[string]PendingQuestion // context_id → question
	pendingQMu       sync.RWMutex

	// memory compression index
	lastMemoryPushIdx int
	memoryMu          sync.Mutex

	// ws write serialization
	writeCh chan writeItem
	done    chan struct{}
	once    sync.Once

	clients ExternalServices
}

type writeItem struct {
	msgType int
	data    []byte
}
