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

func (s *Session) RegisterRequest(requestID, reqType string) {
	s.activeTaskMu.Lock()
	s.PendingRequests[requestID] = reqType
	s.activeTaskMu.Unlock()
}

func (s *Session) OwnsRequest(requestID string) bool {
	s.activeTaskMu.RLock()
	defer s.activeTaskMu.RUnlock()
	_, ok := s.PendingRequests[requestID]
	return ok
}
