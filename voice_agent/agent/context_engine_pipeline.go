package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"voiceagent/internal/protocol"
	"voiceagent/internal/types"
)

// ---------------------------------------------------------------------------
// Context Queue Ingress & Routing
// ---------------------------------------------------------------------------

// EnqueueContext adds a context message to the queue and triggers active push if idle.
func (p *Pipeline) EnqueueContext(msg types.ContextMessage) {
	// Handle requirements updates immediately
	if msg.MsgType == "requirements_updated" {
		p.handleRequirementsUpdate(msg.Content)
		return
	}

	// Handle task_list_update: register task and notify frontend
	if msg.MsgType == "task_list_update" {
		taskID := msg.Metadata["task_id"]
		topic := msg.Metadata["topic"]
		if taskID != "" {
			p.session.RegisterTask(taskID, topic)
			p.session.SetActiveTask(taskID)
			p.session.SendJSON(WSMessage{
				Type:         "task_list_update",
				ActiveTaskID: taskID,
				Tasks:        p.session.GetOwnedTasks(),
			})
		}
		return
	}

	if msg.Priority == "high" {
		select {
		case p.highPriorityQueue <- msg:
		default:
			log.Printf("[ctx] high priority queue full")
		}
	} else {
		select {
		case p.contextQueue <- msg:
		default:
			log.Printf("[ctx] context queue full")
		}
	}

	// If current session is idle, actively trigger one round to consume the update.
	if p.session.GetState() == StateIdle {
		p.sessionCtxMu.RLock()
		sCtx := p.sessionCtx
		p.sessionCtxMu.RUnlock()
		if sCtx != nil && sCtx.Err() == nil {
			if p.session.CompareAndSetState(StateIdle, StateProcessing) {
				go p.processContextUpdate(sCtx, msg)
			}
		}
	}
}

func (p *Pipeline) processContextUpdate(ctx context.Context, msg types.ContextMessage) {
	prompt := fmt.Sprintf("新任务结果（%s）: %s", msg.ActionType, msg.Content)
	p.startProcessing(ctx, prompt)
}

// IngestContextFromCallback is the single callback ingestion method used by HTTP callback.
func (p *Pipeline) IngestContextFromCallback(ctx context.Context, msg ContextMessage) {
	p.enqueueContextMessage(ctx, msg)
}

func (p *Pipeline) enqueueContextMessage(ctx context.Context, msg ContextMessage) {
	switch msg.Priority {
	case "high":
		select {
		case p.highPriorityQueue <- msg:
		case <-ctx.Done():
		}
	default:
		select {
		case p.contextQueue <- msg:
		case <-ctx.Done():
		}
	}
}

// ---------------------------------------------------------------------------
// Context Consumption Helpers
// ---------------------------------------------------------------------------

func (p *Pipeline) drainContextQueue() []ContextMessage {
	p.pendingMu.Lock()
	defer p.pendingMu.Unlock()

	msgs := []ContextMessage{}
	if len(p.pendingContexts) > 0 {
		msgs = append(msgs, p.pendingContexts...)
		p.pendingContexts = nil
	}

	for {
		select {
		case msg := <-p.contextQueue:
			msgs = append(msgs, msg)
		default:
			return msgs
		}
	}
}

func FormatContextForLLM(msgs []ContextMessage) string {
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

func (p *Pipeline) highPriorityListener(ctx context.Context) {
	for {
		select {
		case msg := <-p.highPriorityQueue:
			switch msg.MsgType {
			case "conflict_question", "system_notify":
				if msg.MsgType == "conflict_question" {
					p.session.SendJSON(WSMessage{
						Type:      "conflict_ask",
						TaskID:    msg.Metadata["task_id"],
						PageID:    msg.Metadata["page_id"],
						ContextID: msg.Metadata["context_id"],
						Question:  msg.Content,
					})
				}

				p.session.SetState(StateSpeaking)
				sentenceCh := make(chan string, 1)
				sentenceCh <- msg.Content
				close(sentenceCh)
				p.ttsWorker(ctx, sentenceCh)
				p.session.SetState(StateIdle)

				if msg.MsgType == "conflict_question" {
					pageID := msg.Metadata["page_id"]
					baseTS := int64(0)
					if tsStr := msg.Metadata["base_timestamp"]; tsStr != "" {
						if ts, err := strconv.ParseInt(tsStr, 10, 64); err == nil {
							baseTS = ts
						}
					}
					p.session.AddPendingQuestion(msg.Metadata["context_id"], msg.Metadata["task_id"], pageID, baseTS, msg.Content)
				}

				if ctx.Err() != nil {
					// system_notify interrupted: skip without retry
					if msg.MsgType == "system_notify" {
						log.Printf("[high-priority] system_notify interrupted, skipping")
						continue
					}

					// conflict_question retry mechanism
					retries := 0
					if r, ok := msg.Metadata["_retries"]; ok {
						fmt.Sscanf(r, "%d", &retries)
					}
					retries++
					if retries > 2 {
						log.Printf("[high-priority] conflict_question interrupted %d times, demoting to context",
							retries)
						p.pendingMu.Lock()
						p.pendingContexts = append(p.pendingContexts, msg)
						p.pendingMu.Unlock()
						continue
					}

					log.Printf("[high-priority] conflict_question interrupted, will retry (retry=%d)", retries)
					if msg.Metadata == nil {
						msg.Metadata = make(map[string]string)
					}
					msg.Metadata["_retries"] = fmt.Sprintf("%d", retries)

					// Requeue with non-blocking send
					select {
					case p.highPriorityQueue <- msg:
					default:
						p.pendingMu.Lock()
						p.pendingContexts = append(p.pendingContexts, msg)
						p.pendingMu.Unlock()
					}

					// Exit if context is cancelled after requeue
					select {
					case <-ctx.Done():
						return
					default:
						continue
					}
				}
			default:
				p.pendingMu.Lock()
				p.pendingContexts = append(p.pendingContexts, msg)
				p.pendingMu.Unlock()
			}
		case <-ctx.Done():
			return
		}
	}
}

// ---------------------------------------------------------------------------
// Requirements & Post-processing
// ---------------------------------------------------------------------------

func (p *Pipeline) handleRequirementsUpdate(jsonData string) {
	var updates map[string]interface{}
	if err := json.Unmarshal([]byte(jsonData), &updates); err != nil {
		log.Printf("[pipeline] failed to parse requirements update: %v", err)
		return
	}

	p.session.reqMu.Lock()
	req := p.session.Requirements
	if req == nil {
		req = NewTaskRequirements(p.session.SessionID, p.session.UserID)
		p.session.Requirements = req
	}

	if v, ok := updates["topic"].(string); ok && v != "" {
		req.Topic = v
	}
	if v, ok := updates["subject"].(string); ok && v != "" {
		req.Subject = v
	}
	if v, ok := updates["audience"].(string); ok && v != "" {
		req.TargetAudience = v
	}
	if v, ok := updates["total_pages"].(float64); ok && v > 0 {
		req.TotalPages = int(v)
	}
	if v, ok := updates["knowledge_points"].(string); ok && v != "" {
		req.KnowledgePoints = strings.Split(v, ",")
	}
	if v, ok := updates["teaching_goals"].(string); ok && v != "" {
		req.TeachingGoals = strings.Split(v, ",")
	}
	if v, ok := updates["teaching_logic"].(string); ok && v != "" {
		req.TeachingLogic = v
	}
	if v, ok := updates["key_difficulties"].(string); ok && v != "" {
		req.KeyDifficulties = strings.Split(v, ",")
	}
	if v, ok := updates["duration"].(string); ok && v != "" {
		req.Duration = v
	}
	if v, ok := updates["global_style"].(string); ok && v != "" {
		req.GlobalStyle = v
	}
	if v, ok := updates["interaction_design"].(string); ok && v != "" {
		req.InteractionDesign = v
	}
	if v, ok := updates["output_formats"].(string); ok && v != "" {
		req.OutputFormats = strings.Split(v, ",")
	}
	if v, ok := updates["additional_notes"].(string); ok && v != "" {
		req.AdditionalNotes = v
	}

	req.RefreshCollectedFields()
	if req.IsReadyForConfirm() {
		req.Status = "ready"
	} else {
		req.Status = "collecting"
	}
	req.UpdatedAt = time.Now().UnixMilli()
	reqSnapshot := CloneTaskRequirements(req)
	p.session.reqMu.Unlock()

	if reqSnapshot.Status == "ready" {
		p.session.SendJSON(WSMessage{
			Type:         "requirements_summary",
			SummaryText:  buildSummaryText(reqSnapshot),
			Requirements: reqSnapshot,
		})
	}

	p.session.SendJSON(WSMessage{
		Type:            "requirements_progress",
		Status:          reqSnapshot.Status,
		CollectedFields: reqSnapshot.CollectedFields,
		MissingFields:   reqSnapshot.GetMissingFields(),
		Requirements:    reqSnapshot,
	})

	log.Printf("[pipeline] requirements updated: %v", updates)
}

func buildSummaryText(r *TaskRequirements) string {
	if r == nil {
		return ""
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "【课程主题】%s\n", r.Topic)
	if r.Subject != "" {
		fmt.Fprintf(&sb, "【学科】%s\n", r.Subject)
	}
	fmt.Fprintf(&sb, "【目标受众】%s\n", r.TargetAudience)
	if len(r.TeachingGoals) > 0 {
		fmt.Fprintf(&sb, "【教学目标】%s\n", strings.Join(r.TeachingGoals, "，"))
	}
	if len(r.KnowledgePoints) > 0 {
		fmt.Fprintf(&sb, "【核心知识点】%s\n", strings.Join(r.KnowledgePoints, "、"))
	}
	if r.TeachingLogic != "" {
		fmt.Fprintf(&sb, "【讲授逻辑】%s\n", r.TeachingLogic)
	}
	if len(r.KeyDifficulties) > 0 {
		fmt.Fprintf(&sb, "【重点难点】%s\n", strings.Join(r.KeyDifficulties, "、"))
	}
	if r.Duration != "" {
		fmt.Fprintf(&sb, "【课程时长】%s\n", r.Duration)
	}
	if r.TotalPages > 0 {
		fmt.Fprintf(&sb, "【页数】%d\n", r.TotalPages)
	}
	if r.GlobalStyle != "" {
		fmt.Fprintf(&sb, "【整体风格】%s\n", r.GlobalStyle)
	}
	if r.InteractionDesign != "" {
		fmt.Fprintf(&sb, "【互动设计】%s\n", r.InteractionDesign)
	}
	if len(r.OutputFormats) > 0 {
		fmt.Fprintf(&sb, "【输出格式】%s\n", strings.Join(r.OutputFormats, "、"))
	}
	if r.AdditionalNotes != "" {
		fmt.Fprintf(&sb, "【其他要求】%s\n", r.AdditionalNotes)
	}
	if len(r.ReferenceFiles) > 0 {
		fmt.Fprintf(&sb, "【参考文件】%d 个\n", len(r.ReferenceFiles))
	}
	return strings.TrimRight(sb.String(), "\n")
}

func (p *Pipeline) tryResolveConflict(_ context.Context, userText string, actions []protocol.Action) bool {
	if p.clients == nil {
		return false
	}
	p.session.pendingQMu.RLock()
	pendingCount := len(p.session.PendingQuestions)
	if pendingCount == 0 {
		p.session.pendingQMu.RUnlock()
		return false
	}

	var contextIDs []string
	if pendingCount == 1 {
		for cid := range p.session.PendingQuestions {
			contextIDs = append(contextIDs, cid)
		}
	} else {
		for _, action := range actions {
			if action.Type == "resolve_conflict" {
				contextID := action.Params["context_id"]
				if contextID != "" && p.session.PendingQuestions[contextID].TaskID != "" {
					contextIDs = append(contextIDs, contextID)
				}
			}
		}
		if len(contextIDs) == 0 {
			for cid := range p.session.PendingQuestions {
				contextIDs = append(contextIDs, cid)
				break
			}
		}
	}
	p.session.pendingQMu.RUnlock()

	for _, contextID := range contextIDs {
		if resolved, ok := p.session.ResolvePendingQuestion(contextID); ok {
			log.Printf("[pipeline] resolving conflict context_id=%s task_id=%s", contextID, resolved.TaskID)
			pq := resolved
			go func() {
				if err := p.clients.SendFeedback(context.Background(), PPTFeedbackRequest{
					TaskID:        pq.TaskID,
					BaseTimestamp: pq.BaseTimestamp,
					ViewingPageID: pq.PageID,
					RawText:       userText,
					Intents:       nil,
				}); err != nil {
					log.Printf("[pipeline] SendFeedback resolve_conflict failed: %v", err)
				}
			}()
		}
	}
	return len(contextIDs) > 0
}

func (p *Pipeline) maybeCompressHistory() {
	if p.clients == nil {
		return
	}
	if p.history.TotalChars() <= 8000 {
		return
	}
	messages := p.history.Messages()
	cutoff := len(messages) / 3
	if cutoff == 0 {
		return
	}
	turns := make([]ConversationTurn, cutoff)
	for i, m := range messages[:cutoff] {
		turns[i] = ConversationTurn{Role: m.Role, Content: m.Content}
	}
	userID := p.session.UserID
	sessionID := p.session.SessionID
	go func() {
		if err := p.clients.PushContext(context.Background(), PushContextRequest{
			UserID:    userID,
			SessionID: sessionID,
			Messages:  turns,
		}); err != nil {
			log.Printf("[pipeline] PushContext compress failed: %v", err)
			return
		}
		p.history.DeleteFront(cutoff)
		p.session.memoryMu.Lock()
		p.session.lastMemoryExtractIndex = 0
		p.session.memoryMu.Unlock()
	}()
}

func (p *Pipeline) pushRemainingContext() {
	if p.clients == nil {
		return
	}
	p.session.memoryMu.Lock()
	messages := p.history.Messages()
	startIdx := p.session.lastMemoryExtractIndex
	endIdx := len(messages)
	p.session.memoryMu.Unlock()

	if startIdx >= endIdx {
		return
	}
	turns := make([]ConversationTurn, 0, endIdx-startIdx)
	for _, m := range messages[startIdx:endIdx] {
		turns = append(turns, ConversationTurn{Role: m.Role, Content: m.Content})
	}
	if err := p.clients.PushContext(context.Background(), PushContextRequest{
		UserID:    p.session.UserID,
		SessionID: p.session.SessionID,
		Messages:  turns,
	}); err != nil {
		log.Printf("[pipeline] PushContext session-end failed: %v", err)
	}
}
