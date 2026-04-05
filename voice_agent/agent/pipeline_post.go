package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"voiceagent/internal/protocol"
)

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
		fmt.Fprintf(&sb, "【教学目标】%s\n", strings.Join(r.TeachingGoals, "；"))
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
		// 从 actions 中收集所有 resolve_conflict
		for _, action := range actions {
			if action.Type == "resolve_conflict" {
				contextID := action.Params["context_id"]
				if contextID != "" && p.session.PendingQuestions[contextID].TaskID != "" {
					contextIDs = append(contextIDs, contextID)
				}
			}
		}
		// 如果没找到，使用第一个
		if len(contextIDs) == 0 {
			for cid := range p.session.PendingQuestions {
				contextIDs = append(contextIDs, cid)
				break
			}
		}
	}
	p.session.pendingQMu.RUnlock()

	// 处理所有标记的冲突
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

func (p *Pipeline) asyncExtractMemory(userText, assistantText string) {
	if p.clients == nil || (userText == "" && assistantText == "") {
		return
	}

	p.session.memoryMu.Lock()
	messages := p.history.Messages()
	startIdx := p.session.lastMemoryExtractIndex
	endIdx := len(messages)

	if startIdx >= endIdx {
		p.session.memoryMu.Unlock()
		return
	}

	// 只提取新增的对话
	newMessages := messages[startIdx:endIdx]
	turns := make([]ConversationTurn, 0, len(newMessages))
	for _, msg := range newMessages {
		turns = append(turns, ConversationTurn{Role: msg.Role, Content: msg.Content})
	}

	p.session.lastMemoryExtractIndex = endIdx
	p.session.memoryMu.Unlock()

	sessionID := p.session.SessionID
	userID := p.session.UserID
	go func() {
		if _, err := p.clients.ExtractMemory(context.Background(), MemoryExtractRequest{
			UserID:    userID,
			SessionID: sessionID,
			Messages:  turns,
		}); err != nil {
			log.Printf("[pipeline] ExtractMemory failed: %v", err)
		}
	}()
}

func (p *Pipeline) buildTaskListContext() string {
	p.session.activeTaskMu.RLock()
	defer p.session.activeTaskMu.RUnlock()
	if len(p.session.OwnedTasks) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n\n[系统提示 - 当前用户的 PPT 任务列表]\n")
	for tid, topic := range p.session.OwnedTasks {
		marker := ""
		if tid == p.session.ActiveTaskID {
			marker = " (当前活跃)"
		}
		sb.WriteString(fmt.Sprintf("- task_id=%s, 主题=\"%s\"%s\n", tid, topic, marker))
	}
	if len(p.session.OwnedTasks) > 1 {
		sb.WriteString("\n用户可能用简称、缩写、别名来指代某个任务（例如用\"高数\"指\"高等数学\"）。\n")
		sb.WriteString("请根据语义判断用户说的是哪个任务。如果确实无法判断，主动追问用户，绝不要猜。\n")
		sb.WriteString("默认操作当前活跃的任务，除非用户明确提到了其他任务。\n")
	}
	return sb.String()
}

func (p *Pipeline) buildPendingQuestionsContext() string {
	p.session.pendingQMu.RLock()
	defer p.session.pendingQMu.RUnlock()
	if len(p.session.PendingQuestions) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n\n[系统提示 - 待回答的冲突问题]\n")
	sb.WriteString("以下是 PPT Agent 提出的需要用户确认的问题，请判断用户是否在回答这些问题：\n")
	for cid, pq := range p.session.PendingQuestions {
		sb.WriteString(fmt.Sprintf("- context_id=%s, task_id=%s\n  问题: %s\n", cid, pq.TaskID, pq.QuestionText))
	}
	if len(p.session.PendingQuestions) > 1 {
		sb.WriteString("\n有多个待确认问题，请使用动作标记指定：")
		sb.WriteString("@{resolve_conflict|context_id:xxx}\n")
	}
	return sb.String()
}
