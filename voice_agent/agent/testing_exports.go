package agent

// testing_exports.go — exported shims that expose unexported methods and
// functions for black-box tests in tests/agent (package agent_test).
//
// IMPORTANT: This file is part of the production binary but the methods are
// thin pass-throughs that add no logic. Do NOT add logic here.

import (
	"context"
	"net/http"
	"time"

	"toolcalling"
	"voiceagent/internal/asr"
)

// ---------------------------------------------------------------------------
// Pipeline method shims
// ---------------------------------------------------------------------------

// StartProcessing calls the unexported startProcessing method.
func (p *Pipeline) StartProcessing(ctx context.Context, userText string) {
	p.startProcessing(ctx, userText)
}

// StartDraftThinking calls the unexported startDraftThinking method.
func (p *Pipeline) StartDraftThinking(ctx context.Context, partialText string) {
	p.startDraftThinking(ctx, partialText)
}

// PostProcessResponse calls the unexported postProcessResponse method.
func (p *Pipeline) PostProcessResponse(ctx context.Context, userText, llmResponse string) {
	p.postProcessResponse(ctx, userText, llmResponse)
}

// TryResolveConflict calls the unexported tryResolveConflict method.
func (p *Pipeline) TryResolveConflict(ctx context.Context, userText, llmResponse string) bool {
	return p.tryResolveConflict(ctx, userText, llmResponse)
}

// BuildTaskListContext calls the unexported buildTaskListContext method.
func (p *Pipeline) BuildTaskListContext() string {
	return p.buildTaskListContext()
}

// BuildPendingQuestionsContext calls the unexported buildPendingQuestionsContext method.
func (p *Pipeline) BuildPendingQuestionsContext() string {
	return p.buildPendingQuestionsContext()
}

// DrainContextQueue calls the unexported drainContextQueue method.
func (p *Pipeline) DrainContextQueue() []ContextMessage {
	return p.drainContextQueue()
}

// DrainASRResults calls the unexported drainASRResults method.
func (p *Pipeline) DrainASRResults(ctx context.Context, ch <-chan asr.ASRResult, partialTexts *[]string, finalText *string, timeout time.Duration) {
	p.drainASRResults(ctx, ch, partialTexts, finalText, timeout)
}


// AsyncExtractMemory calls the unexported asyncExtractMemory method.
func (p *Pipeline) AsyncExtractMemory(userText, assistantText string) {
	p.asyncExtractMemory(userText, assistantText)
}

// HighPriorityListener calls the unexported highPriorityListener method.
func (p *Pipeline) HighPriorityListener(ctx context.Context) {
	p.highPriorityListener(ctx)
}


// GetDraftOutput calls the unexported getDraftOutput method.
func (p *Pipeline) GetDraftOutput() string {
	return p.getDraftOutput()
}

// AppendDraftOutput calls the unexported appendDraftOutput method.
func (p *Pipeline) AppendDraftOutput(token string) {
	p.appendDraftOutput(token)
}

// ResetDraftOutput calls the unexported resetDraftOutput method.
func (p *Pipeline) ResetDraftOutput() {
	p.resetDraftOutput()
}

// ExtractContextIDFromResponse calls the unexported extractContextIDFromResponse method.
func (p *Pipeline) ExtractContextIDFromResponse(text string) string {
	return p.extractContextIDFromResponse(text)
}

// TTSWorker calls the unexported ttsWorker method.
func (p *Pipeline) TTSWorker(ctx context.Context, sentenceCh <-chan string) {
	p.ttsWorker(ctx, sentenceCh)
}

// CancelDraft calls the unexported cancelDraft method.
func (p *Pipeline) CancelDraft() {
	p.cancelDraft()
}

// AppendRawToken appends a token to rawGeneratedTokens (for testing race conditions).
func (p *Pipeline) AppendRawToken(token string) {
	p.tokensMu.Lock()
	p.rawGeneratedTokens.WriteString(token)
	p.tokensMu.Unlock()
}

// EnqueueContextMessage calls the unexported enqueueContextMessage method.
func (p *Pipeline) EnqueueContextMessage(ctx context.Context, msg ContextMessage) {
	p.enqueueContextMessage(ctx, msg)
}

// ---------------------------------------------------------------------------
// Session method shims
// ---------------------------------------------------------------------------

// CancelCurrentPipeline calls the unexported cancelCurrentPipeline method.
func (s *Session) CancelCurrentPipeline() {
	s.cancelCurrentPipeline()
}

// NewPipelineContext calls the unexported newPipelineContext method.
func (s *Session) NewPipelineContext() context.Context {
	return s.newPipelineContext()
}

// HandleTextMessage calls the unexported handleTextMessage method.
func (s *Session) HandleTextMessage(msg WSMessage) {
	s.handleTextMessage(msg)
}

// HandleTextInput calls the unexported handleTextInput method.
func (s *Session) HandleTextInput(msg WSMessage) {
	s.handleTextInput(msg)
}

// HandlePageNavigate calls the unexported handlePageNavigate method.
func (s *Session) HandlePageNavigate(msg WSMessage) {
	s.handlePageNavigate(msg)
}

// OnVADEnd calls the unexported onVADEnd method.
func (s *Session) OnVADEnd() {
	s.onVADEnd()
}

// OnVADStart calls the unexported onVADStart method.
func (s *Session) OnVADStart() {
	s.onVADStart()
}

// PublishVADEvent calls the unexported publishVADEvent method.
func (s *Session) PublishVADEvent() {
	s.publishVADEvent()
}

// HandleAudioData calls the unexported handleAudioData method.
func (s *Session) HandleAudioData(data []byte) {
	s.handleAudioData(data)
}

// ---------------------------------------------------------------------------
// Session reqMu accessors
// ---------------------------------------------------------------------------

// LockReqMu acquires the reqMu write lock.
func (s *Session) LockReqMu() { s.reqMu.Lock() }

// UnlockReqMu releases the reqMu write lock.
func (s *Session) UnlockReqMu() { s.reqMu.Unlock() }

// RLockReqMu acquires the reqMu read lock.
func (s *Session) RLockReqMu() { s.reqMu.RLock() }

// RUnlockReqMu releases the reqMu read lock.
func (s *Session) RUnlockReqMu() { s.reqMu.RUnlock() }

// ---------------------------------------------------------------------------
// Session activeTaskMu accessors
// ---------------------------------------------------------------------------

// LockActiveTaskMu acquires the activeTaskMu write lock.
func (s *Session) LockActiveTaskMu() { s.activeTaskMu.Lock() }

// UnlockActiveTaskMu releases the activeTaskMu write lock.
func (s *Session) UnlockActiveTaskMu() { s.activeTaskMu.Unlock() }

// RLockActiveTaskMu acquires the activeTaskMu read lock.
func (s *Session) RLockActiveTaskMu() { s.activeTaskMu.RLock() }

// RUnlockActiveTaskMu releases the activeTaskMu read lock.
func (s *Session) RUnlockActiveTaskMu() { s.activeTaskMu.RUnlock() }

// ReadLoop calls the unexported readLoop method.
func (s *Session) ReadLoop() {
	s.readLoop()
}

// WriteLoop calls the unexported writeLoop method.
func (s *Session) WriteLoop() {
	s.writeLoop()
}

// ---------------------------------------------------------------------------
// Package-level function shims
// ---------------------------------------------------------------------------

// IsInterrupt calls the unexported isInterrupt function (for pipeline_async_test).
func IsInterrupt(ctx context.Context, agent *toolcalling.Agent, text string) bool {
	return isInterrupt(ctx, agent, text)
}

// IsSentenceEnd calls the unexported isSentenceEnd function.
func IsSentenceEnd(s string) bool {
	return isSentenceEnd(s)
}

// Truncate calls the unexported truncate function.
func Truncate(s string, maxLen int) string {
	return truncate(s, maxLen)
}

// FormatProfileSummary calls the unexported formatProfileSummary function.
func FormatProfileSummary(profile *UserProfile) string {
	return formatProfileSummary(profile)
}



// DecodeAPIData calls the unexported decodeAPIData function.
func DecodeAPIData(raw []byte, out any) error {
	return decodeAPIData(raw, out)
}

// ---------------------------------------------------------------------------
// HTTP helper shims
// ---------------------------------------------------------------------------

// WriteSuccess calls the unexported writeSuccess function.
func WriteSuccess(w http.ResponseWriter, httpStatus int, data any) {
	writeSuccess(w, httpStatus, data)
}

// WriteError calls the unexported writeError function.
func WriteError(w http.ResponseWriter, httpStatus, code int, message string) {
	writeError(w, httpStatus, code, message)
}
