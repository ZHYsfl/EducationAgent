package state

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"educationagent/internal/model"

	"github.com/openai/openai-go/v3"
)

// ToolResult stores one pending tool result waiting for the next turn.
type ToolResult struct {
	Name   string
	Result string
}

// WaitingEpisode coordinates the three lines (tts, tool call, asr) between
// an interrupt and the next LLM inference.
type WaitingEpisode struct {
	mu       sync.Mutex
	done     chan struct{}
	active   bool
	ttsDone  bool
	toolDone bool
	segments []string

	needsInterruptedPrefix   bool
	interruptedAssistantText string
}

// newWaitingEpisode creates an active waiting episode.
func newWaitingEpisode(needsPrefix bool, assistantText string, toolDone bool) *WaitingEpisode {
	return &WaitingEpisode{
		done:                     make(chan struct{}),
		active:                   true,
		toolDone:                 toolDone,
		needsInterruptedPrefix:   needsPrefix,
		interruptedAssistantText: assistantText,
	}
}

func (we *WaitingEpisode) isComplete() bool {
	return we.ttsDone && we.toolDone && len(we.segments) > 0
}

func (we *WaitingEpisode) signalIfComplete() {
	if we.isComplete() {
		select {
		case <-we.done:
		default:
			close(we.done)
		}
	}
}

// Segments returns the collected speech segments.
func (we *WaitingEpisode) Segments() []string {
	we.mu.Lock()
	defer we.mu.Unlock()
	out := make([]string, len(we.segments))
	copy(out, we.segments)
	return out
}

// NeedsInterruptedPrefix reports whether the first speech segment needs the
// </interrupted> prefix.
func (we *WaitingEpisode) NeedsInterruptedPrefix() bool {
	we.mu.Lock()
	defer we.mu.Unlock()
	return we.needsInterruptedPrefix
}

// InterruptedAssistantText returns the assistant text to sync before inference.
func (we *WaitingEpisode) InterruptedAssistantText() string {
	we.mu.Lock()
	defer we.mu.Unlock()
	return we.interruptedAssistantText
}

// AppState holds all mutable application state protected by a mutex.
type AppState struct {
	mu                    sync.RWMutex
	req                   model.Requirements
	requirementsFinalized bool
	pptToVoiceQueue       []string
	voiceToPPTQueue       []string

	voiceMu             sync.RWMutex
	voiceHistory        []openai.ChatCompletionMessageParamUnion
	conversationStarted bool

	turnMu sync.Mutex

	weMu             sync.RWMutex
	waitingEpisode   *WaitingEpisode
	pendingTTSSignal bool

	pendingMu                        sync.RWMutex
	pendingToolResults               []ToolResult
	previousTurnEnteredToolCallPhase bool

	turnStateMu                    sync.RWMutex
	currentTurnActive              bool
	currentTurnEnteredToolCallPhase bool
}

// NewAppState creates a fresh application state.
func NewAppState() *AppState {
	return &AppState{}
}

// UpdateRequirements merges the provided fields into the existing requirements.
// It returns the list of missing fields. If requirements are already finalized,
// it returns an error because the update_requirements tool has disappeared.
func (s *AppState) UpdateRequirements(req map[string]any) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.requirementsFinalized {
		return nil, errors.New("update_requirements tool has disappeared")
	}

	if v, ok := req["topic"]; ok {
		if str, ok := v.(string); ok && str != "" {
			s.req.Topic = &str
		}
	}
	if v, ok := req["style"]; ok {
		if str, ok := v.(string); ok && str != "" {
			s.req.Style = &str
		}
	}
	if v, ok := req["total_pages"]; ok {
		switch n := v.(type) {
		case int:
			s.req.TotalPages = &n
		case int64:
			i := int(n)
			s.req.TotalPages = &i
		case float64:
			i := int(n)
			s.req.TotalPages = &i
		case float32:
			i := int(n)
			s.req.TotalPages = &i
		}
	}
	if v, ok := req["audience"]; ok {
		if str, ok := v.(string); ok && str != "" {
			s.req.Audience = &str
		}
	}

	return s.req.MissingFields(), nil
}

// GetRequirements returns a snapshot of current requirements.
func (s *AppState) GetRequirements() model.Requirements {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.req
}

// RequireConfirm verifies that all requirement fields are present.
func (s *AppState) RequireConfirm() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.requirementsFinalized {
		return errors.New("require_confirm tool has disappeared")
	}
	if !s.req.IsComplete() {
		return fmt.Errorf("requirements incomplete, missing: %v", s.req.MissingFields())
	}
	return nil
}

// MarkRequirementsFinalized locks the requirements phase forever.
func (s *AppState) MarkRequirementsFinalized() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requirementsFinalized = true
}

// IsRequirementsFinalized reports whether the requirements phase is locked.
func (s *AppState) IsRequirementsFinalized() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.requirementsFinalized
}

// SendToPPTAgent enqueues a message from the voice agent to the ppt agent.
func (s *AppState) SendToPPTAgent(data string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.voiceToPPTQueue = append(s.voiceToPPTQueue, data)
}

// DrainVoiceToPPTQueue removes and returns all voice-agent-to-ppt-agent messages.
func (s *AppState) DrainVoiceToPPTQueue() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.voiceToPPTQueue))
	copy(out, s.voiceToPPTQueue)
	s.voiceToPPTQueue = s.voiceToPPTQueue[:0]
	return out
}

// VoiceToPPTQueueLen returns the number of pending voice-agent-to-ppt-agent messages.
func (s *AppState) VoiceToPPTQueueLen() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.voiceToPPTQueue)
}

// SendToVoiceAgent enqueues a message from the ppt agent to the voice agent.
func (s *AppState) SendToVoiceAgent(data string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pptToVoiceQueue = append(s.pptToVoiceQueue, data)
}

// DrainPPTToVoiceQueue removes and returns all ppt-agent-to-voice-agent messages.
func (s *AppState) DrainPPTToVoiceQueue() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.pptToVoiceQueue))
	copy(out, s.pptToVoiceQueue)
	s.pptToVoiceQueue = s.pptToVoiceQueue[:0]
	return out
}

// PPTToVoiceQueueLen returns the number of pending ppt-agent-to-voice-agent messages.
func (s *AppState) PPTToVoiceQueueLen() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.pptToVoiceQueue)
}

// ResetConversation clears all state for a fresh conversation.
func (s *AppState) ResetConversation() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.req = model.Requirements{}
	s.requirementsFinalized = false
	s.pptToVoiceQueue = s.pptToVoiceQueue[:0]
	s.voiceToPPTQueue = s.voiceToPPTQueue[:0]

	s.voiceMu.Lock()
	s.voiceHistory = s.voiceHistory[:0]
	s.conversationStarted = false
	s.voiceMu.Unlock()

	s.weMu.Lock()
	s.waitingEpisode = nil
	s.pendingTTSSignal = false
	s.weMu.Unlock()

	s.pendingMu.Lock()
	s.pendingToolResults = s.pendingToolResults[:0]
	s.previousTurnEnteredToolCallPhase = false
	s.pendingMu.Unlock()
}

// Voice history helpers.

// AppendVoiceHistory appends a message to the voice conversation history.
func (s *AppState) AppendVoiceHistory(msg openai.ChatCompletionMessageParamUnion) {
	s.voiceMu.Lock()
	defer s.voiceMu.Unlock()
	s.voiceHistory = append(s.voiceHistory, msg)
}

// GetVoiceHistory returns a snapshot of the voice conversation history.
func (s *AppState) GetVoiceHistory() []openai.ChatCompletionMessageParamUnion {
	s.voiceMu.RLock()
	defer s.voiceMu.RUnlock()
	out := make([]openai.ChatCompletionMessageParamUnion, len(s.voiceHistory))
	copy(out, s.voiceHistory)
	return out
}

// SetConversationStarted marks the conversation as started.
func (s *AppState) SetConversationStarted() {
	s.voiceMu.Lock()
	defer s.voiceMu.Unlock()
	s.conversationStarted = true
}

// IsConversationStarted reports whether the conversation has started.
func (s *AppState) IsConversationStarted() bool {
	s.voiceMu.RLock()
	defer s.voiceMu.RUnlock()
	return s.conversationStarted
}

// Turn serialization.

// LockVoiceTurn acquires the turn mutex.
func (s *AppState) LockVoiceTurn() {
	s.turnMu.Lock()
}

// UnlockVoiceTurn releases the turn mutex.
func (s *AppState) UnlockVoiceTurn() {
	s.turnMu.Unlock()
}

// Waiting episode helpers.

// CreateOrResetWaitingEpisode creates a new waiting episode, replacing any
// previous one. It returns the episode so callers can wait on it.
func (s *AppState) CreateOrResetWaitingEpisode(needsPrefix bool, assistantText string, toolDone bool) *WaitingEpisode {
	we := newWaitingEpisode(needsPrefix, assistantText, toolDone)
	s.weMu.Lock()
	s.waitingEpisode = we
	if s.pendingTTSSignal {
		we.mu.Lock()
		we.ttsDone = true
		we.mu.Unlock()
		s.pendingTTSSignal = false
	}
	s.weMu.Unlock()
	return we
}

// GetWaitingEpisode returns the current waiting episode if any.
func (s *AppState) GetWaitingEpisode() *WaitingEpisode {
	s.weMu.RLock()
	defer s.weMu.RUnlock()
	return s.waitingEpisode
}

// MarkTTSDone marks the tts line of the current waiting episode as done.
// If no episode exists, it stores a pending signal for the next episode.
func (s *AppState) MarkTTSDone() {
	s.weMu.Lock()
	defer s.weMu.Unlock()
	if s.waitingEpisode == nil {
		s.pendingTTSSignal = true
		return
	}
	s.waitingEpisode.mu.Lock()
	s.waitingEpisode.ttsDone = true
	s.waitingEpisode.signalIfComplete()
	s.waitingEpisode.mu.Unlock()
}

// AddSpeechSegment appends a speech segment to the current waiting episode.
func (s *AppState) AddSpeechSegment(text string) {
	s.weMu.RLock()
	we := s.waitingEpisode
	s.weMu.RUnlock()
	if we == nil {
		return
	}
	we.mu.Lock()
	we.segments = append(we.segments, text)
	we.signalIfComplete()
	we.mu.Unlock()
}

// MarkToolCallDone stores pending tool results and marks the tool call line
// of the current waiting episode as done.
func (s *AppState) MarkToolCallDone(results []ToolResult) {
	s.pendingMu.Lock()
	s.pendingToolResults = append(s.pendingToolResults, results...)
	s.pendingMu.Unlock()

	s.weMu.RLock()
	we := s.waitingEpisode
	s.weMu.RUnlock()
	if we == nil {
		return
	}
	we.mu.Lock()
	we.toolDone = true
	we.signalIfComplete()
	we.mu.Unlock()
}

// WaitForWaitingEpisode blocks until the current waiting episode is complete
// or the context is cancelled. It returns the completed episode and clears it
// from state.
func (s *AppState) WaitForWaitingEpisode(ctx context.Context) *WaitingEpisode {
	s.weMu.Lock()
	we := s.waitingEpisode
	s.weMu.Unlock()
	if we == nil {
		return nil
	}

	select {
	case <-we.done:
	case <-ctx.Done():
		return nil
	}

	we.mu.Lock()
	we.active = false
	we.mu.Unlock()

	s.weMu.Lock()
	s.waitingEpisode = nil
	s.weMu.Unlock()
	return we
}

// Pending tool result helpers.

// GetPendingToolResults returns and clears the pending tool results.
func (s *AppState) GetPendingToolResults() []ToolResult {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	out := make([]ToolResult, len(s.pendingToolResults))
	copy(out, s.pendingToolResults)
	s.pendingToolResults = s.pendingToolResults[:0]
	return out
}

// PeekPendingToolResults returns the pending tool results without clearing.
func (s *AppState) PeekPendingToolResults() []ToolResult {
	s.pendingMu.RLock()
	defer s.pendingMu.RUnlock()
	out := make([]ToolResult, len(s.pendingToolResults))
	copy(out, s.pendingToolResults)
	return out
}

// SetPreviousTurnEnteredToolCallPhase records whether the previous turn
// entered the tool_call phase.
func (s *AppState) SetPreviousTurnEnteredToolCallPhase(v bool) {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	s.previousTurnEnteredToolCallPhase = v
}

// IsPreviousTurnEnteredToolCallPhase reports whether the previous turn
// entered the tool_call phase.
func (s *AppState) IsPreviousTurnEnteredToolCallPhase() bool {
	s.pendingMu.RLock()
	defer s.pendingMu.RUnlock()
	return s.previousTurnEnteredToolCallPhase
}

// Current turn state helpers.

// SetCurrentTurnActive records whether a turn is currently active.
func (s *AppState) SetCurrentTurnActive(v bool) {
	s.turnStateMu.Lock()
	defer s.turnStateMu.Unlock()
	s.currentTurnActive = v
	if !v {
		s.currentTurnEnteredToolCallPhase = false
	}
}

// IsCurrentTurnActive reports whether a turn is currently active.
func (s *AppState) IsCurrentTurnActive() bool {
	s.turnStateMu.RLock()
	defer s.turnStateMu.RUnlock()
	return s.currentTurnActive
}

// SetCurrentTurnEnteredToolCallPhase records whether the current turn has
// emitted the opening of the first tool call.
func (s *AppState) SetCurrentTurnEnteredToolCallPhase(v bool) {
	s.turnStateMu.Lock()
	defer s.turnStateMu.Unlock()
	s.currentTurnEnteredToolCallPhase = v
}

// IsCurrentTurnEnteredToolCallPhase reports whether the current turn has
// emitted the opening of the first tool call.
func (s *AppState) IsCurrentTurnEnteredToolCallPhase() bool {
	s.turnStateMu.RLock()
	defer s.turnStateMu.RUnlock()
	return s.currentTurnEnteredToolCallPhase
}
