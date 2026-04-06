package agent

import (
	"encoding/json"
	"fmt"
	"strings"
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

	// 用户画像（如果有）
	UserProfile *UserProfile `json:"user_profile,omitempty"`
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

// ContextManager 统一的上下文管理器（观测层，不改变现有逻辑）
type ContextManager struct {
	session *Session
	mu      sync.RWMutex
}

// NewContextManager 创建上下文管理器
func NewContextManager(session *Session) *ContextManager {
	return &ContextManager{
		session: session,
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

	// 6. UserProfile (Session 中没有 profile 字段，暂时留空)
	// if cm.session.profile != nil {
	// 	snapshot.UserProfile = cm.session.profile
	// }

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

// Visualize 可视化上下文（人类可读格式）
func (cm *ContextManager) Visualize() string {
	snapshot := cm.Export()
	var sb strings.Builder

	sb.WriteString("╔════════════════════════════════════════════════════════════════╗\n")
	sb.WriteString("║              Context Snapshot Visualization                   ║\n")
	sb.WriteString("╚════════════════════════════════════════════════════════════════╝\n")
	sb.WriteString(fmt.Sprintf("\n📅 Timestamp: %s\n", time.UnixMilli(snapshot.Timestamp).Format("2006-01-02 15:04:05")))
	sb.WriteString(fmt.Sprintf("👤 Session: %s | User: %s\n", snapshot.SessionID, snapshot.UserID))

	// Requirements
	if snapshot.Requirements != nil {
		sb.WriteString("\n┌─ 📋 Requirements ─────────────────────────────────────────────┐\n")
		sb.WriteString(fmt.Sprintf("│ Status: %s\n", snapshot.Requirements.Status))
		sb.WriteString(fmt.Sprintf("│ Topic: %s\n", truncateForDisplay(snapshot.Requirements.Topic, 50)))
		if snapshot.Requirements.Subject != "" {
			sb.WriteString(fmt.Sprintf("│ Subject: %s\n", snapshot.Requirements.Subject))
		}
		if snapshot.Requirements.TargetAudience != "" {
			sb.WriteString(fmt.Sprintf("│ Audience: %s\n", snapshot.Requirements.TargetAudience))
		}
		if snapshot.Requirements.TotalPages > 0 {
			sb.WriteString(fmt.Sprintf("│ Total Pages: %d\n", snapshot.Requirements.TotalPages))
		}
		if snapshot.Requirements.GlobalStyle != "" {
			sb.WriteString(fmt.Sprintf("│ Style: %s\n", snapshot.Requirements.GlobalStyle))
		}
		if len(snapshot.Requirements.KnowledgePoints) > 0 {
			sb.WriteString(fmt.Sprintf("│ Knowledge Points: %d items\n", len(snapshot.Requirements.KnowledgePoints)))
		}
		sb.WriteString("└───────────────────────────────────────────────────────────────┘\n")
	}

	// Tasks
	if len(snapshot.Tasks) > 0 {
		sb.WriteString(fmt.Sprintf("\n┌─ 📚 Tasks (Count: %d, Active: %s) ─────────────────────────┐\n",
			len(snapshot.Tasks), truncateForDisplay(snapshot.ActiveTaskID, 20)))
		for i, t := range snapshot.Tasks {
			marker := "  "
			if t.TaskID == snapshot.ActiveTaskID {
				marker = "▶ "
			}
			sb.WriteString(fmt.Sprintf("│ %s%d. [%s] %s\n", marker, i+1,
				truncateForDisplay(t.TaskID, 15), truncateForDisplay(t.Topic, 30)))
			sb.WriteString(fmt.Sprintf("│    Status: %s | Pages: %d | Viewing: %s\n",
				t.Status, t.TotalPages, truncateForDisplay(t.ViewingPageID, 15)))
		}
		sb.WriteString("└───────────────────────────────────────────────────────────────┘\n")
	}

	// Pending Questions
	if len(snapshot.PendingQuestions) > 0 {
		sb.WriteString(fmt.Sprintf("\n┌─ ❓ Pending Questions (Count: %d) ───────────────────────────┐\n",
			len(snapshot.PendingQuestions)))
		for i, q := range snapshot.PendingQuestions {
			sb.WriteString(fmt.Sprintf("│ %d. [%s] Task: %s | Page: %s\n", i+1,
				truncateForDisplay(q.ContextID, 12),
				truncateForDisplay(q.TaskID, 15),
				truncateForDisplay(q.PageID, 10)))
			sb.WriteString(fmt.Sprintf("│    Q: %s\n", truncateForDisplay(q.Question, 50)))
		}
		sb.WriteString("└───────────────────────────────────────────────────────────────┘\n")
	}

	// Context Messages
	if len(snapshot.ContextMessages) > 0 {
		sb.WriteString(fmt.Sprintf("\n┌─ 💬 Context Messages (Count: %d) ────────────────────────────┐\n",
			len(snapshot.ContextMessages)))
		for i, m := range snapshot.ContextMessages {
			priorityIcon := "○"
			if m.Priority == "high" {
				priorityIcon = "●"
			}
			sb.WriteString(fmt.Sprintf("│ %d. %s [%s | %s]\n", i+1, priorityIcon,
				m.ActionType, m.MsgType))
			sb.WriteString(fmt.Sprintf("│    %s\n", truncateForDisplay(m.Content, 55)))
		}
		sb.WriteString("└───────────────────────────────────────────────────────────────┘\n")
	}

	// Conversation History
	if len(snapshot.ConversationHistory) > 0 {
		sb.WriteString(fmt.Sprintf("\n┌─ 💭 Conversation History (Turns: %d) ────────────────────────┐\n",
			len(snapshot.ConversationHistory)))
		for i, turn := range snapshot.ConversationHistory {
			icon := "👤"
			if turn.Role == "assistant" {
				icon = "🤖"
			}
			sb.WriteString(fmt.Sprintf("│ %d. %s %s: %s\n", i+1, icon, turn.Role,
				truncateForDisplay(turn.Content, 50)))
		}
		sb.WriteString("└───────────────────────────────────────────────────────────────┘\n")
	}

	// User Profile
	if snapshot.UserProfile != nil {
		sb.WriteString("\n┌─ 👤 User Profile ─────────────────────────────────────────────┐\n")
		if snapshot.UserProfile.DisplayName != "" {
			sb.WriteString(fmt.Sprintf("│ Name: %s\n", snapshot.UserProfile.DisplayName))
		}
		if snapshot.UserProfile.Subject != "" {
			sb.WriteString(fmt.Sprintf("│ Subject: %s\n", snapshot.UserProfile.Subject))
		}
		if snapshot.UserProfile.School != "" {
			sb.WriteString(fmt.Sprintf("│ School: %s\n", snapshot.UserProfile.School))
		}
		if snapshot.UserProfile.TeachingStyle != "" {
			sb.WriteString(fmt.Sprintf("│ Teaching Style: %s\n", snapshot.UserProfile.TeachingStyle))
		}
		sb.WriteString("└───────────────────────────────────────────────────────────────┘\n")
	}

	sb.WriteString("\n")
	return sb.String()
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

// BuildPrompt is the ONLY entry point for system prompt construction
func (cm *ContextManager) BuildPrompt(baseSystemPrompt string, includeContextQueue bool, pendingContexts []ContextMessage, contextQueue chan ContextMessage, pendingMu *sync.Mutex) string {
	var sb strings.Builder

	// Layer 1: Base or Requirements mode
	sb.WriteString(cm.buildLayer1BasePrompt(baseSystemPrompt))

	// Layer 2: Task list
	if taskCtx := cm.buildLayer2TaskList(); taskCtx != "" {
		sb.WriteString(taskCtx)
	}

	// Layer 3: Pending questions
	if questionsCtx := cm.buildLayer3PendingQuestions(); questionsCtx != "" {
		sb.WriteString(questionsCtx)
	}

	// Layer 4: Context messages (RAG/search/PPT)
	if includeContextQueue {
		if msgCtx := cm.buildLayer4ContextMessages(pendingContexts, contextQueue, pendingMu); msgCtx != "" {
			sb.WriteString(msgCtx)
		}
	}

	// Layer 5: Protocol instructions
	sb.WriteString(protocolInstructions)

	return sb.String()
}

// buildLayer1BasePrompt builds the base system prompt or requirements mode override
func (cm *ContextManager) buildLayer1BasePrompt(baseSystemPrompt string) string {
	cm.session.reqMu.RLock()
	reqSnapshot := CloneTaskRequirements(cm.session.Requirements)
	cm.session.reqMu.RUnlock()

	if reqSnapshot != nil && (reqSnapshot.Status == "collecting" || reqSnapshot.Status == "ready") {
		return reqSnapshot.BuildRequirementsSystemPrompt(nil)
	}

	return baseSystemPrompt
}

// buildLayer2TaskList builds the task list context (exported for testing)
func (cm *ContextManager) buildLayer2TaskList() string {
	cm.session.activeTaskMu.RLock()
	defer cm.session.activeTaskMu.RUnlock()

	if len(cm.session.OwnedTasks) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n\n[系统提示 - 当前用户的 PPT 任务列表]\n")
	for tid, topic := range cm.session.OwnedTasks {
		marker := ""
		if tid == cm.session.ActiveTaskID {
			marker = " (当前活跃)"
		}
		sb.WriteString(fmt.Sprintf("- task_id=%s, 主题=\"%s\"%s\n", tid, topic, marker))
	}
	if len(cm.session.OwnedTasks) > 1 {
		sb.WriteString("\n用户可能用简称、缩写、别名来指代某个任务（例如用\"高数\"指\"高等数学\"）。\n")
		sb.WriteString("请根据语义判断用户说的是哪个任务。如果确实无法判断，主动追问用户，绝不要猜。\n")
		sb.WriteString("默认操作当前活跃的任务，除非用户明确提到了其他任务。\n")
	}
	return sb.String()
}

// buildLayer3PendingQuestions builds the pending questions context (exported for testing)
func (cm *ContextManager) buildLayer3PendingQuestions() string {
	cm.session.pendingQMu.RLock()
	defer cm.session.pendingQMu.RUnlock()

	if len(cm.session.PendingQuestions) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n\n[系统提示 - 待回答的冲突问题]\n")
	sb.WriteString("以下是 PPT Agent 提出的需要用户确认的问题，请判断用户是否在回答这些问题：\n")
	for cid, pq := range cm.session.PendingQuestions {
		sb.WriteString(fmt.Sprintf("- context_id=%s, task_id=%s\n  问题: %s\n", cid, pq.TaskID, pq.QuestionText))
	}
	if len(cm.session.PendingQuestions) > 1 {
		sb.WriteString("\n有多个待确认问题，请使用动作标记指定：")
		sb.WriteString("@{resolve_conflict|context_id:xxx}\n")
	}
	return sb.String()
}

// buildLayer4ContextMessages builds the context messages from queue
func (cm *ContextManager) buildLayer4ContextMessages(pendingContexts []ContextMessage, contextQueue chan ContextMessage, pendingMu *sync.Mutex) string {
	if pendingMu == nil || contextQueue == nil {
		return ""
	}

	// Drain context queue
	pendingMu.Lock()
	var msgs []ContextMessage
	if len(pendingContexts) > 0 {
		msgs = append(msgs, pendingContexts...)
	}

	for {
		select {
		case msg := <-contextQueue:
			msgs = append(msgs, msg)
		default:
			goto done
		}
	}
done:
	pendingMu.Unlock()

	// Format messages
	if len(msgs) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n\n[系统补充信息 - 以下是后台检索到的相关资料，供回答参考]\n")
	for _, m := range msgs {
		sb.WriteString(fmt.Sprintf("\n--- 操作: %s | 类型: %s ---\n%s\n", m.ActionType, m.MsgType, m.Content))
	}
	return sb.String()
}

// truncateForDisplay 截断字符串用于显示
func truncateForDisplay(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
