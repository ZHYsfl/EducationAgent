package agent

import (
	"testing"
)

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
