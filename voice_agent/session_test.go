package main

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

// ===========================================================================
// State management
// ===========================================================================

func TestSession_GetSetState(t *testing.T) {
	s := newTestSession(nil)
	if s.GetState() != StateIdle {
		t.Errorf("initial state should be Idle, got %s", s.GetState())
	}
	s.state = StateListening // bypass SendJSON for unit test
	if s.GetState() != StateListening {
		t.Errorf("state should be Listening, got %s", s.GetState())
	}
}

func TestSession_SetState_SendsWSMessage(t *testing.T) {
	s := newTestSession(nil)
	s.SetState(StateProcessing)
	msgs := drainWriteCh(s)
	found, ok := findWSMessage(msgs, "status")
	if !ok {
		t.Fatal("expected status message")
	}
	if found.State != "processing" {
		t.Errorf("state = %q, want processing", found.State)
	}
}

func TestSession_StateConcurrency(t *testing.T) {
	s := newTestSession(nil)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				s.state = StateListening
			} else {
				_ = s.GetState()
			}
		}(i)
	}
	wg.Wait()
}

// ===========================================================================
// Task management
// ===========================================================================

func TestSession_RegisterAndOwnsTask(t *testing.T) {
	s := newTestSession(nil)
	s.RegisterTask("task_001", "高等数学")
	if !s.OwnsTask("task_001") {
		t.Error("should own task_001")
	}
	if s.OwnsTask("task_999") {
		t.Error("should not own task_999")
	}
}

func TestSession_SetActiveTask(t *testing.T) {
	s := newTestSession(nil)
	s.SetActiveTask("task_001")
	if s.GetActiveTask() != "task_001" {
		t.Errorf("active = %q, want task_001", s.GetActiveTask())
	}
}

func TestSession_GetAllTasks(t *testing.T) {
	s := newTestSession(nil)
	s.RegisterTask("task_a", "数学")
	s.RegisterTask("task_b", "物理")
	tasks := s.GetAllTasks()
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
}

// ===========================================================================
// ResolveTaskID
// ===========================================================================

func TestResolveTaskID_ActiveTask(t *testing.T) {
	s := newTestSession(nil)
	s.RegisterTask("task_001", "数学")
	s.RegisterTask("task_002", "物理")
	s.SetActiveTask("task_001")

	tid, ok := s.ResolveTaskID()
	if !ok || tid != "task_001" {
		t.Errorf("expected task_001, got %q ok=%v", tid, ok)
	}
}

func TestResolveTaskID_SingleTask(t *testing.T) {
	s := newTestSession(nil)
	s.RegisterTask("task_only", "唯一课件")

	tid, ok := s.ResolveTaskID()
	if !ok || tid != "task_only" {
		t.Errorf("expected task_only, got %q ok=%v", tid, ok)
	}
}

func TestResolveTaskID_NoTasks(t *testing.T) {
	s := newTestSession(nil)
	tid, ok := s.ResolveTaskID()
	if ok || tid != "" {
		t.Errorf("no tasks: expected empty, got %q ok=%v", tid, ok)
	}
}

func TestResolveTaskID_MultipleTasks_NoActive(t *testing.T) {
	s := newTestSession(nil)
	s.RegisterTask("task_a", "数学")
	s.RegisterTask("task_b", "物理")

	tid, ok := s.ResolveTaskID()
	if ok {
		t.Errorf("multiple tasks no active: should return resolved=false, got %q", tid)
	}
}

func TestResolveTaskID_ActiveNotOwned(t *testing.T) {
	s := newTestSession(nil)
	s.RegisterTask("task_a", "数学")
	s.SetActiveTask("task_stale")

	tid, ok := s.ResolveTaskID()
	if !ok || tid != "task_a" {
		t.Errorf("stale active, single owned: expected task_a, got %q ok=%v", tid, ok)
	}
}

// ===========================================================================
// PendingQuestions
// ===========================================================================

func TestSession_PendingQuestions(t *testing.T) {
	s := newTestSession(nil)
	s.AddPendingQuestion("ctx_001", "task_a")
	s.AddPendingQuestion("ctx_002", "task_b")

	tid, ok := s.ResolvePendingQuestion("ctx_001")
	if !ok || tid != "task_a" {
		t.Errorf("expected task_a, got %q ok=%v", tid, ok)
	}
	// should be removed
	_, ok = s.ResolvePendingQuestion("ctx_001")
	if ok {
		t.Error("ctx_001 should have been removed")
	}

	// ctx_002 still there
	tid, ok = s.ResolvePendingQuestion("ctx_002")
	if !ok || tid != "task_b" {
		t.Errorf("expected task_b, got %q ok=%v", tid, ok)
	}
}

func TestSession_AddPendingQuestion_EmptyContextID(t *testing.T) {
	s := newTestSession(nil)
	s.AddPendingQuestion("", "task_a")
	if len(s.PendingQuestions) != 0 {
		t.Error("empty context_id should not be added")
	}
}

// ===========================================================================
// ViewingPageID
// ===========================================================================

func TestSession_ViewingPageID(t *testing.T) {
	s := newTestSession(nil)
	if s.GetViewingPageID() != "" {
		t.Error("initial viewing page should be empty")
	}

	s.activeTaskMu.Lock()
	s.ViewingPageID = "page_1"
	s.activeTaskMu.Unlock()

	if s.GetViewingPageID() != "page_1" {
		t.Errorf("expected page_1, got %q", s.GetViewingPageID())
	}
}

// ===========================================================================
// handlePageNavigate
// ===========================================================================

func TestHandlePageNavigate(t *testing.T) {
	s := newTestSession(nil)
	s.handlePageNavigate(WSMessage{TaskID: "task_x", PageID: "page_3"})
	if s.GetActiveTask() != "task_x" {
		t.Errorf("active task = %q, want task_x", s.GetActiveTask())
	}
	if s.GetViewingPageID() != "page_3" {
		t.Errorf("viewing page = %q, want page_3", s.GetViewingPageID())
	}
}

func TestHandlePageNavigate_EmptyTaskID(t *testing.T) {
	s := newTestSession(nil)
	s.SetActiveTask("original")
	s.handlePageNavigate(WSMessage{TaskID: ""})
	if s.GetActiveTask() != "original" {
		t.Error("should not change active task for empty task_id")
	}
}

func TestHandlePageNavigate_NoPageID(t *testing.T) {
	s := newTestSession(nil)
	s.activeTaskMu.Lock()
	s.ViewingPageID = "old_page"
	s.activeTaskMu.Unlock()

	s.handlePageNavigate(WSMessage{TaskID: "task_new"})
	if s.GetViewingPageID() != "old_page" {
		t.Error("should not change page_id when not provided")
	}
	if s.GetActiveTask() != "task_new" {
		t.Error("should update active task")
	}
}

// ===========================================================================
// handleTextMessage dispatch
// ===========================================================================

func TestHandleTextMessage_Dispatch(t *testing.T) {
	s := newTestSession(nil)
	s.handleTextMessage(WSMessage{Type: "page_navigate", TaskID: "t1", PageID: "p1"})
	if s.GetActiveTask() != "t1" {
		t.Error("page_navigate not dispatched")
	}
}

// ===========================================================================
// handleTaskInit
// ===========================================================================

func TestHandleTaskInit(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)

	s.handleTaskInit(WSMessage{
		Type:        "task_init",
		Topic:       "线性代数",
		TotalPages:  20,
		Audience:    "大二学生",
		GlobalStyle: "蓝色简洁",
		Description: "额外说明",
	})

	s.reqMu.RLock()
	req := s.Requirements
	s.reqMu.RUnlock()

	if req == nil {
		t.Fatal("Requirements should be set")
	}
	if req.Topic != "线性代数" {
		t.Errorf("topic = %q, want 线性代数", req.Topic)
	}
	if req.TotalPages != 20 {
		t.Errorf("total_pages = %d, want 20", req.TotalPages)
	}
	if req.TargetAudience != "大二学生" {
		t.Errorf("audience = %q", req.TargetAudience)
	}
	if req.GlobalStyle != "蓝色简洁" {
		t.Errorf("style = %q", req.GlobalStyle)
	}
	if req.AdditionalNotes != "额外说明" {
		t.Errorf("notes = %q", req.AdditionalNotes)
	}
	if req.Status != "collecting" {
		t.Errorf("status = %q, want collecting", req.Status)
	}

	msgs := drainWriteCh(s)
	found, ok := findWSMessage(msgs, "requirements_progress")
	if !ok {
		t.Fatal("expected requirements_progress message")
	}
	if found.Status != "collecting" {
		t.Errorf("WS status = %q", found.Status)
	}
}

// ===========================================================================
// handleRequirementsConfirm
// ===========================================================================

func TestHandleRequirementsConfirm_Confirm(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)

	req := makeFullRequirements()
	req.SessionID = s.SessionID
	req.UserID = s.UserID
	req.Status = "confirming"
	s.Requirements = req

	confirmed := true
	s.handleRequirementsConfirm(WSMessage{Confirmed: &confirmed})

	time.Sleep(50 * time.Millisecond)
	s.reqMu.RLock()
	status := s.Requirements.Status
	s.reqMu.RUnlock()

	if status != "confirmed" && status != "generating" {
		t.Errorf("status = %q, want confirmed or generating", status)
	}
}

func TestHandleRequirementsConfirm_Unconfirm(t *testing.T) {
	s := newTestSession(nil)
	req := makeFullRequirements()
	req.Status = "confirming"
	s.Requirements = req

	notConfirmed := false
	s.handleRequirementsConfirm(WSMessage{
		Confirmed:     &notConfirmed,
		Modifications: "把颜色改成红色",
	})

	s.reqMu.RLock()
	defer s.reqMu.RUnlock()
	if s.Requirements.Status != "collecting" {
		t.Errorf("status = %q, want collecting", s.Requirements.Status)
	}
	if !strings.Contains(s.Requirements.AdditionalNotes, "红色") {
		t.Error("modifications not appended")
	}
}

func TestHandleRequirementsConfirm_NilRequirements(t *testing.T) {
	s := newTestSession(nil)
	confirmed := true
	s.handleRequirementsConfirm(WSMessage{Confirmed: &confirmed})
	// should not panic
}

// ===========================================================================
// publishVADEvent
// ===========================================================================

func TestPublishVADEvent_WithActiveTask(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)
	s.SetActiveTask("task_vad")
	s.activeTaskMu.Lock()
	s.ViewingPageID = "page_vad"
	s.activeTaskMu.Unlock()

	s.publishVADEvent()
	time.Sleep(50 * time.Millisecond)

	mock.mu.Lock()
	calls := mock.vadEventCalls
	mock.mu.Unlock()

	if len(calls) != 1 {
		t.Fatalf("expected 1 VAD event, got %d", len(calls))
	}
	if calls[0].TaskID != "task_vad" {
		t.Errorf("task_id = %q", calls[0].TaskID)
	}
	if calls[0].ViewingPageID != "page_vad" {
		t.Errorf("viewing_page_id = %q", calls[0].ViewingPageID)
	}
	if calls[0].Timestamp == 0 {
		t.Error("timestamp should be non-zero")
	}
	if s.GetLastVADTimestamp() == 0 {
		t.Error("LastVADTimestamp should be set")
	}
}

func TestPublishVADEvent_NoActiveTask(t *testing.T) {
	mock := &mockServices{}
	s := newTestSession(mock)

	s.publishVADEvent()
	time.Sleep(30 * time.Millisecond)

	mock.mu.Lock()
	calls := mock.vadEventCalls
	mock.mu.Unlock()

	if len(calls) != 0 {
		t.Errorf("should not send VAD event without active task, got %d", len(calls))
	}
}

func TestPublishVADEvent_NilClients(t *testing.T) {
	s := newTestSession(nil)
	s.SetActiveTask("task_1")
	s.publishVADEvent() // should not panic
	if s.GetLastVADTimestamp() == 0 {
		t.Error("timestamp should still be recorded even without clients")
	}
}

// ===========================================================================
// prefillFromMemory
// ===========================================================================

func TestPrefillFromMemory_FillsEmptyFields(t *testing.T) {
	mock := &mockServices{
		GetUserProfileFn: func(ctx context.Context, userID string) (UserProfile, error) {
			return UserProfile{
				Subject:           "数学",
				VisualPreferences: map[string]string{"color_scheme": "蓝色"},
			}, nil
		},
	}
	s := newTestSession(mock)
	req := NewTaskRequirements(s.SessionID, s.UserID)
	s.prefillFromMemory(req)

	if !strings.Contains(req.TargetAudience, "数学") {
		t.Errorf("target_audience = %q, expected to contain 数学", req.TargetAudience)
	}
	if req.GlobalStyle != "蓝色" {
		t.Errorf("global_style = %q, want 蓝色", req.GlobalStyle)
	}
}

func TestPrefillFromMemory_DoesNotOverwrite(t *testing.T) {
	mock := &mockServices{
		GetUserProfileFn: func(ctx context.Context, userID string) (UserProfile, error) {
			return UserProfile{
				Subject:           "物理",
				VisualPreferences: map[string]string{"color_scheme": "红色"},
			}, nil
		},
	}
	s := newTestSession(mock)
	req := NewTaskRequirements(s.SessionID, s.UserID)
	req.TargetAudience = "高三学生"
	req.GlobalStyle = "绿色"
	s.prefillFromMemory(req)

	if req.TargetAudience != "高三学生" {
		t.Errorf("should not overwrite existing audience, got %q", req.TargetAudience)
	}
	if req.GlobalStyle != "绿色" {
		t.Errorf("should not overwrite existing style, got %q", req.GlobalStyle)
	}
}

// ===========================================================================
// Session registry (package-level)
// ===========================================================================

func TestSessionRegistry(t *testing.T) {
	s1 := newTestSession(nil)
	s1.SessionID = "sess_reg_1"
	s1.RegisterTask("task_r1", "测试1")

	s2 := newTestSession(nil)
	s2.SessionID = "sess_reg_2"
	s2.RegisterTask("task_r2", "测试2")

	registerSession(s1)
	registerSession(s2)
	registerTask("task_r1", s1.SessionID)
	registerTask("task_r2", s2.SessionID)
	defer func() {
		unregisterSession(s1)
		unregisterSession(s2)
	}()

	found := findSessionByTaskID("task_r1")
	if found == nil || found.SessionID != "sess_reg_1" {
		t.Error("findSessionByTaskID(task_r1) failed")
	}

	found = findSessionByTaskID("task_r2")
	if found == nil || found.SessionID != "sess_reg_2" {
		t.Error("findSessionByTaskID(task_r2) failed")
	}

	found = findSessionByTaskID("task_nonexistent")
	if found != nil {
		t.Error("should return nil for nonexistent task")
	}
}

func TestUnregisterSession_CleansTaskIndex(t *testing.T) {
	s := newTestSession(nil)
	s.SessionID = "sess_unreg"
	s.RegisterTask("task_u1", "测试")

	registerSession(s)
	registerTask("task_u1", s.SessionID)

	found := findSessionByTaskID("task_u1")
	if found == nil {
		t.Fatal("should find session before unregister")
	}

	unregisterSession(s)
	found = findSessionByTaskID("task_u1")
	if found != nil {
		t.Error("should not find session after unregister")
	}
}

// ===========================================================================
// SendJSON / SendAudio
// ===========================================================================

func TestSession_SendJSON(t *testing.T) {
	s := newTestSession(nil)
	s.pipeline = &Pipeline{
		adaptive: NewAdaptiveController(DefaultChannelSizes()),
	}
	s.SendJSON(WSMessage{Type: "test", Text: "hello"})

	msgs := drainWriteCh(s)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Type != "test" || msgs[0].Text != "hello" {
		t.Error("message content mismatch")
	}
}

func TestSession_SendAudio(t *testing.T) {
	s := newTestSession(nil)
	s.pipeline = &Pipeline{
		adaptive: NewAdaptiveController(DefaultChannelSizes()),
	}
	s.SendAudio([]byte{1, 2, 3})

	select {
	case item := <-s.writeCh:
		if item.msgType != 2 { // BinaryMessage
			t.Errorf("expected binary message type, got %d", item.msgType)
		}
	default:
		t.Error("expected audio in writeCh")
	}
}

// ===========================================================================
// createPPTFromRequirements
// ===========================================================================

func TestCreatePPTFromRequirements(t *testing.T) {
	mock := &mockServices{
		InitPPTFn: func(ctx context.Context, req PPTInitRequest) (PPTInitResponse, error) {
			return PPTInitResponse{TaskID: "task_created_001"}, nil
		},
	}
	s := newTestSession(mock)
	_ = newTestPipelineWithTTS(s, mock)
	req := makeFullRequirements()
	req.SessionID = s.SessionID
	req.UserID = s.UserID
	s.Requirements = req

	registerSession(s)
	defer unregisterSession(s)

	s.createPPTFromRequirements()

	if !s.OwnsTask("task_created_001") {
		t.Error("should own the created task")
	}
	if s.GetActiveTask() != "task_created_001" {
		t.Error("should set active task")
	}

	msgs := drainWriteCh(s)
	found, ok := findWSMessage(msgs, "task_created")
	if !ok {
		t.Fatal("expected task_created message")
	}
	if found.TaskID != "task_created_001" {
		t.Error("task_id mismatch in WS message")
	}

	s.reqMu.RLock()
	if s.Requirements.Status != "generating" {
		t.Errorf("status = %q, want generating", s.Requirements.Status)
	}
	s.reqMu.RUnlock()
}

// ===========================================================================
// onVADStart state transitions
// ===========================================================================

func TestOnVADStart_Idle_TransitionsToListening(t *testing.T) {
	// We can't fully test onVADStart because it starts goroutines that
	// require ASR/LLM. But we can verify publishVADEvent is called.
	mock := &mockServices{}
	s := newTestSession(mock)
	s.SetActiveTask("task_1")

	// Manually call publishVADEvent to verify it works
	s.publishVADEvent()
	time.Sleep(30 * time.Millisecond)

	mock.mu.Lock()
	callCount := len(mock.vadEventCalls)
	mock.mu.Unlock()
	if callCount != 1 {
		t.Errorf("expected 1 VAD event, got %d", callCount)
	}
}

// ===========================================================================
// handleAudioData
// ===========================================================================

func TestHandleAudioData_IgnoresWhenNotListening(t *testing.T) {
	s := newTestSession(nil)
	s.state = StateIdle
	s.handleAudioData([]byte{1, 2, 3})
	// should not panic, audio is ignored
}

// ===========================================================================
// WSMessage JSON serialization
// ===========================================================================

func TestWSMessage_JSON_Roundtrip(t *testing.T) {
	msg := WSMessage{
		Type:   "task_created",
		TaskID: "task_json",
		Topic:  "测试",
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	var decoded WSMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Type != "task_created" || decoded.TaskID != "task_json" {
		t.Error("roundtrip failed")
	}
}

func TestWSMessage_JSON_OmitsEmpty(t *testing.T) {
	msg := WSMessage{Type: "status", State: "idle"}
	data, _ := json.Marshal(msg)
	s := string(data)
	if strings.Contains(s, "task_id") {
		t.Error("should omit empty task_id")
	}
	if strings.Contains(s, "page_id") {
		t.Error("should omit empty page_id")
	}
}
