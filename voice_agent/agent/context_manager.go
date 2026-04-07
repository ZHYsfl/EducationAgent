package agent

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// ContextSnapshot 完整上下文快照（可序列化为 JSON）
type ContextSnapshot struct {
	// 元信息
	Timestamp int64  `json:"timestamp"`
	SessionID string `json:"session_id"`
	UserID    string `json:"user_id"`

	// 需求信息
	Requirements *TaskRequirements `json:"requirements,omitempty"`

	// 任务列表
	Tasks        []TaskInfo `json:"tasks"`
	ActiveTaskID string     `json:"active_task_id"`

	// 待回答的冲突问题
	PendingQuestions []SnapshotPendingQuestion `json:"pending_questions"`

	// 上下文消息队列（RAG/搜索结果/PPT回调）
	ContextMessages []ContextMessage `json:"context_messages"`

	// 对话历史
	ConversationHistory []SnapshotConversationTurn `json:"conversation_history"`
}

// TaskInfo 任务信息摘要
type TaskInfo struct {
	TaskID        string `json:"task_id"`
	Topic         string `json:"topic"`
	Status        string `json:"status"`
	TotalPages    int    `json:"total_pages"`
	ViewingPageID string `json:"viewing_page_id"`
	CreatedAt     int64  `json:"created_at"`
}

// SnapshotPendingQuestion 快照中的冲突问题（避免与 session_types.go 中的 PendingQuestion 冲突）
type SnapshotPendingQuestion struct {
	ContextID     string `json:"context_id"`
	TaskID        string `json:"task_id"`
	PageID        string `json:"page_id"`
	Question      string `json:"question"`
	BaseTimestamp int64  `json:"base_timestamp"`
	AskedAt       int64  `json:"asked_at"`
}

// SnapshotConversationTurn 快照中的对话轮次（避免与 types.go 中的 ConversationTurn 冲突）
type SnapshotConversationTurn struct {
	Role    string `json:"role"` // "user" | "assistant"
	Content string `json:"content"`
}

// ContextChange 上下文变更记录
type ContextChange struct {
	Type   string `json:"type"`
	Desc   string `json:"desc"`
	Before any    `json:"before,omitempty"`
	After  any    `json:"after,omitempty"`
}

// ContextManager 统一的上下文管理器
type ContextManager struct {
	session  *Session
	pipeline *Pipeline
	mu       sync.RWMutex
}

// NewContextManager 创建上下文管理器
func NewContextManager(session *Session, pipeline *Pipeline) *ContextManager {
	return &ContextManager{
		session:  session,
		pipeline: pipeline,
	}
}

// Export 导出当前完整上下文快照
func (cm *ContextManager) Export() ContextSnapshot {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	snapshot := ContextSnapshot{
		Timestamp: time.Now().UnixMilli(),
		SessionID: cm.session.SessionID,
		UserID:    cm.session.UserID,
	}

	// 1. Requirements
	cm.session.reqMu.RLock()
	if cm.session.Requirements != nil {
		snapshot.Requirements = CloneTaskRequirements(cm.session.Requirements)
	}
	cm.session.reqMu.RUnlock()

	// 2. Tasks (从 OwnedTasks 读取)
	cm.session.activeTaskMu.RLock()
	snapshot.ActiveTaskID = cm.session.ActiveTaskID
	for taskID, topic := range cm.session.OwnedTasks {
		snapshot.Tasks = append(snapshot.Tasks, TaskInfo{
			TaskID:        taskID,
			Topic:         topic,
			Status:        "unknown", // Session 中没有存储 status
			TotalPages:    0,         // Session 中没有存储 total_pages
			ViewingPageID: cm.session.ViewingPageID,
			CreatedAt:     0, // Session 中没有存储 created_at
		})
	}
	cm.session.activeTaskMu.RUnlock()

	// 3. PendingQuestions
	cm.session.pendingQMu.RLock()
	for contextID, q := range cm.session.PendingQuestions {
		snapshot.PendingQuestions = append(snapshot.PendingQuestions, SnapshotPendingQuestion{
			ContextID:     contextID,
			TaskID:        q.TaskID,
			PageID:        q.PageID,
			Question:      q.QuestionText,
			BaseTimestamp: q.BaseTimestamp,
			AskedAt:       0, // Session 中没有存储 asked_at
		})
	}
	cm.session.pendingQMu.RUnlock()

	// 4. ContextMessages (从 pipeline 读取)
	if cm.session.pipeline != nil {
		cm.session.pipeline.pendingMu.Lock()
		snapshot.ContextMessages = append([]ContextMessage{}, cm.session.pipeline.pendingContexts...)
		cm.session.pipeline.pendingMu.Unlock()

		// 从队列中读取（非阻塞）
		for {
			select {
			case msg := <-cm.session.pipeline.contextQueue:
				snapshot.ContextMessages = append(snapshot.ContextMessages, msg)
			default:
				goto doneContextQueue
			}
		}
	doneContextQueue:
	}

	// 5. ConversationHistory
	if cm.session.pipeline != nil && cm.session.pipeline.history != nil {
		messages := cm.session.pipeline.history.Messages()
		for _, msg := range messages {
			snapshot.ConversationHistory = append(snapshot.ConversationHistory, SnapshotConversationTurn{
				Role:    msg.Role,
				Content: msg.Content,
			})
		}
	}

	return snapshot
}

// ExportJSON 导出为 JSON 字符串
func (cm *ContextManager) ExportJSON() (string, error) {
	snapshot := cm.Export()
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// Diff 对比两个快照，返回变更列表
func Diff(before, after ContextSnapshot) []ContextChange {
	var changes []ContextChange

	// 1. Requirements 变更
	if before.Requirements == nil && after.Requirements != nil {
		changes = append(changes, ContextChange{
			Type:  "requirements_created",
			Desc:  "Requirements initialized",
			After: after.Requirements,
		})
	} else if before.Requirements != nil && after.Requirements != nil {
		if before.Requirements.Status != after.Requirements.Status {
			changes = append(changes, ContextChange{
				Type:   "requirements_status_changed",
				Desc:   fmt.Sprintf("Status: %s → %s", before.Requirements.Status, after.Requirements.Status),
				Before: before.Requirements.Status,
				After:  after.Requirements.Status,
			})
		}
		if before.Requirements.Topic != after.Requirements.Topic {
			changes = append(changes, ContextChange{
				Type:   "requirements_topic_changed",
				Desc:   fmt.Sprintf("Topic: %s → %s", before.Requirements.Topic, after.Requirements.Topic),
				Before: before.Requirements.Topic,
				After:  after.Requirements.Topic,
			})
		}
		// 可以继续添加其他字段的对比
	}

	// 2. Tasks 变更
	if len(before.Tasks) != len(after.Tasks) {
		changes = append(changes, ContextChange{
			Type: "tasks_count_changed",
			Desc: fmt.Sprintf("Tasks count: %d → %d", len(before.Tasks), len(after.Tasks)),
		})
	}
	if before.ActiveTaskID != after.ActiveTaskID {
		changes = append(changes, ContextChange{
			Type:   "active_task_changed",
			Desc:   fmt.Sprintf("Active task: %s → %s", before.ActiveTaskID, after.ActiveTaskID),
			Before: before.ActiveTaskID,
			After:  after.ActiveTaskID,
		})
	}

	// 3. PendingQuestions 变更
	if len(before.PendingQuestions) != len(after.PendingQuestions) {
		changes = append(changes, ContextChange{
			Type: "pending_questions_changed",
			Desc: fmt.Sprintf("Pending questions: %d → %d", len(before.PendingQuestions), len(after.PendingQuestions)),
		})
	}

	// 4. ContextMessages 变更
	if len(before.ContextMessages) != len(after.ContextMessages) {
		changes = append(changes, ContextChange{
			Type: "context_messages_changed",
			Desc: fmt.Sprintf("Context messages: %d → %d", len(before.ContextMessages), len(after.ContextMessages)),
		})
	}

	// 5. ConversationHistory 变更
	if len(before.ConversationHistory) != len(after.ConversationHistory) {
		changes = append(changes, ContextChange{
			Type: "conversation_turns_changed",
			Desc: fmt.Sprintf("Conversation turns: %d → %d", len(before.ConversationHistory), len(after.ConversationHistory)),
		})
	}

	return changes
}
