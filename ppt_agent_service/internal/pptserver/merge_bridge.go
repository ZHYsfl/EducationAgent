package pptserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"educationagent/ppt_agent_service_go/internal/merge"
	"educationagent/ppt_agent_service_go/internal/slideutil"
	"educationagent/ppt_agent_service_go/internal/task"
	"educationagent/ppt_agent_service_go/internal/toolllm"
)

type voicePPTMessage struct {
	TaskID    string `json:"task_id"`
	PageID    string `json:"page_id"`
	Priority  string `json:"priority"`
	ContextID string `json:"context_id"`
	TTSText   string `json:"tts_text"`
	MsgType   string `json:"msg_type"`
}

func (s *Server) mergeLock(taskID, bucket string) *sync.Mutex {
	key := taskID + "\x00" + bucket
	v, _ := s.mergeBucketMu.LoadOrStore(key, &sync.Mutex{})
	return v.(*sync.Mutex)
}

func (s *Server) isDeckBusy(taskID string) bool {
	_, ok := s.deckRunning.Load(taskID)
	return ok
}

func (s *Server) newContextID() string {
	return "ctx_" + uuid.New().String()
}

func (s *Server) cancelSuspendWatcher(taskID, pageID string) {
	key := taskID + "\x00" + pageID
	if v, ok := s.suspendWatchers.LoadAndDelete(key); ok {
		if fn, ok := v.(context.CancelFunc); ok {
			fn()
		}
	}
}

func suspendReaskSec() int {
	n, _ := strconv.Atoi(strings.TrimSpace(os.Getenv("PPT_SUSPEND_REASK_SEC")))
	if n <= 0 {
		n = 45
	}
	return n
}

func suspendAutoResolveSec() int {
	n, _ := strconv.Atoi(strings.TrimSpace(os.Getenv("PPT_SUSPEND_AUTORESOLVE_SEC")))
	if n <= 0 {
		n = 180
	}
	return n
}

func (s *Server) scheduleSuspendWatch(taskID, pageID string) {
	key := taskID + "\x00" + pageID
	if v, ok := s.suspendWatchers.Load(key); ok {
		if fn, ok := v.(context.CancelFunc); ok {
			fn()
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.suspendWatchers.Store(key, context.CancelFunc(cancel))
	reask := time.Duration(suspendReaskSec()) * time.Second
	total := time.Duration(suspendAutoResolveSec()) * time.Second
	go func() {
		defer s.suspendWatchers.Delete(key)
		timer := time.NewTimer(reask)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		t := s.getTask(context.Background(), taskID)
		if t == nil || t.SuspendedPages[pageID] == nil {
			return
		}
		sp := t.SuspendedPages[pageID]
		sp.LastAskedAt = task.UTCMS()
		sp.AskCount++
		s.persist(context.Background(), t)
		s.voiceConflictQuestion(taskID, pageID, sp.ContextID,
			coalesce(sp.QuestionForUser, "仍在等待您对上一问题的确认，请回答以便继续修改课件。"))

		remain := total - reask
		if remain < 0 {
			remain = 0
		}
		timer2 := time.NewTimer(remain)
		defer timer2.Stop()
		select {
		case <-ctx.Done():
			return
		case <-timer2.C:
		}
		s.autoResolveSuspension(context.Background(), taskID, pageID)
	}()
}

func coalesce(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

func (s *Server) autoResolveSuspension(ctx context.Context, taskID, pageID string) {
	t := s.getTask(ctx, taskID)
	if t == nil {
		return
	}
	sp, ok := t.SuspendedPages[pageID]
	if !ok {
		return
	}
	delete(t.SuspendedPages, pageID)
	s.cancelSuspendWatcher(taskID, pageID)
	if sp != nil {
		delete(t.OpenConflictContexts, sp.ContextID)
	}
	if p := t.Pages[pageID]; p != nil {
		p.Status = "completed"
	}
	note := fmt.Sprintf("\n\n[系统自决-悬挂超时 page=%s context_id=%s]\n采用系统自动优化方案。原因摘要：%s\n",
		pageID, sp.ContextID, sp.Reason)
	for _, pb := range sp.PendingFeedbacks {
		b, _ := json.Marshal(pb)
		note += "[pending_feedback_while_suspended] " + string(b) + "\n"
	}
	busy := s.isDeckBusy(taskID)
	if busy {
		s.Store.AppendPendingFeedback(taskID, []string{note})
	} else {
		t.Description += note
	}
	t.LastUpdate = task.UTCMS()
	if !busy && t.Pages[pageID] != nil {
		html := slideutil.ExtractHTMLFromPy(t.Pages[pageID].PyCode)
		if nh, err := s.editSlideHTML(ctx, t.Topic, html, note, "system_auto_resolve"); err == nil {
			p := t.Pages[pageID]
			p.PyCode = slideutil.WrapSlideHTML(nh, pageID, p.SlideIndex)
			p.Version++
			p.UpdatedAt = task.UTCMS()
		}
	} else if !busy {
		s.startBackgroundRegen(taskID)
	}
	if !busy && t.Pages[pageID] != nil {
		p := t.Pages[pageID]
		inner := slideutil.ExtractHTMLFromPy(p.PyCode)
		_, _ = s.writeSlidePreview(ctx, taskID, p.SlideIndex, inner, false)
	}
	_ = s.saveCanvasFromTask(ctx, t, "")
	s.persist(ctx, t)
}

func (s *Server) voiceEndpointCandidates() []string {
	// 新规范优先：Voice Agent 反向求助接口。
	if u := strings.TrimSpace(os.Getenv("PPT_VOICE_MESSAGE_URL")); u != "" {
		return []string{u}
	}
	if base := strings.TrimRight(strings.TrimSpace(os.Getenv("VOICE_AGENT_BASE_URL")), "/"); base != "" {
		return []string{
			base + "/api/v1/voice/ppt_message",
			base + "/api/v1/voice/ppt_message_tool",
		}
	}
	// 兼容旧配置：直接 webhook URL。
	if u := strings.TrimSpace(os.Getenv("PPT_VOICE_WEBHOOK_URL")); u != "" {
		return []string{u}
	}
	return nil
}

func (s *Server) postVoiceMessage(msg voicePPTMessage) {
	cands := s.voiceEndpointCandidates()
	if len(cands) == 0 {
		if msg.MsgType == "conflict_question" {
			log.Printf("[voice/conflict_question] task=%s page=%s ctx=%s text=%s", msg.TaskID, msg.PageID, msg.ContextID, msg.TTSText)
		} else {
			log.Printf("[voice/ppt_status] task=%s %s", msg.TaskID, msg.TTSText)
		}
		return
	}
	go func() {
		body, _ := json.Marshal(msg)
		for i, u := range cands {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			req, _ := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			if strings.TrimSpace(s.Cfg.InternalKey) != "" {
				req.Header.Set("X-Internal-Key", s.Cfg.InternalKey)
			}
			resp, err := s.HC.Do(req)
			cancel()
			if err != nil {
				if i == len(cands)-1 {
					log.Printf("voice message post failed: %v", err)
				}
				continue
			}
			_ = resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return
			}
			if i == len(cands)-1 {
				log.Printf("voice message rejected: %s status=%d", u, resp.StatusCode)
			}
		}
	}()
}

func (s *Server) voiceConflictQuestion(taskID, pageID, contextID, ttsText string) {
	s.postVoiceMessage(voicePPTMessage{
		TaskID:    taskID,
		PageID:    pageID,
		Priority:  "high",
		ContextID: contextID,
		TTSText:   ttsText,
		MsgType:   "conflict_question",
	})
}

// notifyPPTExportComplete 对齐 Python safe_notify_voice_agent_ppt_status（§3.4）。
func (s *Server) notifyPPTExportComplete(taskID, format string) {
	tts := fmt.Sprintf("课件导出完成，请下载（%s）", format)
	s.postVoiceMessage(voicePPTMessage{
		TaskID:    taskID,
		PageID:    "",
		Priority:  "normal",
		ContextID: "",
		TTSText:   tts,
		MsgType:   "ppt_status",
	})
}

func (s *Server) enterSuspension(ctx context.Context, t *task.Task, suspendPageID, question, reason string) {
	if suspendPageID == "" || t.Pages[suspendPageID] == nil {
		suspendPageID = merge.SuspendPageIDForTask(t, merge.GlobalKey)
	}
	if suspendPageID == "" {
		return
	}
	ctxID := s.newContextID()
	now := task.UTCMS()
	sp := &task.SuspendedPage{
		PageID:          suspendPageID,
		ContextID:       ctxID,
		Reason:          reason,
		QuestionForUser: question,
		SuspendedAt:     now,
		LastAskedAt:     now,
		AskCount:        1,
	}
	t.SuspendedPages[suspendPageID] = sp
	t.OpenConflictContexts[ctxID] = suspendPageID
	t.Pages[suspendPageID].Status = "suspended_for_human"
	t.LastUpdate = now
	s.persist(ctx, t)
	_ = s.saveCanvasFromTask(ctx, t, "")
	s.voiceConflictQuestion(t.TaskID, suspendPageID, ctxID,
		coalesce(question, "课件编辑需要您确认一个问题，请回答。"))
	s.scheduleSuspendWatch(t.TaskID, suspendPageID)
}

func (s *Server) partialRefreshPages(ctx context.Context, taskID string, pageIDs []string) error {
	t := s.getTask(ctx, taskID)
	if t == nil {
		return nil
	}
	for _, pid := range pageIDs {
		if p := t.Pages[pid]; p != nil {
			inner := slideutil.ExtractHTMLFromPy(p.PyCode)
			_, _ = s.writeSlidePreview(ctx, taskID, p.SlideIndex, inner, false)
		}
	}
	return s.saveCanvasFromTask(ctx, t, "")
}

func (s *Server) feedbackRelatedWithLLM(ctx context.Context, sp *task.SuspendedPage, intents []merge.Intent, rawText string) (bool, error) {
	if strings.ToLower(strings.TrimSpace(os.Getenv("PPT_SUSPEND_RELATED_USE_LLM"))) == "false" {
		return false, nil
	}
	q := sp.QuestionForUser
	if len([]rune(q)) > 800 {
		q = string([]rune(q)[:800])
	}
	r := sp.Reason
	if len([]rune(r)) > 400 {
		r = string([]rune(r)[:400])
	}
	rt := rawText
	if len([]rune(rt)) > 1200 {
		rt = string([]rune(rt)[:1200])
	}
	var lines strings.Builder
	for _, it := range intents {
		ins := it.Instruction
		if len([]rune(ins)) > 300 {
			ins = string([]rune(ins)[:300])
		}
		lines.WriteString(fmt.Sprintf("- %s / %s: %s\n", it.ActionType, it.TargetPageID, ins))
	}
	intentBlock := lines.String()
	if intentBlock == "" {
		intentBlock = "（无）"
	}
	prompt := fmt.Sprintf(`你是教学课件场景的二元分类器（只输出 JSON，不要 Markdown）。

【系统因编辑冲突向教师提出的问题】
%s

【内部原因标签】
%s

【教师新输入 ASR】
%s

【解析出的修改意图】
%s

任务：这条新反馈是否在**回应、澄清或选择**上述冲突问题（与「该问句/该决策」同一话题）？
- 若是（包括在回答选 A 还是 B、确认偏好等）→ related=true
- 若明显在提**无关的新修改**（与上述问题不是一回事）→ related=false

只输出一行 JSON：{"related": true} 或 {"related": false}`, q, r, rt, intentBlock)

	out, err := s.LLM.Complete(ctx, prompt, "只输出 JSON。", &toolllm.Options{JSONMode: true})
	if err != nil {
		return false, err
	}
	var m map[string]any
	_ = json.Unmarshal([]byte(out), &m)
	if m == nil {
		return false, fmt.Errorf("parse json")
	}
	switch v := m["related"].(type) {
	case bool:
		return v, nil
	case string:
		return strings.EqualFold(v, "true"), nil
	default:
		return false, nil
	}
}

func (s *Server) mergeDeps(ctx context.Context) merge.Deps {
	return merge.Deps{
		Redis: s.Redis,
		GetTask: func(c context.Context, taskID string) *task.Task {
			return s.getTask(c, taskID)
		},
		Persist: s.persist,
		SaveCanvas: func(c context.Context, t *task.Task) error {
			return s.saveCanvasFromTask(c, t, "")
		},
		MergeLock:     s.mergeLock,
		IsDeckBusy:    s.isDeckBusy,
		AppendPending: s.Store.AppendPendingFeedback,
		ScheduleRegen: s.startBackgroundRegen,
		EditSlideHTML: s.editSlideHTML,
		MergeLLM: func(c context.Context, user, system string) (string, error) {
			return s.LLM.MergeDecisionByTool(c, user, system)
		},
		PartialRefresh:         s.partialRefreshPages,
		EnterSuspension:        s.enterSuspension,
		NewContextID:           s.newContextID,
		CancelSuspendWatcher:   s.cancelSuspendWatcher,
		FeedbackRelatedWithLLM: s.feedbackRelatedWithLLM,
		VoiceConflictQuestion:  s.voiceConflictQuestion,
		StartMergeJob: func(taskID string, intents []merge.Intent, baseTS int64, rawText string) {
			d := s.mergeDeps(context.Background())
			go merge.RunFeedbackMergeJob(context.Background(), d, taskID, intents, baseTS, rawText)
		},
	}
}

</think>
Removing the hack: calling `slideutil` directly from `merge_bridge.go`.

<｜tool▁calls▁begin｜><｜tool▁call▁begin｜>
StrReplace