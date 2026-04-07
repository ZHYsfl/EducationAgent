package agent

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

var ErrUserIDRequired = errors.New("session: user_id is required")

func NewSession(conn *websocket.Conn, config *Config, clients ExternalServices, sessionID, userID string) (*Session, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, ErrUserIDRequired
	}
	if sessionID == "" {
		sessionID = newID("sess_")
	}
	s := &Session{
		conn:             conn,
		config:           config,
		SessionID:        sessionID,
		UserID:           userID,
		state:            StateIdle,
		writeCh:          make(chan writeItem, 256),
		done:             make(chan struct{}),
		clients:          clients,
		OwnedTasks:       make(map[string]string),
		PendingRequests:  make(map[string]string),
		PendingQuestions: make(map[string]PendingQuestion),
	}
	s.pipeline = NewPipeline(s, config, clients)
	go s.bootstrapMemory()
	return s, nil
}

func (s *Session) Run() {
	defer s.Close()
	go s.writeLoop()
	s.readLoop()
}

func (s *Session) Close() {
	s.once.Do(func() {
		close(s.done)
		s.cancelPipeline()
		if s.pipeline != nil {
			s.pipeline.pushRemainingContext()
		}
		if err := s.conn.Close(); err != nil {
			log.Printf("[session] close: %v", err)
		}
	})
}

// --- State machine ---

func (s *Session) GetState() SessionState {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	return s.state
}

func (s *Session) SetState(st SessionState) {
	s.stateMu.Lock()
	s.state = st
	s.stateMu.Unlock()
	s.SendJSON(WSMessage{Type: "status", Status: st.String()})
}

func (s *Session) CompareAndSetState(expected, next SessionState) bool {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if s.state != expected {
		return false
	}
	s.state = next
	return true
}

// --- Pipeline lifecycle ---

func (s *Session) newPipelineContext() context.Context {
	s.pipelineMu.Lock()
	defer s.pipelineMu.Unlock()
	if s.pipelineCancel != nil {
		s.pipelineCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.pipelineCancel = cancel
	return ctx
}

func (s *Session) cancelPipeline() {
	s.pipelineMu.Lock()
	defer s.pipelineMu.Unlock()
	if s.pipelineCancel != nil {
		s.pipelineCancel()
		s.pipelineCancel = nil
	}
}

// --- WebSocket write ---

func (s *Session) SendJSON(msg WSMessage) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	select {
	case s.writeCh <- writeItem{websocket.TextMessage, data}:
	case <-s.done:
	default:
	}
}

func (s *Session) SendAudio(data []byte) {
	select {
	case s.writeCh <- writeItem{websocket.BinaryMessage, data}:
	case <-s.done:
	default:
	}
}

func (s *Session) writeLoop() {
	for {
		select {
		case item := <-s.writeCh:
			if err := s.conn.WriteMessage(item.msgType, item.data); err != nil {
				return
			}
		case <-s.done:
			return
		}
	}
}

func (s *Session) readLoop() {
	for {
		msgType, data, err := s.conn.ReadMessage()
		if err != nil {
			return
		}
		if msgType == websocket.BinaryMessage {
			s.onAudioData(data)
			continue
		}
		var msg WSMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			log.Printf("[session] invalid JSON: %v", err)
			continue
		}
		s.handleMessage(msg)
	}
}

// --- Message dispatch ---

func (s *Session) handleMessage(msg WSMessage) {
	switch msg.Type {
	case "vad_start":
		s.onVADStart()
	case "vad_end":
		s.onVADEnd()
	case "text_input":
		s.onTextInput(msg.Text)
	case "page_navigate":
		s.onPageNavigate(msg)
	case "add_reference_files":
		s.onAddReferenceFiles(msg)
	}
}

func (s *Session) onAudioData(data []byte) {
	if s.GetState() == StateListening {
		s.pipeline.OnAudioData(data)
	}
}

func (s *Session) onVADStart() {
	s.publishVADEvent()
	switch s.GetState() {
	case StateIdle:
		s.SetState(StateListening)
		ctx := s.newPipelineContext()
		go s.pipeline.StartInteractive(ctx)
	case StateProcessing, StateSpeaking:
		s.pipeline.OnInterrupt()
		s.cancelPipeline()
		s.SetState(StateListening)
		ctx := s.newPipelineContext()
		go s.pipeline.StartInteractive(ctx)
	}
}

func (s *Session) onVADEnd() {
	if s.GetState() == StateListening {
		s.pipeline.OnVADEnd()
	}
}

func (s *Session) onTextInput(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	state := s.GetState()
	if state == StateListening || state == StateProcessing || state == StateSpeaking {
		s.pipeline.OnInterrupt()
		s.cancelPipeline()
	}
	ctx := s.newPipelineContext()
	go s.pipeline.startProcessing(ctx, text)
}

func (s *Session) onPageNavigate(msg WSMessage) {
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

func (s *Session) onAddReferenceFiles(msg WSMessage) {
	if len(msg.Files) == 0 {
		return
	}
	s.reqMu.Lock()
	if s.Requirements == nil {
		s.Requirements = NewTaskRequirements(s.SessionID, s.UserID)
	}
	for _, f := range msg.Files {
		if f.FileID == "" {
			continue
		}
		s.Requirements.ReferenceFiles = append(s.Requirements.ReferenceFiles, ReferenceFileReq{
			FileID:      f.FileID,
			FileURL:     f.FileURL,
			FileType:    f.FileType,
			Instruction: f.Instruction,
		})
	}
	s.Requirements.UpdatedAt = time.Now().UnixMilli()
	s.reqMu.Unlock()
}

func (s *Session) publishVADEvent() {
	ts := time.Now().UnixMilli()
	s.activeTaskMu.Lock()
	s.LastVADTimestamp = ts
	taskID := s.ActiveTaskID
	pageID := s.ViewingPageID
	s.activeTaskMu.Unlock()

	if s.clients == nil || taskID == "" {
		return
	}
	go func() {
		if err := s.clients.NotifyVADEvent(context.Background(), VADEvent{
			TaskID:        taskID,
			Timestamp:     ts,
			ViewingPageID: pageID,
		}); err != nil {
			log.Printf("[session] VADEvent: %v", err)
		}
	}()
}

// bootstrapMemory fires an async memory recall on session start.
// Result arrives later via POST /api/v1/voice/ppt_message (event_type: get_memory).
func (s *Session) bootstrapMemory() {
	if s.clients == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = s.clients.RecallMemory(ctx, MemoryRecallRequest{
		UserID:    s.UserID,
		SessionID: s.SessionID,
		Query:     "用户的个性化信息、教学风格、内容深度偏好、常用课件结构和其他可复用偏好。",
		TopK:      10,
	})
}

// --- Task accessors ---

func (s *Session) RegisterTask(taskID, topic string) {
	s.activeTaskMu.Lock()
	s.OwnedTasks[taskID] = topic
	s.activeTaskMu.Unlock()
}

func (s *Session) SetActiveTask(taskID string) {
	s.activeTaskMu.Lock()
	s.ActiveTaskID = taskID
	s.activeTaskMu.Unlock()
}

func (s *Session) GetActiveTask() string {
	s.activeTaskMu.RLock()
	defer s.activeTaskMu.RUnlock()
	return s.ActiveTaskID
}

func (s *Session) GetViewingPageID() string {
	s.activeTaskMu.RLock()
	defer s.activeTaskMu.RUnlock()
	return s.ViewingPageID
}

func (s *Session) GetOwnedTasks() map[string]string {
	s.activeTaskMu.RLock()
	defer s.activeTaskMu.RUnlock()
	out := make(map[string]string, len(s.OwnedTasks))
	for k, v := range s.OwnedTasks {
		out[k] = v
	}
	return out
}

func (s *Session) OwnsTask(taskID string) bool {
	s.activeTaskMu.RLock()
	defer s.activeTaskMu.RUnlock()
	_, ok := s.OwnedTasks[taskID]
	return ok
}

func (s *Session) GetRequirements() *TaskRequirements {
	s.reqMu.RLock()
	defer s.reqMu.RUnlock()
	return s.Requirements.Clone()
}

func (s *Session) AddPendingQuestion(contextID, taskID, pageID string, baseTS int64, question string) {
	if contextID == "" {
		return
	}
	s.pendingQMu.Lock()
	s.PendingQuestions[contextID] = PendingQuestion{
		TaskID:        taskID,
		PageID:        pageID,
		BaseTimestamp: baseTS,
		QuestionText:  question,
	}
	s.pendingQMu.Unlock()
}

func (s *Session) ResolvePendingQuestion(contextID string) (PendingQuestion, bool) {
	s.pendingQMu.Lock()
	defer s.pendingQMu.Unlock()
	pq, ok := s.PendingQuestions[contextID]
	if ok {
		delete(s.PendingQuestions, contextID)
	}
	return pq, ok
}
