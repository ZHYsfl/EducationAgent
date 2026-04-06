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
	case "add_reference_files":
		s.handleAddReferenceFiles(msg)
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
		go s.pipeline.StartInteractive(ctx)

	case StateProcessing, StateSpeaking:
		// User interrupts: preserve history, start new listening
		s.pipeline.OnInterrupt()
		s.cancelCurrentPipeline()
		s.SetState(StateListening)
		ctx := s.newPipelineContext()
		go s.pipeline.StartInteractive(ctx)
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
