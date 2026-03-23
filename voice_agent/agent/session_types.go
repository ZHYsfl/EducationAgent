package agent

import (
	"context"
	"sync"

	"github.com/gorilla/websocket"
	cfgpkg "voiceagent/internal/config"
	svcclients "voiceagent/internal/clients"
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
	conn   *websocket.Conn
	config *cfgpkg.Config

	SessionID string
	UserID    string

	state   SessionState
	stateMu sync.RWMutex

	pipeline *Pipeline
	clients  svcclients.ExternalServices

	ActiveTaskID     string
	ViewingPageID    string
	LastVADTimestamp int64
	OwnedTasks       map[string]string // task_id → topic
	activeTaskMu     sync.RWMutex

	Requirements *TaskRequirements
	reqMu        sync.RWMutex

	PendingQuestions map[string]string // context_id -> task_id
	pendingQMu       sync.RWMutex

	pipelineCancel context.CancelFunc
	pipelineMu     sync.Mutex

	// All writes go through this channel to avoid concurrent WS writes
	writeCh chan writeItem

	done chan struct{}
	once sync.Once
}

type writeItem struct {
	msgType int
	data    []byte
}
