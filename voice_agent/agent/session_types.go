package agent

import (
	"context"
	"sync"

	svcclients "voiceagent/internal/clients"
	cfgpkg "voiceagent/internal/config"

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

type Session struct {
	// ========== Core Dependencies ==========
	conn   *websocket.Conn
	config *cfgpkg.Config

	// ========== Identity ==========
	SessionID string
	UserID    string

	// ========== State Management (protected by stateMu) ==========
	state   SessionState
	stateMu sync.RWMutex

	// ========== Processing Pipeline ==========
	pipeline *Pipeline
	clients  svcclients.ExternalServices

	// ========== Task Management (protected by activeTaskMu) ==========
	ActiveTaskID     string
	ViewingPageID    string
	LastVADTimestamp int64
	OwnedTasks       map[string]string // task_id → topic
	PendingRequests  map[string]string // request_id → type ("search" or "kb")
	activeTaskMu     sync.RWMutex

	// ========== Requirements Collection (protected by reqMu) ==========
	Requirements *TaskRequirements
	reqMu        sync.RWMutex

	// ========== Conflict Resolution (protected by pendingQMu) ==========
	PendingQuestions map[string]string // context_id -> task_id
	pendingQMu       sync.RWMutex

	// ========== Pipeline Lifecycle (protected by pipelineMu) ==========
	pipelineCancel context.CancelFunc
	pipelineMu     sync.Mutex

	// ========== WebSocket Communication ==========
	// All writes go through this channel to avoid concurrent WS writes
	writeCh chan writeItem

	// ========== Shutdown Coordination ==========
	done chan struct{}
	once sync.Once
}

type writeItem struct {
	msgType int
	data    []byte
}
