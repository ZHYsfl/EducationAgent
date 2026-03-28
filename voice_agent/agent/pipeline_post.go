package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"
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

	req.RefreshCollectedFields()
	req.UpdatedAt = time.Now().UnixMilli()
	reqSnapshot := CloneTaskRequirements(req)
	p.session.reqMu.Unlock()

	p.session.SendJSON(WSMessage{
		Type:            "requirements_progress",
		Status:          req.Status,
		CollectedFields: req.CollectedFields,
		MissingFields:   req.GetMissingFields(),
		Requirements:    reqSnapshot,
	})

	log.Printf("[pipeline] requirements updated: %v", updates)
}

func (p *Pipeline) tryResolveConflict(_ context.Context, userText, llmResponse string) bool {
	if p.clients == nil {
		return false
	}
	p.session.pendingQMu.RLock()
	pendingCount := len(p.session.PendingQuestions)
	if pendingCount == 0 {
		p.session.pendingQMu.RUnlock()
		return false
	}

	var contextID, taskID string
	if pendingCount == 1 {
		for cid, tid := range p.session.PendingQuestions {
			contextID = cid
			taskID = tid
		}
	} else {
		contextID = p.extractContextIDFromResponse(llmResponse)
		if contextID != "" {
			taskID = p.session.PendingQuestions[contextID]
		}
		if contextID == "" || taskID == "" {
			for cid, tid := range p.session.PendingQuestions {
				contextID = cid
				taskID = tid
				break
			}
		}
	}
	p.session.pendingQMu.RUnlock()

	if _, ok := p.session.ResolvePendingQuestion(contextID); !ok {
		return false
	}
	log.Printf("[pipeline] resolving conflict context_id=%s task_id=%s", contextID, taskID)

	viewingPageID := p.session.GetViewingPageID()
	baseTS := p.session.GetLastVADTimestamp()
	go func() {
		if err := p.clients.SendFeedback(context.Background(), PPTFeedbackRequest{
			TaskID:        taskID,
			BaseTimestamp: baseTS,
			ViewingPageID: viewingPageID,
			RawText:       userText,
			Intents:       nil, // PPT Agent 负责解析
		}); err != nil {
			log.Printf("[pipeline] SendFeedback resolve_conflict failed: %v", err)
		}
	}()
	return true
}

func (p *Pipeline) extractContextIDFromResponse(text string) string {
	marker := "[RESOLVE_CONFLICT:"
	idx := strings.Index(text, marker)
	if idx < 0 {
		return ""
	}
	rest := text[idx+len(marker):]
	end := strings.Index(rest, "]")
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(rest[:end])
}

func (p *Pipeline) asyncExtractMemory(userText, assistantText string) {
	if p.clients == nil || (userText == "" && assistantText == "") {
		return
	}
	sessionID := p.session.SessionID
	userID := p.session.UserID
	turns := make([]ConversationTurn, 0, 2)
	if userText != "" {
		turns = append(turns, ConversationTurn{Role: "user", Content: userText})
	}
	if assistantText != "" {
		turns = append(turns, ConversationTurn{Role: "assistant", Content: assistantText})
	}
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
	for cid, tid := range p.session.PendingQuestions {
		sb.WriteString(fmt.Sprintf("- context_id=%s, task_id=%s\n", cid, tid))
	}
	if len(p.session.PendingQuestions) > 1 {
		sb.WriteString("\n有多个待确认问题，请在回复末尾标注你判断用户回答的是哪个问题：")
		sb.WriteString("[RESOLVE_CONFLICT:context_id值]\n")
	}
	return sb.String()
}
