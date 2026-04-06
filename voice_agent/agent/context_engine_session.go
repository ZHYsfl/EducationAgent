package agent

import (
	"log"
	"time"
)

// ---------------------------------------------------------------------------
// Session Context State Writes
// ---------------------------------------------------------------------------

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

func (s *Session) AddPendingQuestion(contextID, taskID, pageID string, baseTimestamp int64, questionText string) {
	if contextID == "" {
		return
	}
	s.pendingQMu.Lock()
	s.PendingQuestions[contextID] = PendingQuestion{
		TaskID:        taskID,
		PageID:        pageID,
		BaseTimestamp: baseTimestamp,
		QuestionText:  questionText,
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

func (s *Session) SetActiveTask(taskID string) {
	s.activeTaskMu.Lock()
	s.ActiveTaskID = taskID
	tasks := make(map[string]string)
	for k, v := range s.OwnedTasks {
		tasks[k] = v
	}
	s.activeTaskMu.Unlock()

	s.SendJSON(WSMessage{
		Type:         "task_list_update",
		ActiveTaskID: taskID,
		Tasks:        tasks,
	})
}

func (s *Session) RegisterTask(taskID, topic string) {
	s.activeTaskMu.Lock()
	s.OwnedTasks[taskID] = topic
	tasks := make(map[string]string)
	for k, v := range s.OwnedTasks {
		tasks[k] = v
	}
	activeTaskID := s.ActiveTaskID
	s.activeTaskMu.Unlock()

	s.SendJSON(WSMessage{
		Type:         "task_list_update",
		ActiveTaskID: activeTaskID,
		Tasks:        tasks,
	})
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

func (s *Session) handleAddReferenceFiles(msg WSMessage) {
	if len(msg.Files) == 0 {
		return
	}

	s.reqMu.Lock()
	req := s.Requirements
	if req == nil {
		req = NewTaskRequirements(s.SessionID, s.UserID)
		s.Requirements = req
	}

	for _, f := range msg.Files {
		if f.FileID == "" {
			continue
		}
		req.ReferenceFiles = append(req.ReferenceFiles, ReferenceFileReq{
			FileID:      f.FileID,
			FileURL:     f.FileURL,
			FileType:    f.FileType,
			Instruction: f.Instruction,
		})
	}
	req.RefreshCollectedFields()
	req.UpdatedAt = time.Now().UnixMilli()
	s.reqMu.Unlock()

	log.Printf("[session] added %d reference file(s)", len(msg.Files))
}
