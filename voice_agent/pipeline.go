package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
	"toolcalling"
	"unicode"

	"github.com/openai/openai-go/v3"
)

type Pipeline struct {
	session *Session
	config  *Config
	clients ExternalServices

	asrClient ASRProvider
	smallLLM  *toolcalling.Agent
	largeLLM  *toolcalling.Agent
	ttsClient TTSProvider

	history  *ConversationHistory
	adaptive *AdaptiveController

	audioBuf *AudioBuffer
	audioCh  chan []byte   // audio data from session → pipeline
	vadEndCh chan struct{} // signal: user stopped speaking
	ioMu     sync.RWMutex  // protects audioCh/vadEndCh pointer swaps
	runMu    sync.Mutex    // ensures only one StartListening runs at a time

	// For interrupt preservation: tracks ALL tokens including <think> content
	rawGeneratedTokens strings.Builder
	tokensMu           sync.Mutex

	// Draft thinking
	draftCancel   context.CancelFunc
	draftMu       sync.Mutex
	draftOutput   strings.Builder // accumulated thinker output across rounds
	draftOutputMu sync.Mutex

	contextQueue      chan ContextMessage
	pendingContexts   []ContextMessage
	pendingMu         sync.Mutex
	highPriorityQueue chan ContextMessage
}

func NewPipeline(session *Session, config *Config, clients ExternalServices) *Pipeline {
	sizes := LoadChannelSizes(config.AdaptiveSizesFile, DefaultChannelSizes())

	var asr ASRProvider
	if config.ASRMode == "remote" {
		asr = NewDouBaoASRClient(DouBaoASRConfig{
			AppKey:     config.DouBaoASRAppKey,
			AccessKey:  config.DouBaoASRAccessKey,
			ResourceId: config.DouBaoASRResourceId,
		})
		log.Printf("ASR mode: remote (Doubao)")
	} else {
		asr = NewASRClient(config.ASRWSURL)
		log.Printf("ASR mode: local (%s)", config.ASRWSURL)
	}

	var tts TTSProvider
	if config.TTSMode == "remote" {
		tts = NewDouBaoTTSClient(DouBaoTTSConfig{
			AppId:     config.DouBaoTTSAppId,
			Token:     config.DouBaoTTSToken,
			Cluster:   config.DouBaoTTSCluster,
			VoiceType: config.DouBaoTTSVoiceType,
		})
		log.Printf("TTS mode: remote (Doubao %s)", config.DouBaoTTSVoiceType)
	} else {
		tts = NewTTSClient(config.TTSURL)
		log.Printf("TTS mode: local (%s)", config.TTSURL)
	}

	return &Pipeline{
		session:   session,
		config:    config,
		clients:   clients,
		asrClient: asr,
		smallLLM: toolcalling.NewAgent(toolcalling.LLMConfig{
			APIKey:  config.SmallLLMAPIKey,
			Model:   config.SmallLLMModel,
			BaseURL: config.SmallLLMBaseURL,
		}),
		largeLLM: toolcalling.NewAgent(toolcalling.LLMConfig{
			APIKey:  config.LargeLLMAPIKey,
			Model:   config.LargeLLMModel,
			BaseURL: config.LargeLLMBaseURL,
		}),
		ttsClient:         tts,
		history:           NewConversationHistory(config.SystemPrompt),
		audioBuf:          NewAudioBuffer(),
		adaptive:          NewAdaptiveController(sizes),
		contextQueue:      make(chan ContextMessage, 64),
		highPriorityQueue: make(chan ContextMessage, 16),
	}
}

// OnAudioData is called by the session when audio arrives during LISTENING.
func (p *Pipeline) OnAudioData(data []byte) {
	p.ioMu.RLock()
	audioCh := p.audioCh
	p.ioMu.RUnlock()
	if audioCh != nil {
		p.adaptive.RecordLen("audio_ch", len(audioCh))
		select {
		case audioCh <- data:
		default:
			p.adaptive.RecordBlock("audio_ch")
		}
	}
}

// OnVADEnd is called when the browser signals end of speech.
func (p *Pipeline) OnVADEnd() {
	p.ioMu.RLock()
	vadEndCh := p.vadEndCh
	p.ioMu.RUnlock()
	if vadEndCh != nil {
		select {
		case vadEndCh <- struct{}{}:
		default:
		}
	}
}

// OnInterrupt is called when user starts speaking during PROCESSING/SPEAKING.
// Saves partial LLM output (including <think> reasoning) to history before
// the pipeline context is cancelled, so the model can resume from where it
// left off in the next turn.
func (p *Pipeline) OnInterrupt() {
	p.tokensMu.Lock()
	raw := p.rawGeneratedTokens.String()
	p.rawGeneratedTokens.Reset()
	p.tokensMu.Unlock()

	if raw != "" {
		// Close unclosed <think> tag so the model sees well-formed history.
		// Strip any partial </think> suffix (e.g. "</thi") before appending.
		if strings.Contains(raw, "<think>") && !strings.Contains(raw, "</think>") {
			if partial := longestSuffixPrefix(raw, "</think>"); partial != "" {
				raw = raw[:len(raw)-len(partial)]
			}
			raw += "</think>"
		}
		p.history.AddInterruptedAssistant(raw)
		log.Printf("Interrupt: preserved %d chars (including thinking)", len(raw))
	}

	p.cancelDraft()
}

// ---------------------------------------------------------------------------
// LISTENING phase
// ---------------------------------------------------------------------------

// StartListening runs the listening pipeline: accumulate audio, run ASR,
// check for interrupt intent, and optionally do draft thinking.
func (p *Pipeline) StartListening(ctx context.Context) {
	p.runMu.Lock()
	defer p.runMu.Unlock()

	audioCh := make(chan []byte, p.adaptive.Get("audio_ch"))
	vadEndCh := make(chan struct{}, 1)
	p.ioMu.Lock()
	p.audioBuf.Reset()
	p.audioCh = audioCh
	p.vadEndCh = vadEndCh
	p.ioMu.Unlock()
	defer func() {
		p.ioMu.Lock()
		if p.audioCh == audioCh {
			p.audioCh = nil
		}
		if p.vadEndCh == vadEndCh {
			p.vadEndCh = nil
		}
		p.ioMu.Unlock()
	}()

	go p.highPriorityListener(ctx)

	p.tokensMu.Lock()
	p.rawGeneratedTokens.Reset()
	p.tokensMu.Unlock()

	p.resetDraftOutput()

	asrAudioCh := make(chan []byte, p.adaptive.Get("asr_audio_ch"))

	asrResultCh, err := p.asrClient.RecognizeStream(ctx, asrAudioCh, p.adaptive.Get("asr_result_ch"))
	if err != nil {
		log.Printf("ASR start failed: %v", err)
		p.session.SetState(StateIdle)
		return
	}

	var partialTexts []string
	var finalText string
	vadEnded := false
	draftStarted := false
	asrSinceDraft := 0
	draftInterval := 1
	contextQueriesStarted := false

	for {
		select {
		case audio := <-audioCh:
			p.audioBuf.Write(audio)

			// Extract complete 1s blocks and send to ASR
			for {
				block, ok := p.audioBuf.GetBlock()
				if !ok {
					break
				}
				p.adaptive.RecordLen("asr_audio_ch", len(asrAudioCh))
				select {
				case asrAudioCh <- block:
				case <-ctx.Done():
					close(asrAudioCh)
					return
				}
			}

		case result, ok := <-asrResultCh:
			if !ok {
				fullText := finalText
				if fullText == "" {
					fullText = strings.Join(partialTexts, "")
				}
				if fullText != "" {
					p.startProcessing(ctx, fullText)
				} else {
					p.session.SetState(StateIdle)
				}
				return
			}

			if result.Mode == "2pass-offline" {
				finalText = result.Text
				p.session.SendJSON(WSMessage{Type: "transcript_final", Text: finalText})
			} else {
				partialTexts = append(partialTexts, result.Text)
				partialText := strings.Join(partialTexts, "")
				p.session.SendJSON(WSMessage{Type: "transcript", Text: partialText})
			}

			// Draft thinking uses partial text (low latency)
			currentText := strings.Join(partialTexts, "")
			if currentText == "" && finalText != "" {
				currentText = finalText
			}
			if !contextQueriesStarted && currentText != "" {
				contextQueriesStarted = true
				p.launchAsyncContextQueries(ctx, currentText)
			}
			if currentText == "" {
				break
			}

			if !draftStarted {
				if isInterrupt(ctx, p.smallLLM, currentText) {
					draftStarted = true
					asrSinceDraft = 0
					draftInterval = 2
					p.startDraftThinking(ctx, currentText)
				}
			} else {
				asrSinceDraft++
				if asrSinceDraft >= draftInterval {
					asrSinceDraft = 0
					draftInterval++
					p.startDraftThinking(ctx, currentText)
				}
			}

		case <-vadEndCh:
			vadEnded = true
			if remaining := p.audioBuf.Flush(); len(remaining) > 0 {
				select {
				case asrAudioCh <- remaining:
				default:
				}
			}
			close(asrAudioCh)

			// Wait for final ASR results (including 2pass-offline)
			p.drainASRResults(ctx, asrResultCh, &partialTexts, &finalText, 2*time.Second)

			p.cancelDraft()

			fullText := finalText
			if fullText == "" {
				fullText = strings.Join(partialTexts, "")
			}
			if fullText == "" {
				p.session.SetState(StateIdle)
				return
			}
			if finalText != "" {
				p.session.SendJSON(WSMessage{Type: "transcript_final", Text: finalText})
			}
			p.startProcessing(ctx, fullText)
			return

		case <-ctx.Done():
			if !vadEnded {
				close(asrAudioCh)
			}
			return
		}
	}
}

// drainASRResults reads remaining ASR results until timeout or channel close.
// 2pass-offline results update finalText; others append to partialTexts.
func (p *Pipeline) drainASRResults(ctx context.Context, ch <-chan ASRResult, partialTexts *[]string, finalText *string, timeout time.Duration) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case result, ok := <-ch:
			if !ok {
				return
			}
			if result.Text == "" {
				continue
			}
			if result.Mode == "2pass-offline" {
				*finalText = result.Text
			} else {
				*partialTexts = append(*partialTexts, result.Text)
			}
			if result.IsFinal {
				return
			}
		case <-timer.C:
			return
		case <-ctx.Done():
			return
		}
	}
}

// ---------------------------------------------------------------------------
// 边听边想: Draft thinking
// ---------------------------------------------------------------------------

func (p *Pipeline) startDraftThinking(ctx context.Context, partialText string) {
	previousThought := p.getDraftOutput()
	p.cancelDraft()
	p.resetDraftOutput()

	p.draftMu.Lock()
	draftCtx, cancel := context.WithCancel(ctx)
	p.draftCancel = cancel
	p.draftMu.Unlock()

	go func() {
		messages := p.history.ToOpenAIWithDraftAndThought(partialText, previousThought)
		tokenCh := p.largeLLM.StreamChat(draftCtx, messages)
		for token := range tokenCh {
			p.appendDraftOutput(token)
		}
	}()
}

func (p *Pipeline) cancelDraft() {
	p.draftMu.Lock()
	if p.draftCancel != nil {
		p.draftCancel()
		p.draftCancel = nil
	}
	p.draftMu.Unlock()
}

func (p *Pipeline) getDraftOutput() string {
	p.draftOutputMu.Lock()
	defer p.draftOutputMu.Unlock()
	return p.draftOutput.String()
}

func (p *Pipeline) appendDraftOutput(token string) {
	p.draftOutputMu.Lock()
	p.draftOutput.WriteString(token)
	p.draftOutputMu.Unlock()
}

func (p *Pipeline) resetDraftOutput() {
	p.draftOutputMu.Lock()
	p.draftOutput.Reset()
	p.draftOutputMu.Unlock()
}

// ---------------------------------------------------------------------------
// PROCESSING → SPEAKING phase
// ---------------------------------------------------------------------------

func (p *Pipeline) startProcessing(ctx context.Context, userText string) {
	p.session.SetState(StateProcessing)

	// Grab accumulated thinker output before clearing
	previousThought := p.getDraftOutput()
	p.resetDraftOutput()

	p.history.AddUser(userText)

	contextMsgs := p.drainContextQueue()
	contextPrompt := FormatContextForLLM(contextMsgs)

	inRequirementsMode := false
	systemPrompt := p.config.SystemPrompt
	p.session.reqMu.RLock()
	reqSnapshot := CloneTaskRequirements(p.session.Requirements)
	p.session.reqMu.RUnlock()
	if reqSnapshot != nil && (reqSnapshot.Status == "collecting" || reqSnapshot.Status == "confirming") {
		inRequirementsMode = true
		var profile *UserProfile
		if p.clients != nil {
			if pInfo, err := p.clients.GetUserProfile(ctx, reqSnapshot.UserID); err == nil {
				profile = &pInfo
			}
		}
		systemPrompt = reqSnapshot.BuildRequirementsSystemPrompt(profile)
	}

	if !inRequirementsMode {
		systemPrompt += pptIntentDetectionPrompt
	}

	taskListContext := p.buildTaskListContext()
	if taskListContext != "" {
		systemPrompt += taskListContext
	}
	pendingQContext := p.buildPendingQuestionsContext()
	if pendingQContext != "" {
		systemPrompt += pendingQContext
	}
	if contextPrompt != "" {
		systemPrompt += contextPrompt
	}

	log.Printf("Processing user input: %s", truncate(userText, 100))
	if previousThought != "" {
		log.Printf("With %d chars of pre-thinking", len(previousThought))
	}

	// Send user text to browser for display
	p.session.SendJSON(WSMessage{Type: "transcript", Text: userText})

	// TTS sentence queue — decouples LLM generation from TTS synthesis
	sentenceCh := make(chan string, p.adaptive.Get("sentence_ch"))

	var ttsWg sync.WaitGroup
	ttsWg.Add(1)
	go func() {
		defer ttsWg.Done()
		p.ttsWorker(ctx, sentenceCh)
	}()

	// Stream tokens from Large LLM (with accumulated thought if available).
	messages := p.history.ToOpenAIWithThoughtAndPrompt(previousThought, systemPrompt)
	tokenCh := p.largeLLM.StreamChat(ctx, messages)

	totalTokens := 0 // ALL tokens (including <think>) for budget accounting
	var sentenceBuf strings.Builder
	var allTokens strings.Builder
	firstSentenceSent := false
	nextFillerAt := p.config.TokenBudget // first filler fires at TokenBudget
	fillerCount := 0

	var tf thinkFilter

	// Interrupt-safe send: never blocks if ttsWorker already exited.
	sendSentence := func(s string) bool {
		p.adaptive.RecordLen("sentence_ch", len(sentenceCh))
		select {
		case sentenceCh <- s:
			return true
		case <-ctx.Done():
			return false
		}
	}

	for token := range tokenCh {
		if ctx.Err() != nil {
			break
		}

		totalTokens++

		// Track ALL raw tokens (including <think>) for interrupt preservation
		p.tokensMu.Lock()
		p.rawGeneratedTokens.WriteString(token)
		p.tokensMu.Unlock()

		// Strip <think>...</think> blocks — only pass visible content through
		visible := tf.Feed(token)

		if visible != "" {
			allTokens.WriteString(visible)
			p.session.SendJSON(WSMessage{Type: "response", Text: visible})
		}

		// Budget window counts ALL tokens: gives the model time to think
		// internally, but if the budget runs out with nothing spoken, inject filler.
		if totalTokens <= p.config.TokenBudget {
			if visible != "" {
				sentenceBuf.WriteString(visible)
				if isSentenceEnd(visible) && sentenceBuf.Len() > 0 {
					sentence := sentenceBuf.String()
					sentenceBuf.Reset()
					if !sendSentence(sentence) {
						break
					}
					firstSentenceSent = true
					if p.session.GetState() == StateProcessing {
						p.session.SetState(StateSpeaking)
					}
				}
			}
			continue
		}

		// Periodic filler while model is still thinking and no visible sentence produced.
		// Each filler is a different phrase; stop after MaxFillers to avoid sounding robotic.
		if !firstSentenceSent && fillerCount < p.config.MaxFillers && totalTokens >= nextFillerAt {
			idx := fillerCount
			if idx >= len(p.config.FillerPhrases) {
				idx = len(p.config.FillerPhrases) - 1
			}
			if !sendSentence(p.config.FillerPhrases[idx]) {
				break
			}
			fillerCount++
			nextFillerAt = totalTokens + p.config.FillerInterval
			if p.session.GetState() == StateProcessing {
				p.session.SetState(StateSpeaking)
			}
		}

		if visible != "" {
			sentenceBuf.WriteString(visible)
			if isSentenceEnd(visible) {
				sentence := sentenceBuf.String()
				sentenceBuf.Reset()
				if !sendSentence(sentence) {
					break
				}
				firstSentenceSent = true
			}
		}
	}

	if flushed := tf.Flush(); flushed != "" {
		allTokens.WriteString(flushed)
		sentenceBuf.WriteString(flushed)
	}
	if sentenceBuf.Len() > 0 {
		sendSentence(sentenceBuf.String())
	}
	close(sentenceCh)

	// Wait for all TTS audio to be sent
	ttsWg.Wait()

	// Save to history (if not interrupted)
	if ctx.Err() == nil {
		finalText := allTokens.String()
		if finalText != "" {
			p.history.AddAssistant(finalText)
		}

		p.postProcessResponse(ctx, userText, finalText, inRequirementsMode)
		p.asyncExtractMemory(userText, finalText)

		p.session.SetState(StateIdle)
	}

	p.adaptive.Adjust()
	p.adaptive.Save(p.config.AdaptiveSizesFile)
}

// ---------------------------------------------------------------------------
// Post-processing: conflict resolution, requirements state transitions
// ---------------------------------------------------------------------------

func (p *Pipeline) postProcessResponse(ctx context.Context, userText, llmResponse string, inRequirementsMode bool) {
	if p.tryResolveConflict(ctx, userText, llmResponse) {
		return
	}

	if inRequirementsMode {
		p.handleRequirementsTransition(llmResponse)
		return
	}

	if p.tryDetectTaskInit(llmResponse) {
		return
	}

	p.trySendPPTFeedback(userText, llmResponse)
}

func (p *Pipeline) tryResolveConflict(_ context.Context, userText, llmResponse string) bool {
	if p.clients == nil {
		return false
	}
	p.session.pendingQMu.RLock()
	pendingCount := len(p.session.PendingQuestions)
	if pendingCount == 0 {
		p.session.pendingQMu.RUnlock()
		return false
	}

	var contextID, taskID string
	if pendingCount == 1 {
		for cid, tid := range p.session.PendingQuestions {
			contextID = cid
			taskID = tid
		}
	} else {
		contextID = p.extractContextIDFromResponse(llmResponse)
		if contextID != "" {
			taskID = p.session.PendingQuestions[contextID]
		}
		if contextID == "" || taskID == "" {
			for cid, tid := range p.session.PendingQuestions {
				contextID = cid
				taskID = tid
				break
			}
		}
	}
	p.session.pendingQMu.RUnlock()

	if _, ok := p.session.ResolvePendingQuestion(contextID); !ok {
		return false
	}
	log.Printf("[pipeline] resolving conflict context_id=%s task_id=%s", contextID, taskID)

	viewingPageID := p.session.GetViewingPageID()
	baseTS := p.session.GetLastVADTimestamp()
	go func() {
		if err := p.clients.SendFeedback(context.Background(), PPTFeedbackRequest{
			TaskID:           taskID,
			BaseTimestamp:    baseTS,
			ViewingPageID:    viewingPageID,
			ReplyToContextID: contextID,
			RawText:          userText,
			Intents: []Intent{{
				ActionType: "resolve_conflict",
				ContextID:  contextID,
			}},
		}); err != nil {
			log.Printf("[pipeline] SendFeedback resolve_conflict failed: %v", err)
		}
	}()
	return true
}

func (p *Pipeline) extractContextIDFromResponse(text string) string {
	marker := "[RESOLVE_CONFLICT:"
	idx := strings.Index(text, marker)
	if idx < 0 {
		return ""
	}
	rest := text[idx+len(marker):]
	end := strings.Index(rest, "]")
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(rest[:end])
}

func (p *Pipeline) trySendPPTFeedback(userText, llmResponse string) {
	if p.clients == nil {
		return
	}
	taskID, ok := p.session.ResolveTaskID()
	if !ok || taskID == "" {
		return
	}

	marker := "[PPT_FEEDBACK]"
	idx := strings.Index(llmResponse, marker)
	if idx < 0 {
		return
	}
	jsonStr := llmResponse[idx+len(marker):]
	jsonStr = strings.TrimSpace(jsonStr)

	endIdx := strings.Index(jsonStr, "[/PPT_FEEDBACK]")
	if endIdx > 0 {
		jsonStr = jsonStr[:endIdx]
	}

	var parsed struct {
		ActionType   string   `json:"action_type"`
		PageID       string   `json:"page_id"`
		TargetPageID string   `json:"target_page_id"`
		Instruction  string   `json:"instruction"`
		Scope        string   `json:"scope"`
		Keywords     []string `json:"keywords"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		log.Printf("[pipeline] failed to parse PPT_FEEDBACK JSON: %v (raw: %s)", err, truncate(jsonStr, 200))
		return
	}

	viewingPageID := p.session.GetViewingPageID()
	pageID := parsed.PageID
	if pageID == "" {
		pageID = viewingPageID
	}

	baseTS := p.session.GetLastVADTimestamp()
	go func() {
		if err := p.clients.SendFeedback(context.Background(), PPTFeedbackRequest{
			TaskID:        taskID,
			BaseTimestamp: baseTS,
			ViewingPageID: viewingPageID,
			RawText:       userText,
			Intents: []Intent{{
				ActionType:   parsed.ActionType,
				PageID:       pageID,
				TargetPageID: parsed.TargetPageID,
				Instruction:  parsed.Instruction,
				Scope:        parsed.Scope,
				Keywords:     parsed.Keywords,
			}},
		}); err != nil {
			log.Printf("[pipeline] SendFeedback failed: %v", err)
		}
	}()
}

func (p *Pipeline) tryDetectTaskInit(llmResponse string) bool {
	marker := "[TASK_INIT]"
	idx := strings.Index(llmResponse, marker)
	if idx < 0 {
		return false
	}

	jsonStr := strings.TrimSpace(llmResponse[idx+len(marker):])
	endIdx := strings.Index(jsonStr, "[/TASK_INIT]")
	if endIdx > 0 {
		jsonStr = jsonStr[:endIdx]
	}

	var initData struct {
		Topic string `json:"topic"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &initData); err != nil {
		log.Printf("[pipeline] failed to parse TASK_INIT JSON: %v (raw: %s)", err, truncate(jsonStr, 200))
		return false
	}

	req := NewTaskRequirements(p.session.SessionID, p.session.UserID)
	if initData.Topic != "" {
		req.Topic = initData.Topic
	}
	p.session.prefillFromMemory(req)
	req.RefreshCollectedFields()
	reqSnapshot := CloneTaskRequirements(req)

	p.session.reqMu.Lock()
	p.session.Requirements = req
	p.session.reqMu.Unlock()

	p.session.SendJSON(WSMessage{
		Type:            "requirements_progress",
		Status:          req.Status,
		CollectedFields: req.CollectedFields,
		MissingFields:   req.GetMissingFields(),
		Requirements:    reqSnapshot,
	})

	log.Printf("[pipeline] voice-initiated task_init, topic=%q", initData.Topic)
	return true
}

func (p *Pipeline) asyncExtractMemory(userText, assistantText string) {
	if p.clients == nil || (userText == "" && assistantText == "") {
		return
	}
	sessionID := p.session.SessionID
	userID := p.session.UserID
	turns := make([]ConversationTurn, 0, 2)
	if userText != "" {
		turns = append(turns, ConversationTurn{Role: "user", Content: userText})
	}
	if assistantText != "" {
		turns = append(turns, ConversationTurn{Role: "assistant", Content: assistantText})
	}
	go func() {
		if _, err := p.clients.ExtractMemory(context.Background(), MemoryExtractRequest{
			UserID:    userID,
			SessionID: sessionID,
			Messages:  turns,
		}); err != nil {
			log.Printf("[pipeline] ExtractMemory failed: %v", err)
		}
	}()
}

func (p *Pipeline) handleRequirementsTransition(llmResponse string) {
	p.session.reqMu.Lock()
	req := p.session.Requirements
	if req == nil {
		p.session.reqMu.Unlock()
		return
	}

	switch req.Status {
	case "confirming":
		if strings.Contains(llmResponse, "[REQUIREMENTS_CONFIRMED]") {
			reqRef := req
			req.Status = "confirmed"
			req.UpdatedAt = time.Now().UnixMilli()
			reqSnapshot := CloneTaskRequirements(req)
			p.session.reqMu.Unlock()
			go p.session.createPPTFromSnapshot(reqRef, reqSnapshot)
			return
		}
		req.Status = "collecting"
		req.UpdatedAt = time.Now().UnixMilli()
		req.RefreshCollectedFields()
		collected := req.CollectedFields
		missing := req.GetMissingFields()
		p.session.reqMu.Unlock()

		p.session.SendJSON(WSMessage{
			Type:            "requirements_progress",
			Status:          "collecting",
			CollectedFields: collected,
			MissingFields:   missing,
		})
		return

	case "collecting":
		if strings.Contains(llmResponse, "[REQUIREMENTS_CONFIRMED]") {
			req.Status = "confirming"
			req.UpdatedAt = time.Now().UnixMilli()
			req.RefreshCollectedFields()
			summaryText := req.BuildSummaryText()
			reqSnapshot := CloneTaskRequirements(req)
			p.session.reqMu.Unlock()

			p.session.SendJSON(WSMessage{
				Type:         "requirements_summary",
				SummaryText:  summaryText,
				Requirements: reqSnapshot,
			})
			return
		}

		req.RefreshCollectedFields()
		req.UpdatedAt = time.Now().UnixMilli()
		missing := req.GetMissingFields()
		collected := req.CollectedFields
		p.session.reqMu.Unlock()

		p.session.SendJSON(WSMessage{
			Type:            "requirements_progress",
			Status:          "collecting",
			CollectedFields: collected,
			MissingFields:   missing,
		})
		return

	default:
		p.session.reqMu.Unlock()
	}
}

func (p *Pipeline) buildTaskListContext() string {
	p.session.activeTaskMu.RLock()
	defer p.session.activeTaskMu.RUnlock()
	if len(p.session.OwnedTasks) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n\n[系统提示 - 当前用户的 PPT 任务列表]\n")
	for tid, topic := range p.session.OwnedTasks {
		marker := ""
		if tid == p.session.ActiveTaskID {
			marker = " (当前活跃)"
		}
		sb.WriteString(fmt.Sprintf("- task_id=%s, 主题=\"%s\"%s\n", tid, topic, marker))
	}
	if len(p.session.OwnedTasks) > 1 {
		sb.WriteString("\n用户可能用简称、缩写、别名来指代某个任务（例如用\"高数\"指\"高等数学\"）。\n")
		sb.WriteString("请根据语义判断用户说的是哪个任务。如果确实无法判断，主动追问用户，绝不要猜。\n")
		sb.WriteString("默认操作当前活跃的任务，除非用户明确提到了其他任务。\n")
	}
	return sb.String()
}

func (p *Pipeline) buildPendingQuestionsContext() string {
	p.session.pendingQMu.RLock()
	defer p.session.pendingQMu.RUnlock()
	if len(p.session.PendingQuestions) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n\n[系统提示 - 待回答的冲突问题]\n")
	sb.WriteString("以下是 PPT Agent 提出的需要用户确认的问题，请判断用户是否在回答这些问题：\n")
	for cid, tid := range p.session.PendingQuestions {
		sb.WriteString(fmt.Sprintf("- context_id=%s, task_id=%s\n", cid, tid))
	}
	if len(p.session.PendingQuestions) > 1 {
		sb.WriteString("\n有多个待确认问题，请在回复末尾标注你判断用户回答的是哪个问题：")
		sb.WriteString("[RESOLVE_CONFLICT:context_id值]\n")
	}
	return sb.String()
}

// ttsWorker reads sentences from the channel, synthesizes audio, and sends
// WAV chunks back to the browser.
func (p *Pipeline) ttsWorker(ctx context.Context, sentenceCh <-chan string) {
	for {
		select {
		case sentence, ok := <-sentenceCh:
			if !ok {
				return
			}

			sentence = strings.TrimSpace(sentence)
			if sentence == "" {
				continue
			}

			log.Printf("TTS: %s", truncate(sentence, 60))

			audioCh, err := p.ttsClient.Synthesize(ctx, sentence, p.adaptive.Get("tts_chunk_ch"))
			if err != nil {
				log.Printf("tts synthesize: %v", err)
				continue
			}
			for chunk := range audioCh {
				if ctx.Err() != nil {
					return
				}
				p.session.SendAudio(chunk)
			}

		case <-ctx.Done():
			return
		}
	}
}

func (p *Pipeline) launchAsyncContextQueries(ctx context.Context, query string) {
	if p.clients == nil {
		return
	}

	userID := p.session.UserID
	sessionID := p.session.SessionID

	var kbTopScoreMu sync.Mutex
	kbTopScore := 0.0 // KB 没结果时 score=0，应触发搜索结果沉淀
	kbScoreReady := make(chan struct{})
	var kbScoreReadyOnce sync.Once
	markKBReady := func() {
		kbScoreReadyOnce.Do(func() { close(kbScoreReady) })
	}

	p.asyncQuery(ctx, "knowledge_base", "rag_chunks", func() (string, error) {
		defer markKBReady()
		resp, err := p.clients.QueryKB(ctx, KBQueryRequest{
			UserID:         userID,
			Query:          query,
			TopK:           5,
			ScoreThreshold: 0.5,
		})
		if err != nil {
			return "", err
		}
		if len(resp.Chunks) > 0 {
			kbTopScoreMu.Lock()
			kbTopScore = resp.Chunks[0].Score
			kbTopScoreMu.Unlock()
		}
		return formatChunksForLLM(resp.Chunks), nil
	})

	p.asyncQuery(ctx, "memory", "memory_recall", func() (string, error) {
		resp, err := p.clients.RecallMemory(ctx, MemoryRecallRequest{
			UserID:    userID,
			SessionID: sessionID,
			Query:     query,
			TopK:      10,
		})
		if err != nil {
			return "", err
		}
		return formatMemoryForLLM(resp), nil
	})

	p.asyncQuery(ctx, "web_search", "search_result", func() (string, error) {
		resp, err := p.clients.SearchWeb(ctx, SearchRequest{
			RequestID:  NewID("search_"),
			UserID:     userID,
			Query:      query,
			MaxResults: 5,
			Language:   "zh",
			SearchType: "general",
		})
		if err != nil {
			return "", err
		}

		// Wait briefly for KB score so ingest decision uses KB result when available.
		select {
		case <-kbScoreReady:
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}

		kbTopScoreMu.Lock()
		shouldIngest := kbTopScore < 0.5
		kbTopScoreMu.Unlock()
		if shouldIngest && len(resp.Results) > 0 {
			items := make([]SearchIngestItem, 0, len(resp.Results))
			for _, r := range resp.Results {
				items = append(items, SearchIngestItem{
					Title:   r.Title,
					URL:     r.URL,
					Content: r.Snippet,
					Source:  r.Source,
				})
			}
			go func(items []SearchIngestItem) {
				if err := p.clients.IngestFromSearch(context.Background(), IngestFromSearchRequest{
					UserID: userID,
					Items:  items,
				}); err != nil {
					log.Printf("[context-bus] ingest-from-search failed: %v", err)
				}
			}(items)
		}
		return formatSearchForLLM(resp), nil
	})
}

func formatChunksForLLM(chunks []RetrievedChunk) string {
	if len(chunks) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("知识库检索结果：\n")
	for i, c := range chunks {
		sb.WriteString(fmt.Sprintf("%d) [%s] %s (score=%.2f)\n", i+1, c.DocTitle, c.Content, c.Score))
	}
	return strings.TrimSpace(sb.String())
}

func formatMemoryForLLM(resp MemoryRecallResponse) string {
	var sb strings.Builder
	if len(resp.Facts) > 0 {
		sb.WriteString("相关事实记忆：\n")
		for i, f := range resp.Facts {
			text := strings.TrimSpace(f.Content)
			if text == "" {
				text = strings.TrimSpace(f.Value)
			}
			if text == "" {
				continue
			}
			sb.WriteString(fmt.Sprintf("%d) %s\n", i+1, text))
		}
	}
	if len(resp.Preferences) > 0 {
		sb.WriteString("相关偏好：\n")
		for i, f := range resp.Preferences {
			text := strings.TrimSpace(f.Content)
			if text == "" {
				text = strings.TrimSpace(f.Value)
			}
			if text == "" {
				continue
			}
			sb.WriteString(fmt.Sprintf("%d) %s\n", i+1, text))
		}
	}
	if resp.ProfileSummary != "" {
		sb.WriteString("画像摘要：")
		sb.WriteString(resp.ProfileSummary)
		sb.WriteString("\n")
	}
	return strings.TrimSpace(sb.String())
}

func formatSearchForLLM(resp SearchResponse) string {
	var sb strings.Builder
	if resp.Summary != "" {
		sb.WriteString("网络搜索摘要：")
		sb.WriteString(resp.Summary)
		sb.WriteString("\n")
	}
	if len(resp.Results) > 0 {
		sb.WriteString("搜索结果：\n")
		for i, r := range resp.Results {
			sb.WriteString(fmt.Sprintf("%d) %s - %s (%s)\n", i+1, r.Title, r.Snippet, r.URL))
		}
	}
	return strings.TrimSpace(sb.String())
}

// ---------------------------------------------------------------------------
// Interrupt detection (uses Small LLM via SDK Agent)
// ---------------------------------------------------------------------------

const interruptDetectionPrompt = `你是语音意图检测模型。给定一段ASR识别文本，判断其是否包含有意义的用户意图。
interrupt — 文本含有实际语义：问题、指令、陈述、确认、否定、哪怕是未说完的半句话。
do not interrupt — 文本仅为无语义噪声：语气词（嗯、啊、哦、呃、emm）、咳嗽/笑声的误识别、重复填充音、空白或乱码。
只输出 interrupt 或 do not interrupt，不要输出任何其他内容。`

func isInterrupt(ctx context.Context, agent *toolcalling.Agent, text string) bool {
	messages := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(interruptDetectionPrompt),
		openai.UserMessage(text),
	}
	resp, err := agent.ChatText(ctx, messages)
	if err != nil {
		log.Printf("interrupt detection error: %v", err)
		return true
	}
	// Small LLM is a thinking model (Qwen3.5-0.8B); strip <think>...</think>
	// before parsing the label, otherwise reasoning content that happens to
	// contain "interrupt" would cause false positives.
	resp = stripThinkTags(resp)
	label := strings.ToLower(strings.TrimSpace(resp))
	label = strings.Trim(label, " \t\r\n\"'`.,!?;:()[]{}<>，。！？；：")

	// Prefer exact label match first.
	switch label {
	case "interrupt":
		return true
	case "do not interrupt", "do-not-interrupt", "donotinterrupt":
		return false
	}

	// Fallback for noisy outputs, e.g. "interrupt." / "do not interrupt\n".
	if strings.Contains(label, "do not interrupt") || strings.Contains(label, "do-not-interrupt") {
		return false
	}
	if strings.Contains(label, "interrupt") {
		return true
	}

	// Conservative fallback: treat unknown output as interrupt.
	log.Printf("interrupt detection unexpected output: %q", resp)
	return true
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

var sentenceEnders = map[rune]bool{
	'。': true, '！': true, '？': true, '；': true,
	'.': true, '!': true, '?': true, ';': true,
	'，': true, ',': true, // also split on commas for faster streaming
	'\n': true,
}

func isSentenceEnd(s string) bool {
	s = strings.TrimRightFunc(s, unicode.IsSpace)
	if s == "" {
		return false
	}
	lastRune := []rune(s)[len([]rune(s))-1]
	return sentenceEnders[lastRune]
}

func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}

// ---------------------------------------------------------------------------
// LLM prompt fragments for intent detection
// ---------------------------------------------------------------------------

const pptIntentDetectionPrompt = `

[系统指令 - PPT 操作意图识别]
当用户表达想制作/创建PPT课件的意图时，请在回复末尾追加标记：
[TASK_INIT]{"topic":"用户提到的主题（如有）"}[/TASK_INIT]
例如用户说"帮我做一个关于高等数学的PPT"，你在正常回复后追加：
[TASK_INIT]{"topic":"高等数学"}[/TASK_INIT]

当用户对已有PPT提出修改/编辑/调整指令时，请在回复末尾追加标记：
[PPT_FEEDBACK]{"action_type":"modify|insert|delete|reorder|style","page_id":"","instruction":"用户的具体修改要求","scope":"page|global","keywords":[]}[/PPT_FEEDBACK]
action_type 取值：modify(修改内容)、insert(新增页面)、delete(删除页面)、reorder(调整顺序)、style(修改样式)
page_id：如果用户指定了某一页就填入，否则留空
scope：page(只改某页) 或 global(全局修改)

注意：这些标记不会展示给用户，仅供系统后处理使用。正常对话内容中不要提及这些标记。
`
