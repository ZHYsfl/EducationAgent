package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const protocolInstructions = `

[动作协议]
1. 思考使用 #{...}，例如 #{思考内容}。
2. 工具调用使用 @{type|k:v|k:v}，例如 @{kb_query|query:导数定义}。
3. 对用户可见的自然语言不要放在 #{...} 或 @{...} 里。
4. 工具结果上下文统一以 <tool>...</tool> 注入。
5. 若被打断，保留可恢复轨迹，后续由 </interrupted> 表示中断续写语义。
6. 当前支持工具: kb_query, web_search, update_requirements, require_confirm, ppt_init, ppt_mod, get_memory。
7. 遇到冲突问题时，基于用户原话直接通过 @{ppt_mod|raw_text:用户原话|user_distance:int} 反馈。`

// buildSystemPrompt assembles the full system prompt.
// includeQueue=true drains and injects pending tool results (used in startProcessing).
// includeQueue=false skips queue drain (used in think warmup).
func (p *Pipeline) buildSystemPrompt(includeQueue bool) string {
	var sb strings.Builder

	// Layer 1: base or requirements collection
	p.session.reqMu.RLock()
	req := p.session.Requirements.Clone()
	p.session.reqMu.RUnlock()
	if req != nil && (req.Status == "collecting" || req.Status == "ready") {
		sb.WriteString(req.BuildCollectionPrompt())
	} else {
		sb.WriteString(p.config.SystemPrompt)
	}

	// Layer 2: task list
	p.session.activeTaskMu.RLock()
	activeTask := p.session.ActiveTaskID
	tasks := make(map[string]string, len(p.session.OwnedTasks))
	for k, v := range p.session.OwnedTasks {
		tasks[k] = v
	}
	p.session.activeTaskMu.RUnlock()

	if len(tasks) > 0 {
		sb.WriteString("\n\n[任务列表]\n")
		ids := make([]string, 0, len(tasks))
		for id := range tasks {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			marker := ""
			if id == activeTask {
				marker = " (当前任务)"
			}
			fmt.Fprintf(&sb, "- task_id=%s, topic=%q%s\n", id, tasks[id], marker)
		}
		if len(tasks) > 1 {
			sb.WriteString("存在多任务时，请先确认用户当前指的是哪一个任务。\n")
		}
	}

	// Layer 3: pending conflict questions
	p.session.pendingQMu.RLock()
	questions := make(map[string]PendingQuestion, len(p.session.PendingQuestions))
	for k, v := range p.session.PendingQuestions {
		questions[k] = v
	}
	p.session.pendingQMu.RUnlock()

	if len(questions) > 0 {
		sb.WriteString("\n\n[待解决冲突问题]\n")
		ids := make([]string, 0, len(questions))
		for id := range questions {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, cid := range ids {
			pq := questions[cid]
			fmt.Fprintf(&sb, "- context_id=%s, task_id=%s\n  question=%s\n", cid, pq.TaskID, pq.QuestionText)
		}
		if len(questions) > 1 {
			sb.WriteString("多问题并存时，请在自然语言中明确任务或页面，系统会自动匹配对应冲突。\n")
		}
	}

	// Layer 4: tool results
	if includeQueue {
		if toolCtx := p.drainToolResults(); toolCtx != "" {
			sb.WriteString(toolCtx)
		}
	}

	// Layer 5: protocol
	sb.WriteString(protocolInstructions)
	return sb.String()
}

// drainToolResults drains pendingContexts + contextQueue and formats as <tool> blocks.
func (p *Pipeline) drainToolResults() string {
	p.pendingMu.Lock()
	msgs := append([]ContextMessage{}, p.pendingContexts...)
	p.pendingContexts = nil
	for {
		select {
		case msg := <-p.contextQueue:
			msgs = append(msgs, msg)
		default:
			p.pendingMu.Unlock()
			goto done
		}
	}
done:
	if len(msgs) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n\n[tool_context|工具结果]\n")
	for _, m := range msgs {
		content := strings.TrimSpace(m.Content)
		if content == "" {
			continue
		}
		eventType := strings.TrimSpace(m.EventType)
		if eventType == "" {
			eventType = "event"
		}
		fmt.Fprintf(&sb, "<tool>%s:%s</tool>\n", eventType, content)
	}
	return sb.String()
}

// EnqueueContext routes context messages into immediate handlers or queues.
func (p *Pipeline) EnqueueContext(msg ContextMessage) {
	if msg.EventType == "update_requirements" {
		p.applyRequirementsUpdate(msg.Content)
		return
	}

	if msg.EventType == "task_list_update" {
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
			log.Printf("[ctx] high priority queue full, dropping")
		}
	} else {
		select {
		case p.contextQueue <- msg:
		default:
			log.Printf("[ctx] context queue full, dropping")
		}
	}

	if p.session.CompareAndSetState(StateIdle, StateProcessing) {
		p.sessionCtxMu.RLock()
		sCtx := p.sessionCtx
		p.sessionCtxMu.RUnlock()
		if sCtx != nil && sCtx.Err() == nil {
			go p.startProcessing(sCtx, fmt.Sprintf("新工具结果（%s）", msg.EventType))
		} else {
			p.session.SetState(StateIdle)
		}
	}
}

// highPriorityListener handles conflict/system high-priority events.
func (p *Pipeline) highPriorityListener(ctx context.Context) {
	for {
		select {
		case msg := <-p.highPriorityQueue:
			p.handleHighPriorityMsg(ctx, msg)
		case <-ctx.Done():
			return
		}
	}
}

func (p *Pipeline) handleHighPriorityMsg(ctx context.Context, msg ContextMessage) {
	switch msg.EventType {
	case "conflict_question":
		p.session.SendJSON(WSMessage{
			Type:      "conflict_ask",
			TaskID:    msg.Metadata["task_id"],
			PageID:    msg.Metadata["page_id"],
			ContextID: msg.Metadata["context_id"],
			Question:  msg.Content,
		})
		p.speakText(ctx, msg.Content)

		baseTS := int64(0)
		if ts, err := strconv.ParseInt(msg.Metadata["base_timestamp"], 10, 64); err == nil {
			baseTS = ts
		}
		p.session.AddPendingQuestion(
			msg.Metadata["context_id"],
			msg.Metadata["task_id"],
			msg.Metadata["page_id"],
			baseTS,
			msg.Content,
		)

		if ctx.Err() != nil {
			retries := 0
			fmt.Sscanf(msg.Metadata["_retries"], "%d", &retries)
			retries++
			if retries > 2 {
				log.Printf("[high-priority] conflict_question demoted after %d retries", retries)
				p.pendingMu.Lock()
				p.pendingContexts = append(p.pendingContexts, msg)
				p.pendingMu.Unlock()
				return
			}
			if msg.Metadata == nil {
				msg.Metadata = make(map[string]string)
			}
			msg.Metadata["_retries"] = fmt.Sprintf("%d", retries)
			select {
			case p.highPriorityQueue <- msg:
			default:
				p.pendingMu.Lock()
				p.pendingContexts = append(p.pendingContexts, msg)
				p.pendingMu.Unlock()
			}
		}

	case "system_notify":
		p.speakText(ctx, msg.Content)
		if ctx.Err() != nil {
			log.Printf("[high-priority] system_notify interrupted, skipping")
		}

	default:
		p.pendingMu.Lock()
		p.pendingContexts = append(p.pendingContexts, msg)
		p.pendingMu.Unlock()
	}
}

func (p *Pipeline) speakText(ctx context.Context, text string) {
	p.session.SetState(StateSpeaking)
	ch := make(chan string, 1)
	ch <- text
	close(ch)
	p.ttsWorker(ctx, ch)
	p.session.SetState(StateIdle)
}

func (p *Pipeline) applyRequirementsUpdate(jsonData string) {
	var updates map[string]any
	if err := json.Unmarshal([]byte(jsonData), &updates); err != nil {
		log.Printf("[pipeline] requirements update parse error: %v", err)
		return
	}

	p.session.reqMu.Lock()
	req := p.session.Requirements
	if req == nil {
		req = NewTaskRequirements(p.session.SessionID, p.session.UserID)
		p.session.Requirements = req
	}

	setStr := func(dst *string, key string) {
		if v, ok := updates[key].(string); ok {
			v = strings.TrimSpace(v)
			if v != "" {
				*dst = v
			}
		}
	}
	setSlice := func(dst *[]string, key string) {
		if v, ok := updates[key]; ok {
			vals := parseStringSlice(v)
			if len(vals) > 0 {
				*dst = vals
			}
		}
	}

	setStr(&req.Topic, "topic")
	setStr(&req.Description, "description")
	setStr(&req.TargetAudience, "audience")
	setSlice(&req.KnowledgePoints, "knowledge_points")
	setSlice(&req.TeachingGoals, "teaching_goals")
	setStr(&req.TeachingLogic, "teaching_logic")
	setSlice(&req.KeyDifficulties, "key_difficulties")
	setStr(&req.Duration, "duration")
	setStr(&req.GlobalStyle, "global_style")
	setStr(&req.InteractionDesign, "interaction_design")
	setSlice(&req.OutputFormats, "output_formats")
	setStr(&req.AdditionalNotes, "additional_notes")

	switch v := updates["total_pages"].(type) {
	case float64:
		if v > 0 {
			req.TotalPages = int(v)
		}
	case int:
		if v > 0 {
			req.TotalPages = v
		}
	case string:
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
			req.TotalPages = n
		}
	}

	req.RefreshCollectedFields()
	if req.IsReady() {
		req.Status = "ready"
	} else {
		req.Status = "collecting"
	}
	req.UpdatedAt = time.Now().UnixMilli()
	snap := req.Clone()
	p.session.reqMu.Unlock()

	if snap.Status == "ready" {
		p.session.SendJSON(WSMessage{
			Type:         "requirements_summary",
			SummaryText:  buildRequirementsSummary(snap),
			Requirements: snap,
		})
	}
	p.session.SendJSON(WSMessage{
		Type:            "requirements_progress",
		Status:          snap.Status,
		CollectedFields: snap.CollectedFields,
		MissingFields:   snap.GetMissingFields(),
		Requirements:    snap,
	})
}

func buildRequirementsSummary(r *TaskRequirements) string {
	var sb strings.Builder
	if r.Topic != "" {
		fmt.Fprintf(&sb, "主题: %s\n", r.Topic)
	}
	if r.Description != "" {
		fmt.Fprintf(&sb, "描述: %s\n", r.Description)
	}
	if r.TargetAudience != "" {
		fmt.Fprintf(&sb, "受众: %s\n", r.TargetAudience)
	}
	if r.TotalPages > 0 {
		fmt.Fprintf(&sb, "页数: %d\n", r.TotalPages)
	}
	if r.Duration != "" {
		fmt.Fprintf(&sb, "时长: %s\n", r.Duration)
	}
	if r.GlobalStyle != "" {
		fmt.Fprintf(&sb, "风格: %s\n", r.GlobalStyle)
	}
	if len(r.KnowledgePoints) > 0 {
		fmt.Fprintf(&sb, "知识点: %s\n", strings.Join(r.KnowledgePoints, "、"))
	}
	if len(r.TeachingGoals) > 0 {
		fmt.Fprintf(&sb, "教学目标: %s\n", strings.Join(r.TeachingGoals, "、"))
	}
	if r.TeachingLogic != "" {
		fmt.Fprintf(&sb, "教学逻辑: %s\n", r.TeachingLogic)
	}
	if len(r.KeyDifficulties) > 0 {
		fmt.Fprintf(&sb, "重点难点: %s\n", strings.Join(r.KeyDifficulties, "、"))
	}
	if r.InteractionDesign != "" {
		fmt.Fprintf(&sb, "互动设计: %s\n", r.InteractionDesign)
	}
	if len(r.OutputFormats) > 0 {
		fmt.Fprintf(&sb, "输出格式: %s\n", strings.Join(r.OutputFormats, "、"))
	}
	return strings.TrimRight(sb.String(), "\n")
}


func parseStringSlice(v any) []string {
	switch t := v.(type) {
	case string:
		parts := strings.FieldsFunc(t, func(r rune) bool {
			return r == ',' || r == '，' || r == ';' || r == '；' || unicode.IsSpace(r)
		})
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				out = append(out, p)
			}
		}
		return out
	case []string:
		out := make([]string, 0, len(t))
		for _, p := range t {
			p = strings.TrimSpace(p)
			if p != "" {
				out = append(out, p)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s, ok := item.(string); ok {
				s = strings.TrimSpace(s)
				if s != "" {
					out = append(out, s)
				}
			}
		}
		return out
	default:
		return nil
	}
}
