package main

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
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
	config *Config

	SessionID string
	UserID    string

	state   SessionState
	stateMu sync.RWMutex

	pipeline *Pipeline
	clients  ExternalServices

	ActiveTaskID    string
	ViewingPageID   string
	LastVADTimestamp int64
	OwnedTasks      map[string]string // task_id → topic（该 session 拥有的所有任务）
	activeTaskMu    sync.RWMutex

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

func NewSession(conn *websocket.Conn, config *Config, clients ExternalServices, sessionID, userID string) *Session {
	if sessionID == "" {
		sessionID = NewID("sess_")
	}
	if userID == "" {
		userID = config.DefaultUserID
	}
	sizes := LoadChannelSizes(config.AdaptiveSizesFile, DefaultChannelSizes())
	s := &Session{
		conn:             conn,
		config:           config,
		SessionID:        sessionID,
		UserID:           userID,
		state:            StateIdle,
		writeCh:          make(chan writeItem, sizes.WriteCh),
		done:             make(chan struct{}),
		clients:          clients,
		OwnedTasks:       make(map[string]string),
		PendingQuestions: make(map[string]string),
	}
	s.pipeline = NewPipeline(s, config, clients)
	return s
}

func (s *Session) Run() {
	defer s.Close()
	go s.writeLoop()
	s.readLoop()
}

func (s *Session) Close() {
	s.once.Do(func() {
		close(s.done)
		s.cancelCurrentPipeline()
		s.conn.Close()
	})
}

func (s *Session) GetState() SessionState {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	return s.state
}

func (s *Session) SetState(state SessionState) {
	s.stateMu.Lock()
	s.state = state
	s.stateMu.Unlock()
	s.SendJSON(WSMessage{Type: "status", State: state.String()})
}

func (s *Session) cancelCurrentPipeline() {
	s.pipelineMu.Lock()
	if s.pipelineCancel != nil {
		s.pipelineCancel()
		s.pipelineCancel = nil
	}
	s.pipelineMu.Unlock()
}

func (s *Session) newPipelineContext() context.Context {
	s.pipelineMu.Lock()
	if s.pipelineCancel != nil {
		s.pipelineCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.pipelineCancel = cancel
	s.pipelineMu.Unlock()
	return ctx
}

func (s *Session) SendJSON(msg WSMessage) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	if s.pipeline != nil {
		s.pipeline.adaptive.RecordLen("write_ch", len(s.writeCh))
	}
	select {
	case s.writeCh <- writeItem{msgType: websocket.TextMessage, data: data}:
	case <-s.done:
	}
}

func (s *Session) SendAudio(data []byte) {
	if s.pipeline != nil {
		s.pipeline.adaptive.RecordLen("write_ch", len(s.writeCh))
	}
	select {
	case s.writeCh <- writeItem{msgType: websocket.BinaryMessage, data: data}:
	case <-s.done:
	}
}

func (s *Session) readLoop() {
	for {
		msgType, data, err := s.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("WebSocket read error: %v", err)
			}
			return
		}

		switch msgType {
		case websocket.TextMessage:
			var msg WSMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				log.Printf("Invalid JSON: %v", err)
				continue
			}
			s.handleTextMessage(msg)

		case websocket.BinaryMessage:
			s.handleAudioData(data)
		}
	}
}

func (s *Session) writeLoop() {
	for {
		select {
		case item := <-s.writeCh:
			if err := s.conn.WriteMessage(item.msgType, item.data); err != nil {
				log.Printf("WebSocket write error: %v", err)
				return
			}
		case <-s.done:
			return
		}
	}
}

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
	case "task_init":
		s.handleTaskInit(msg)
	case "requirements_confirm":
		s.handleRequirementsConfirm(msg)
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

func (s *Session) handleTaskInit(msg WSMessage) {
	req := NewTaskRequirements(s.SessionID, s.UserID)
	req.Topic = strings.TrimSpace(msg.Topic)
	req.TotalPages = msg.TotalPages
	req.TargetAudience = strings.TrimSpace(msg.Audience)
	req.GlobalStyle = strings.TrimSpace(msg.GlobalStyle)
	req.AdditionalNotes = strings.TrimSpace(msg.Description)
	req.UpdatedAt = time.Now().UnixMilli()

	s.prefillFromMemory(req)
	req.RefreshCollectedFields()
	reqSnapshot := CloneTaskRequirements(req)

	s.reqMu.Lock()
	s.Requirements = req
	s.reqMu.Unlock()

	s.SendJSON(WSMessage{
		Type:            "requirements_progress",
		Status:          req.Status,
		CollectedFields: req.CollectedFields,
		MissingFields:   req.GetMissingFields(),
		Requirements:    reqSnapshot,
	})
}

func (s *Session) prefillFromMemory(req *TaskRequirements) {
	if s.clients == nil {
		return
	}
	profile, err := s.clients.GetUserProfile(context.Background(), req.UserID)
	if err != nil {
		return
	}
	if profile.Subject != "" && req.TargetAudience == "" {
		req.TargetAudience = profile.Subject + "专业学生"
	}
	if style, ok := profile.VisualPreferences["color_scheme"]; ok && req.GlobalStyle == "" {
		req.GlobalStyle = style
	}
}

func (s *Session) handleRequirementsConfirm(msg WSMessage) {
	s.reqMu.Lock()
	if s.Requirements == nil {
		s.reqMu.Unlock()
		return
	}
	if msg.Confirmed != nil && *msg.Confirmed {
		reqRef := s.Requirements
		s.Requirements.Status = "confirmed"
		s.Requirements.UpdatedAt = time.Now().UnixMilli()
		reqSnapshot := CloneTaskRequirements(s.Requirements)
		s.reqMu.Unlock()
		go s.createPPTFromSnapshot(reqRef, reqSnapshot)
		return
	}
	if msg.Modifications != "" {
		s.Requirements.Status = "collecting"
		s.Requirements.AdditionalNotes = strings.TrimSpace(
			strings.TrimSpace(s.Requirements.AdditionalNotes) + "\n用户修改意见: " + msg.Modifications,
		)
	}
	s.Requirements.UpdatedAt = time.Now().UnixMilli()
	s.reqMu.Unlock()
}

func (s *Session) createPPTFromRequirements() {
	s.reqMu.RLock()
	reqRef := s.Requirements
	req := CloneTaskRequirements(reqRef)
	s.reqMu.RUnlock()
	s.createPPTFromSnapshot(reqRef, req)
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
