package agent

// mock.go — test helpers exported from package agent so that black-box tests
// in tests/agent (package agent_test) can construct Session and Pipeline
// instances that require direct access to unexported fields.
//
// These helpers are only used by tests. The types and functions here are
// exported so they are visible from tests/agent (package agent_test).

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	adaptivepkg "voiceagent/internal/adaptive"
	hist "voiceagent/internal/history"
	"voiceagent/internal/protocol"
)

// compile-time check: MockServices must satisfy ExternalServices
var _ ExternalServices = (*MockServices)(nil)

// ---------------------------------------------------------------------------
// MockServices implements ExternalServices.
// Each method delegates to an injectable func field; if nil, returns zero/nil.
// ---------------------------------------------------------------------------

// MockServices is a configurable mock implementation of ExternalServices.
type MockServices struct {
	QueryKBFn         func(ctx context.Context, req KBQueryRequest) (KBQueryResponse, error)
	RecallMemoryFn    func(ctx context.Context, req MemoryRecallRequest) (MemoryRecallResponse, error)
	PushContextFn     func(ctx context.Context, req PushContextRequest) error
	SearchWebFn       func(ctx context.Context, req SearchRequest) (SearchResponse, error)
	InitPPTFn         func(ctx context.Context, req PPTInitRequest) (PPTInitResponse, error)
	SendFeedbackFn    func(ctx context.Context, req PPTFeedbackRequest) error
	GetCanvasStatusFn func(ctx context.Context, taskID string) (CanvasStatusResponse, error)
	UploadFileFn      func(r *http.Request) (json.RawMessage, error)
	NotifyVADEventFn  func(ctx context.Context, event VADEvent) error

	Mu            sync.Mutex
	FeedbackCalls []PPTFeedbackRequest
	VADEventCalls []VADEvent
	PushCtxCalls  []PushContextRequest
	InitPPTCalls  []PPTInitRequest
}

func (m *MockServices) QueryKB(ctx context.Context, req KBQueryRequest) (KBQueryResponse, error) {
	if m.QueryKBFn != nil {
		return m.QueryKBFn(ctx, req)
	}
	return KBQueryResponse{}, nil
}
func (m *MockServices) RecallMemory(ctx context.Context, req MemoryRecallRequest) (MemoryRecallResponse, error) {
	if m.RecallMemoryFn != nil {
		return m.RecallMemoryFn(ctx, req)
	}
	return MemoryRecallResponse{}, nil
}
func (m *MockServices) PushContext(ctx context.Context, req PushContextRequest) error {
	m.Mu.Lock()
	m.PushCtxCalls = append(m.PushCtxCalls, req)
	m.Mu.Unlock()
	if m.PushContextFn != nil {
		return m.PushContextFn(ctx, req)
	}
	return nil
}
func (m *MockServices) SearchWeb(ctx context.Context, req SearchRequest) (SearchResponse, error) {
	if m.SearchWebFn != nil {
		return m.SearchWebFn(ctx, req)
	}
	return SearchResponse{}, nil
}
func (m *MockServices) InitPPT(ctx context.Context, req PPTInitRequest) (PPTInitResponse, error) {
	m.Mu.Lock()
	m.InitPPTCalls = append(m.InitPPTCalls, req)
	m.Mu.Unlock()
	if m.InitPPTFn != nil {
		return m.InitPPTFn(ctx, req)
	}
	return PPTInitResponse{TaskID: "task_mock_001"}, nil
}
func (m *MockServices) SendFeedback(ctx context.Context, req PPTFeedbackRequest) error {
	m.Mu.Lock()
	m.FeedbackCalls = append(m.FeedbackCalls, req)
	m.Mu.Unlock()
	if m.SendFeedbackFn != nil {
		return m.SendFeedbackFn(ctx, req)
	}
	return nil
}
func (m *MockServices) GetCanvasStatus(ctx context.Context, taskID string) (CanvasStatusResponse, error) {
	if m.GetCanvasStatusFn != nil {
		return m.GetCanvasStatusFn(ctx, taskID)
	}
	return CanvasStatusResponse{TaskID: taskID}, nil
}
func (m *MockServices) UploadFile(r *http.Request) (json.RawMessage, error) {
	if m.UploadFileFn != nil {
		return m.UploadFileFn(r)
	}
	return json.RawMessage(`{"file_id":"f_mock"}`), nil
}
func (m *MockServices) NotifyVADEvent(ctx context.Context, event VADEvent) error {
	m.Mu.Lock()
	m.VADEventCalls = append(m.VADEventCalls, event)
	m.Mu.Unlock()
	if m.NotifyVADEventFn != nil {
		return m.NotifyVADEventFn(ctx, event)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Test helper constructors
// ---------------------------------------------------------------------------

// NewTestConfig returns a minimal *Config suitable for tests.
func NewTestConfig() *Config {
	return &Config{
		ServerPort:         9000,
		SystemPrompt:       "你是测试助手。",
		AdaptiveSizesFile:  "",
		TokenBudget:        50,
		FillerInterval:     100,
		FillerPhrases:      []string{"稍等"},
		MaxFillers:         3,
		ThinkDeltaChars:    7,
		ThinkMinIntervalMS: 800,
		ThinkActionGuardMS: 1500,
	}
}

// NewTestSession creates a minimal Session for unit tests (no real WebSocket).
func NewTestSession(clients ExternalServices) *Session {
	cfg := NewTestConfig()
	s := &Session{
		config:           cfg,
		SessionID:        "sess_test_001",
		UserID:           "user_test",
		state:            StateIdle,
		writeCh:          make(chan writeItem, 4096),
		done:             make(chan struct{}),
		clients:          clients,
		OwnedTasks:       make(map[string]string),
		PendingQuestions: make(map[string]PendingQuestion),
	}
	return s
}

// NewTestPipeline creates a minimal Pipeline for unit tests.
func NewTestPipeline(session *Session, clients ExternalServices) *Pipeline {
	p := &Pipeline{
		session:           session,
		config:            session.config,
		clients:           clients,
		adaptive:          adaptivepkg.NewAdaptiveController(adaptivepkg.DefaultChannelSizes()),
		contextQueue:      make(chan ContextMessage, 64),
		highPriorityQueue: make(chan ContextMessage, 16),
		history:           hist.NewConversationHistory("test prompt"),
		parser:            protocol.NewParser(),
	}
	p.contextMgr = NewContextManager(session, p)
	session.pipeline = p
	return p
}

// MockTTS is a no-op TTS provider for tests.
type MockTTS struct{}

// Synthesize implements tts.TTSProvider.
func (m *MockTTS) Synthesize(ctx context.Context, text string, bufSize int) (<-chan []byte, error) {
	ch := make(chan []byte)
	close(ch)
	return ch, nil
}

// NewTestPipelineWithTTS creates a pipeline with a mock TTS for testing.
func NewTestPipelineWithTTS(session *Session, clients ExternalServices) *Pipeline {
	p := NewTestPipeline(session, clients)
	p.ttsClient = &MockTTS{}
	return p
}

// NewBarePipelineForTest returns a minimal pipeline (adaptive only) for black-box tests
// that need SetPipeline without full NewPipeline wiring.
func NewBarePipelineForTest(s *Session) *Pipeline {
	p := &Pipeline{
		session:  s,
		adaptive: adaptivepkg.NewAdaptiveController(adaptivepkg.DefaultChannelSizes()),
	}
	s.SetPipeline(p)
	return p
}

// DrainNextWriteItem pops one item from the session write channel (for tests).
func DrainNextWriteItem(s *Session) (msgType int, data []byte, ok bool) {
	select {
	case item := <-s.writeCh:
		return item.msgType, item.data, true
	default:
		return 0, nil, false
	}
}

// DrainWriteCh reads all pending WSMessage values from the session's write channel.
func DrainWriteCh(s *Session) []WSMessage {
	var msgs []WSMessage
	for {
		select {
		case item := <-s.writeCh:
			var msg WSMessage
			if err := json.Unmarshal(item.data, &msg); err == nil {
				msgs = append(msgs, msg)
			}
		default:
			return msgs
		}
	}
}

// FindWSMessage finds the first WSMessage with the given type from the list.
func FindWSMessage(msgs []WSMessage, msgType string) (WSMessage, bool) {
	for _, m := range msgs {
		if m.Type == msgType {
			return m, true
		}
	}
	return WSMessage{}, false
}

// WaitForFeedback waits for SendFeedback calls with a short timeout.
func WaitForFeedback(m *MockServices, count int) []PPTFeedbackRequest {
	for i := 0; i < 200; i++ {
		m.Mu.Lock()
		n := len(m.FeedbackCalls)
		m.Mu.Unlock()
		if n >= count {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	m.Mu.Lock()
	defer m.Mu.Unlock()
	return m.FeedbackCalls
}

// WaitForPushCtx waits for PushContext calls.
func WaitForPushCtx(m *MockServices, count int) []PushContextRequest {
	for i := 0; i < 200; i++ {
		m.Mu.Lock()
		n := len(m.PushCtxCalls)
		m.Mu.Unlock()
		if n >= count {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	m.Mu.Lock()
	defer m.Mu.Unlock()
	return m.PushCtxCalls
}
