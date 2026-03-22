package main

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// compile-time check: mockServices must satisfy ExternalServices
var _ ExternalServices = (*mockServices)(nil)

// ---------------------------------------------------------------------------
// mockServices implements ExternalServices.
// Each method delegates to an injectable func field; if nil, returns zero/nil.
// ---------------------------------------------------------------------------

type mockServices struct {
	QueryKBFn          func(ctx context.Context, req KBQueryRequest) (KBQueryResponse, error)
	RecallMemoryFn     func(ctx context.Context, req MemoryRecallRequest) (MemoryRecallResponse, error)
	GetUserProfileFn   func(ctx context.Context, userID string) (UserProfile, error)
	SearchWebFn        func(ctx context.Context, req SearchRequest) (SearchResponse, error)
	InitPPTFn          func(ctx context.Context, req PPTInitRequest) (PPTInitResponse, error)
	SendFeedbackFn     func(ctx context.Context, req PPTFeedbackRequest) error
	GetCanvasStatusFn  func(ctx context.Context, taskID string) (CanvasStatusResponse, error)
	UploadFileFn       func(r *http.Request) (json.RawMessage, error)
	IngestFromSearchFn func(ctx context.Context, req IngestFromSearchRequest) error
	ExtractMemoryFn    func(ctx context.Context, req MemoryExtractRequest) (MemoryExtractResponse, error)
	SaveWorkingMemFn   func(ctx context.Context, req WorkingMemorySaveRequest) error
	GetWorkingMemFn    func(ctx context.Context, sessionID string) (*WorkingMemory, error)
	NotifyVADEventFn   func(ctx context.Context, event VADEvent) error

	mu              sync.Mutex
	feedbackCalls   []PPTFeedbackRequest
	vadEventCalls   []VADEvent
	extractMemCalls []MemoryExtractRequest
	initPPTCalls    []PPTInitRequest
}

func (m *mockServices) QueryKB(ctx context.Context, req KBQueryRequest) (KBQueryResponse, error) {
	if m.QueryKBFn != nil {
		return m.QueryKBFn(ctx, req)
	}
	return KBQueryResponse{}, nil
}
func (m *mockServices) RecallMemory(ctx context.Context, req MemoryRecallRequest) (MemoryRecallResponse, error) {
	if m.RecallMemoryFn != nil {
		return m.RecallMemoryFn(ctx, req)
	}
	return MemoryRecallResponse{}, nil
}
func (m *mockServices) GetUserProfile(ctx context.Context, userID string) (UserProfile, error) {
	if m.GetUserProfileFn != nil {
		return m.GetUserProfileFn(ctx, userID)
	}
	return UserProfile{}, nil
}
func (m *mockServices) SearchWeb(ctx context.Context, req SearchRequest) (SearchResponse, error) {
	if m.SearchWebFn != nil {
		return m.SearchWebFn(ctx, req)
	}
	return SearchResponse{}, nil
}
func (m *mockServices) InitPPT(ctx context.Context, req PPTInitRequest) (PPTInitResponse, error) {
	m.mu.Lock()
	m.initPPTCalls = append(m.initPPTCalls, req)
	m.mu.Unlock()
	if m.InitPPTFn != nil {
		return m.InitPPTFn(ctx, req)
	}
	return PPTInitResponse{TaskID: "task_mock_001"}, nil
}
func (m *mockServices) SendFeedback(ctx context.Context, req PPTFeedbackRequest) error {
	m.mu.Lock()
	m.feedbackCalls = append(m.feedbackCalls, req)
	m.mu.Unlock()
	if m.SendFeedbackFn != nil {
		return m.SendFeedbackFn(ctx, req)
	}
	return nil
}
func (m *mockServices) GetCanvasStatus(ctx context.Context, taskID string) (CanvasStatusResponse, error) {
	if m.GetCanvasStatusFn != nil {
		return m.GetCanvasStatusFn(ctx, taskID)
	}
	return CanvasStatusResponse{TaskID: taskID}, nil
}
func (m *mockServices) UploadFile(r *http.Request) (json.RawMessage, error) {
	if m.UploadFileFn != nil {
		return m.UploadFileFn(r)
	}
	return json.RawMessage(`{"file_id":"f_mock"}`), nil
}
func (m *mockServices) IngestFromSearch(ctx context.Context, req IngestFromSearchRequest) error {
	if m.IngestFromSearchFn != nil {
		return m.IngestFromSearchFn(ctx, req)
	}
	return nil
}
func (m *mockServices) ExtractMemory(ctx context.Context, req MemoryExtractRequest) (MemoryExtractResponse, error) {
	m.mu.Lock()
	m.extractMemCalls = append(m.extractMemCalls, req)
	m.mu.Unlock()
	if m.ExtractMemoryFn != nil {
		return m.ExtractMemoryFn(ctx, req)
	}
	return MemoryExtractResponse{}, nil
}
func (m *mockServices) SaveWorkingMemory(ctx context.Context, req WorkingMemorySaveRequest) error {
	if m.SaveWorkingMemFn != nil {
		return m.SaveWorkingMemFn(ctx, req)
	}
	return nil
}
func (m *mockServices) GetWorkingMemory(ctx context.Context, sessionID string) (*WorkingMemory, error) {
	if m.GetWorkingMemFn != nil {
		return m.GetWorkingMemFn(ctx, sessionID)
	}
	return nil, nil
}
func (m *mockServices) NotifyVADEvent(ctx context.Context, event VADEvent) error {
	m.mu.Lock()
	m.vadEventCalls = append(m.vadEventCalls, event)
	m.mu.Unlock()
	if m.NotifyVADEventFn != nil {
		return m.NotifyVADEventFn(ctx, event)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func newTestConfig() *Config {
	return &Config{
		ServerPort:        9000,
		SystemPrompt:      "你是测试助手。",
		AdaptiveSizesFile: "",
		TokenBudget:       50,
		FillerInterval:    100,
		FillerPhrases:     []string{"稍等"},
		MaxFillers:        3,
		DefaultUserID:     "user_test",
	}
}

func newTestSession(clients ExternalServices) *Session {
	cfg := newTestConfig()
	s := &Session{
		config:           cfg,
		SessionID:        "sess_test_001",
		UserID:           "user_test",
		state:            StateIdle,
		writeCh:          make(chan writeItem, 256),
		done:             make(chan struct{}),
		clients:          clients,
		OwnedTasks:       make(map[string]string),
		PendingQuestions: make(map[string]string),
	}
	return s
}

func newTestPipeline(session *Session, clients ExternalServices) *Pipeline {
	p := &Pipeline{
		session:           session,
		config:            session.config,
		clients:           clients,
		adaptive:          NewAdaptiveController(DefaultChannelSizes()),
		contextQueue:      make(chan ContextMessage, 64),
		highPriorityQueue: make(chan ContextMessage, 16),
		history:           NewConversationHistory("test prompt"),
	}
	session.pipeline = p
	return p
}

// mockTTS is a no-op TTS provider for tests.
type mockTTS struct{}

func (m *mockTTS) Synthesize(ctx context.Context, text string, bufSize int) (<-chan []byte, error) {
	ch := make(chan []byte)
	close(ch)
	return ch, nil
}

// newTestPipelineWithTTS creates a pipeline with a mock TTS so speakText won't panic.
func newTestPipelineWithTTS(session *Session, clients ExternalServices) *Pipeline {
	p := newTestPipeline(session, clients)
	p.ttsClient = &mockTTS{}
	return p
}

// drainWriteCh reads all pending WSMessage from the session's writeCh.
func drainWriteCh(s *Session) []WSMessage {
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

// findWSMessage finds the first WSMessage with the given type from the list.
func findWSMessage(msgs []WSMessage, msgType string) (WSMessage, bool) {
	for _, m := range msgs {
		if m.Type == msgType {
			return m, true
		}
	}
	return WSMessage{}, false
}

// waitForFeedback waits for a SendFeedback call with a short timeout.
func waitForFeedback(m *mockServices, count int) []PPTFeedbackRequest {
	for i := 0; i < 200; i++ {
		m.mu.Lock()
		n := len(m.feedbackCalls)
		m.mu.Unlock()
		if n >= count {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.feedbackCalls
}

// waitForExtractMem waits for ExtractMemory calls.
func waitForExtractMem(m *mockServices, count int) []MemoryExtractRequest {
	for i := 0; i < 200; i++ {
		m.mu.Lock()
		n := len(m.extractMemCalls)
		m.mu.Unlock()
		if n >= count {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.extractMemCalls
}
