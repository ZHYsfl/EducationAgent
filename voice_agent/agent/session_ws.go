package agent

import (
	"context"
	"log"
	"strings"
	"time"
)

func (s *Session) handleTextMessage(msg WSMessage) {
	switch msg.Type {
	case "vad_start":
		s.onVADStart()
	case "vad_end":
		s.onVADEnd()
	case "text_input":
		s.handleTextInput(msg)
	case "page_navigate":
		s.handlePageNavigate(msg)
	}
}

func (s *Session) handleAudioData(data []byte) {
	state := s.GetState()
	if state == StateListening {
		s.pipeline.OnAudioData(data)
	}
}

func (s *Session) onVADStart() {
	state := s.GetState()

	s.publishVADEvent()

	switch state {
	case StateIdle:
		s.SetState(StateListening)
		ctx := s.newPipelineContext()
		go s.pipeline.StartListening(ctx)

	case StateProcessing, StateSpeaking:
		// User interrupts — preserve history, start new listening
		s.pipeline.OnInterrupt()
		s.cancelCurrentPipeline()
		s.SetState(StateListening)
		ctx := s.newPipelineContext()
		go s.pipeline.StartListening(ctx)
	}
}

func (s *Session) publishVADEvent() {
	ts := time.Now().UnixMilli()
	s.activeTaskMu.Lock()
	s.LastVADTimestamp = ts
	s.activeTaskMu.Unlock()

	if s.clients == nil {
		return
	}
	taskID := s.GetActiveTask()
	if taskID == "" {
		return
	}
	go func() {
		if err := s.clients.NotifyVADEvent(context.Background(), VADEvent{
			TaskID:        taskID,
			Timestamp:     ts,
			ViewingPageID: s.GetViewingPageID(),
		}); err != nil {
			log.Printf("[session] VADEvent notify failed: %v", err)
		}
	}()
}

func (s *Session) GetLastVADTimestamp() int64 {
	s.activeTaskMu.RLock()
	defer s.activeTaskMu.RUnlock()
	return s.LastVADTimestamp
}

func (s *Session) onVADEnd() {
	state := s.GetState()
	if state == StateListening {
		s.pipeline.OnVADEnd()
	}
}

func (s *Session) handleTextInput(msg WSMessage) {
	text := strings.TrimSpace(msg.Text)
	if text == "" {
		return
	}
	state := s.GetState()
	if state == StateProcessing || state == StateSpeaking {
		s.pipeline.OnInterrupt()
		s.cancelCurrentPipeline()
	}
	ctx := s.newPipelineContext()
	go s.pipeline.startProcessing(ctx, text)
}

func (s *Session) handlePageNavigate(msg WSMessage) {
	if msg.TaskID == "" {
		return
	}
	s.activeTaskMu.Lock()
	s.ActiveTaskID = msg.TaskID
	if msg.PageID != "" {
		s.ViewingPageID = msg.PageID
	}
	s.activeTaskMu.Unlock()
}

func (s *Session) GetViewingPageID() string {
	s.activeTaskMu.RLock()
	defer s.activeTaskMu.RUnlock()
	return s.ViewingPageID
}

func (s *Session) prefillFromMemory(req *TaskRequirements) {
	if s.clients == nil {
		return
	}
	profile, err := s.clients.GetUserProfile(context.Background(), req.UserID)
	if err != nil {
		return
	}

	// Direct field mappings
	if profile.Subject != "" && req.Subject == "" {
		req.Subject = profile.Subject
	}
	if profile.TeachingStyle != "" && req.GlobalStyle == "" {
		req.GlobalStyle = profile.TeachingStyle
	}
	if profile.ContentDepth != "" && req.AdditionalNotes == "" {
		req.AdditionalNotes = "内容深度: " + profile.ContentDepth
	}
	if profile.School != "" && req.TargetAudience == "" {
		req.TargetAudience = profile.School
	}
	if profile.HistorySummary != "" {
		if req.AdditionalNotes == "" {
			req.AdditionalNotes = profile.HistorySummary
		} else {
			req.AdditionalNotes += "\n" + profile.HistorySummary
		}
	}

	// VisualPreferences mapping
	for key, value := range profile.VisualPreferences {
		if value == "" {
			continue
		}
		switch key {
		case "color_scheme":
			if req.GlobalStyle == "" {
				req.GlobalStyle = value
			}
		case "layout_style":
			if req.InteractionDesign == "" {
				req.InteractionDesign = value
			}
		}
	}

	// General Preferences mapping
	for key, value := range profile.Preferences {
		if value == "" {
			continue
		}
		switch key {
		case "default_duration":
			if req.Duration == "" {
				req.Duration = value
			}
		case "interaction_level":
			if req.InteractionDesign == "" {
				req.InteractionDesign = value
			}
		}
	}
}

func (s *Session) createPPTFromSnapshot(reqRef, req *TaskRequirements) {
	if req == nil || s.clients == nil {
		return
	}

	initReq := req.ToPPTInitRequest()
	resp, err := s.clients.InitPPT(context.Background(), initReq)
	if err != nil {
		log.Printf("[session] InitPPT failed: %v", err)
		s.SendJSON(WSMessage{Type: "error", Code: 50200, Message: "PPT创建失败: " + err.Error()})
		return
	}

	s.RegisterTask(resp.TaskID, initReq.Topic)
	s.SetActiveTask(resp.TaskID)
	registerTask(resp.TaskID, s.SessionID)
	s.reqMu.Lock()
	if s.Requirements == reqRef && s.Requirements != nil {
		s.Requirements.Status = "generating"
		s.Requirements.UpdatedAt = time.Now().UnixMilli()
	}
	s.reqMu.Unlock()

	s.SendJSON(WSMessage{
		Type:   "task_created",
		TaskID: resp.TaskID,
		Topic:  initReq.Topic,
	})

	s.speakText("好的，正在为您生成课件，请稍候。")
}

func (s *Session) speakText(text string) {
	if s.pipeline == nil {
		return
	}
	s.SetState(StateSpeaking)
	sentenceCh := make(chan string, 1)
	sentenceCh <- text
	close(sentenceCh)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s.pipeline.ttsWorker(ctx, sentenceCh)
	s.SetState(StateIdle)
}

func (s *Session) AddPendingQuestion(contextID, taskID string) {
	if contextID == "" {
		return
	}
	s.pendingQMu.Lock()
	s.PendingQuestions[contextID] = taskID
	s.pendingQMu.Unlock()
}

func (s *Session) ResolvePendingQuestion(contextID string) (string, bool) {
	s.pendingQMu.Lock()
	defer s.pendingQMu.Unlock()
	taskID, ok := s.PendingQuestions[contextID]
	if ok {
		delete(s.PendingQuestions, contextID)
	}
	return taskID, ok
}

func (s *Session) SetActiveTask(taskID string) {
	s.activeTaskMu.Lock()
	s.ActiveTaskID = taskID
	s.activeTaskMu.Unlock()
}

func (s *Session) RegisterTask(taskID, topic string) {
	s.activeTaskMu.Lock()
	s.OwnedTasks[taskID] = topic
	s.activeTaskMu.Unlock()
}

func (s *Session) OwnsTask(taskID string) bool {
	s.activeTaskMu.RLock()
	defer s.activeTaskMu.RUnlock()
	_, ok := s.OwnedTasks[taskID]
	return ok
}

func (s *Session) GetActiveTask() string {
	s.activeTaskMu.RLock()
	defer s.activeTaskMu.RUnlock()
	return s.ActiveTaskID
}

func (s *Session) GetAllTasks() []string {
	s.activeTaskMu.RLock()
	defer s.activeTaskMu.RUnlock()
	tasks := make([]string, 0, len(s.OwnedTasks))
	for tid := range s.OwnedTasks {
		tasks = append(tasks, tid)
	}
	return tasks
}

// ResolveTaskID implements §0.5 task routing rules (deterministic part).
// Rule 1 (conflict reply) is handled in tryResolveConflict.
// Rule 2 (fuzzy topic matching) and Rule 5 (ask user) are delegated to LLM
// via buildTaskListContext system prompt injection.
// Returns (task_id, resolved). resolved=false means LLM should decide.
func (s *Session) ResolveTaskID() (string, bool) {
	s.activeTaskMu.RLock()
	defer s.activeTaskMu.RUnlock()

	// Rule 3: active_task_id exists
	if s.ActiveTaskID != "" {
		if _, ok := s.OwnedTasks[s.ActiveTaskID]; ok {
			return s.ActiveTaskID, true
		}
	}

	// Rule 4: only 1 task
	if len(s.OwnedTasks) == 1 {
		for tid := range s.OwnedTasks {
			return tid, true
		}
	}

	// Rule 2 & 5: LLM handles fuzzy matching / asking user
	return "", false
}
