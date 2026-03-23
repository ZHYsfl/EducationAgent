package merge

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"educationagent/ppt_agent_service_go/internal/redisx"
	"educationagent/ppt_agent_service_go/internal/slideutil"
	"educationagent/ppt_agent_service_go/internal/task"
)

// Deps 合并流水线依赖（由 pptserver 注入，避免循环 import）。
type Deps struct {
	Redis *redisx.Store

	GetTask   func(ctx context.Context, taskID string) *task.Task
	Persist   func(ctx context.Context, t *task.Task)
	SaveCanvas func(ctx context.Context, t *task.Task) error
	MergeLock func(taskID, bucket string) *sync.Mutex

	IsDeckBusy    func(taskID string) bool
	AppendPending func(taskID string, lines []string)
	ScheduleRegen func(taskID string)

	EditSlideHTML func(ctx context.Context, topic, curHTML, instruction, actionType string) (string, error)
	MergeLLM      MergeLLMCaller

	PartialRefresh func(ctx context.Context, taskID string, pageIDs []string) error

	EnterSuspension func(ctx context.Context, t *task.Task, suspendPageID, question, reason string)
	NewContextID    func() string
	CancelSuspendWatcher func(taskID, pageID string)

	// StartMergeJob 异步启动合并任务（通常为 go RunFeedbackMergeJob(...））
	StartMergeJob func(taskID string, intents []Intent, baseTS int64, rawText string)

	// FeedbackRelatedWithLLM 悬挂页新反馈是否与冲突提问相关；nil 或失败视为 false（会重播）。
	FeedbackRelatedWithLLM func(ctx context.Context, sp *task.SuspendedPage, intents []Intent, rawText string) (bool, error)
	VoiceConflictQuestion  func(taskID, pageID, contextID, ttsText string)
}

func apply393Fallback() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("PPT_393_APPLY_LLM_FALLBACK")))
	return v == "" || v == "1" || v == "true" || v == "yes"
}

func getPyCodeFromCanvasDoc(doc map[string]any, pageID string) string {
	pages, _ := doc["pages"].(map[string]any)
	if pages == nil {
		return ""
	}
	ent, _ := pages[pageID].(map[string]any)
	if ent == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(ent["py_code"]))
}

// SuspendPageIDForTask 导出供悬挂页选择默认 page（与 suspendPageIDForBucket 一致）。
func SuspendPageIDForTask(t *task.Task, bucket string) string {
	return suspendPageIDForBucket(t, bucket)
}

func suspendPageIDForBucket(t *task.Task, bucket string) string {
	if bucket == GlobalKey {
		if t.CurrentViewingPageID != "" && t.Pages[t.CurrentViewingPageID] != nil {
			return t.CurrentViewingPageID
		}
		if len(t.PageOrder) > 0 {
			return t.PageOrder[0]
		}
		return ""
	}
	return bucket
}

// GetCurrentCodeForMerge 返回 (currentCode, pageIDForPatch)。
func GetCurrentCodeForMerge(ctx context.Context, red *redisx.Store, t *task.Task, bucket string) (string, string) {
	var doc map[string]any
	if red != nil && red.OK {
		doc, _ = red.LoadCanvasDocument(ctx, t.TaskID)
	}
	order := t.PageOrder
	if doc != nil {
		if po, ok := doc["page_order"].([]any); ok && len(po) > 0 {
			order = make([]string, 0, len(po))
			for _, x := range po {
				order = append(order, fmt.Sprint(x))
			}
		}
	}

	if bucket == GlobalKey {
		var parts []string
		for _, pid := range order {
			if pid == "" {
				continue
			}
			code := ""
			if doc != nil {
				code = getPyCodeFromCanvasDoc(doc, pid)
			}
			if code == "" {
				if p := t.Pages[pid]; p != nil {
					code = p.PyCode
				}
			}
			parts = append(parts, code)
		}
		current := strings.Join(parts, "\n")
		if strings.TrimSpace(current) == "" && len(t.Pages) > 0 {
			for _, pid := range t.PageOrder {
				if p := t.Pages[pid]; p != nil {
					parts = append(parts, p.PyCode)
				}
			}
			current = strings.Join(parts, "\n")
		}
		return current, suspendPageIDForBucket(t, GlobalKey)
	}

	code := ""
	if doc != nil {
		code = getPyCodeFromCanvasDoc(doc, bucket)
	}
	if code == "" {
		if p := t.Pages[bucket]; p != nil {
			code = p.PyCode
		}
	}
	return code, bucket
}

func ensurePageMerge(t *task.Task, bucket string) *task.PageMerge {
	if t.PageMerges[bucket] == nil {
		t.PageMerges[bucket] = &task.PageMerge{
			ChainBaselinePages: make(map[string]string),
		}
	}
	if t.PageMerges[bucket].ChainBaselinePages == nil {
		t.PageMerges[bucket].ChainBaselinePages = make(map[string]string)
	}
	return t.PageMerges[bucket]
}

func htmlFromMergeDecision(decision MergeDecision, currentHTML string) string {
	mpy := strings.TrimSpace(decision.MergedPycode)
	if mpy == "" {
		return currentHTML
	}
	h := slideutil.ExtractHTMLFromPy(mpy)
	if strings.TrimSpace(h) == "" {
		return currentHTML
	}
	return h
}

func onePageNewHTML(ctx context.Context, deps Deps, t *task.Task, p *task.Page, intent Intent, decision MergeDecision) *string {
	currentHTML := slideutil.ExtractHTMLFromPy(p.PyCode)
	mergedHTML := htmlFromMergeDecision(decision, currentHTML)
	if mergedHTML != currentHTML {
		return &mergedHTML
	}
	if decision.RuleMergePath {
		if ruled := TryRuleApplyHTML(currentHTML, intent.Instruction); ruled != nil {
			return ruled
		}
		if !apply393Fallback() {
			return &currentHTML
		}
	}
	if deps.EditSlideHTML == nil {
		return nil
	}
	out, err := deps.EditSlideHTML(ctx, t.Topic, currentHTML, intent.Instruction, intent.ActionType)
	if err != nil {
		return nil
	}
	return &out
}

func applyMergeDecisionToPages(ctx context.Context, deps Deps, taskID, bucket string, decision MergeDecision, intent Intent) {
	if decision.MergeStatus != "auto_resolved" {
		return
	}
	t := deps.GetTask(ctx, taskID)
	if t == nil {
		return
	}
	busy := deps.IsDeckBusy(taskID)
	line := fmt.Sprintf("[partial_edit bucket=%s action=%s] %s", bucket, intent.ActionType, intent.Instruction)
	if busy {
		deps.AppendPending(taskID, []string{line})
		deps.Persist(ctx, t)
		return
	}

	var changed []string

	applyPID := func(pid string) bool {
		p := t.Pages[pid]
		if p == nil {
			return true
		}
		nh := onePageNewHTML(ctx, deps, t, p, intent, decision)
		if nh == nil {
			return false
		}
		p.PyCode = slideutil.WrapSlideHTML(*nh, pid, p.SlideIndex)
		p.Version++
		p.UpdatedAt = task.UTCMS()
		changed = append(changed, pid)
		return true
	}

	if bucket == GlobalKey {
		for _, pid := range append([]string(nil), t.PageOrder...) {
			if !applyPID(pid) {
				t.Description += "\n[partial_edit_fallback_full] 无可用编辑模型，触发整册重生成\n"
				deps.Persist(ctx, t)
				deps.ScheduleRegen(taskID)
				return
			}
		}
	} else {
		if !applyPID(bucket) {
			t.Description += fmt.Sprintf("\n[partial_edit_fallback_full page=%s] %s\n", bucket, intent.Instruction)
			deps.Persist(ctx, t)
			deps.ScheduleRegen(taskID)
			return
		}
	}

	t.Description += "\n" + line + "\n"
	t.LastUpdate = task.UTCMS()

	pm := ensurePageMerge(t, bucket)
	if bucket == GlobalKey {
		for _, pid := range t.PageOrder {
			if p := t.Pages[pid]; p != nil {
				pm.ChainBaselinePages[pid] = p.PyCode
			}
		}
	} else if p := t.Pages[bucket]; p != nil {
		pm.ChainBaselinePages[bucket] = p.PyCode
	}

	_ = deps.SaveCanvas(ctx, t)
	if deps.PartialRefresh != nil && len(changed) > 0 {
		_ = deps.PartialRefresh(ctx, taskID, changed)
	}
	deps.Persist(ctx, t)
}

// ProcessOneIntent 单条意图（不含锁）。
func ProcessOneIntent(ctx context.Context, deps Deps, taskID string, intent Intent, baseTimestamp int64, rawText string, chainContinuation bool) {
	t := deps.GetTask(ctx, taskID)
	if t == nil {
		return
	}

	switch intent.ActionType {
	case "insert_before", "insert_after", "delete":
		line := fmt.Sprintf("[structural action=%s target=%s] %s", intent.ActionType, intent.TargetPageID, intent.Instruction)
		busy := deps.IsDeckBusy(taskID)
		if busy {
			deps.AppendPending(taskID, []string{line})
		} else {
			t.Description += "\n" + line + "\n"
			t.LastUpdate = task.UTCMS()
		}
		if !busy {
			deps.ScheduleRegen(taskID)
		}
		deps.Persist(ctx, t)
		return
	}

	bucket := MergeBucket(intent)
	pm := ensurePageMerge(t, bucket)

	if bucket != GlobalKey && t.Pages[bucket] == nil {
		t.Description += fmt.Sprintf("\n[merge skip: missing page %s]\n", bucket)
		t.LastUpdate = task.UTCMS()
		deps.Persist(ctx, t)
		return
	}

	current, pageForPatch := GetCurrentCodeForMerge(ctx, deps.Redis, t, bucket)

	var basePages map[string]any
	if chainContinuation && len(pm.ChainBaselinePages) > 0 {
		basePages = make(map[string]any)
		for pid, code := range pm.ChainBaselinePages {
			basePages[pid] = map[string]any{"page_id": pid, "py_code": code, "status": "completed"}
		}
	} else if chainContinuation && pm.ChainVADTimestamp > 0 && deps.Redis != nil && deps.Redis.OK {
		snap, _ := deps.Redis.LoadSnapshotDocument(ctx, taskID, pm.ChainVADTimestamp)
		if snap != nil {
			if pgs, ok := snap["pages"].(map[string]any); ok {
				basePages = pgs
			}
		}
	} else if baseTimestamp > 0 && deps.Redis != nil && deps.Redis.OK {
		snap, _ := deps.Redis.LoadSnapshotDocument(ctx, taskID, baseTimestamp)
		if snap != nil {
			if pgs, ok := snap["pages"].(map[string]any); ok {
				basePages = pgs
			}
		}
	}

	var patch string
	if bucket == GlobalKey {
		if len(basePages) > 0 {
			patch = BuildSystemPatchGlobal(basePages, t.PageOrder, current)
		} else {
			patch = "(无 VAD 快照，system_patch 为空)"
		}
	} else {
		if len(basePages) > 0 {
			patch = BuildSystemPatch(basePages, pageForPatch, current)
		} else {
			patch = "(无 VAD 快照，system_patch 为空)"
		}
	}

	decision := DecideThreeWayMerge(ctx, taskID, bucket, current, patch, intent.Instruction, intent.ActionType, deps.MergeLLM)

	if decision.MergeStatus == "ask_human" {
		pid := suspendPageIDForBucket(t, bucket)
		if deps.EnterSuspension != nil && pid != "" {
			q := decision.QuestionForUser
			if strings.TrimSpace(q) == "" {
				q = "需要您确认如何修改课件。"
			}
			deps.EnterSuspension(ctx, t, pid, q, fmt.Sprintf("merge ask_human bucket=%s action=%s", bucket, intent.ActionType))
		}
		deps.Persist(ctx, t)
		return
	}

	applyMergeDecisionToPages(ctx, deps, taskID, bucket, decision, intent)
}

// RunMergeIntentSerial §3.9.2 同 bucket 串行 + 排队。
func RunMergeIntentSerial(ctx context.Context, deps Deps, taskID string, intent Intent, baseTimestamp int64, rawText string, chainContinuation bool) {
	bucket := MergeBucket(intent)
	mu := deps.MergeLock(taskID, bucket)
	mu.Lock()
	t := deps.GetTask(ctx, taskID)
	if t == nil {
		mu.Unlock()
		return
	}
	pm := ensurePageMerge(t, bucket)
	if pm.IsRunning {
		d := map[string]any{
			"action_type":     intent.ActionType,
			"target_page_id":  intent.TargetPageID,
			"instruction":     intent.Instruction,
			"base_timestamp":  baseTimestamp,
			"raw_text":        rawText,
		}
		pm.PendingIntents = append(pm.PendingIntents, d)
		deps.Persist(ctx, t)
		mu.Unlock()
		return
	}
	if !chainContinuation {
		if baseTimestamp > 0 {
			if pm.ChainVADTimestamp != 0 && pm.ChainVADTimestamp != baseTimestamp {
				pm.ChainBaselinePages = make(map[string]string)
			}
			pm.ChainVADTimestamp = baseTimestamp
		}
	}
	pm.IsRunning = true
	deps.Persist(ctx, t)
	mu.Unlock()

	func() {
		defer func() {
			mu.Lock()
			t2 := deps.GetTask(ctx, taskID)
			if t2 != nil {
				pm2 := ensurePageMerge(t2, bucket)
				pm2.IsRunning = false
				deps.Persist(ctx, t2)
			}
			mu.Unlock()
			drainPendingMergeIntents(ctx, deps, taskID, bucket)
			if t2 := deps.GetTask(ctx, taskID); t2 != nil {
				deps.Persist(ctx, t2)
			}
		}()
		ProcessOneIntent(ctx, deps, taskID, intent, baseTimestamp, rawText, chainContinuation)
	}()
}

func drainPendingMergeIntents(ctx context.Context, deps Deps, taskID, bucketKey string) {
	t := deps.GetTask(ctx, taskID)
	if t == nil {
		return
	}
	if _, suspended := t.SuspendedPages[bucketKey]; suspended {
		return
	}
	if bucketKey == GlobalKey && len(t.SuspendedPages) > 0 {
		return
	}
	pm := ensurePageMerge(t, bucketKey)
	if len(pm.PendingIntents) == 0 {
		return
	}
	raw := pm.PendingIntents[0]
	pm.PendingIntents = pm.PendingIntents[1:]
	deps.Persist(ctx, t)

	it := Intent{
		ActionType:   fmt.Sprint(raw["action_type"]),
		TargetPageID: fmt.Sprint(raw["target_page_id"]),
		Instruction:  fmt.Sprint(raw["instruction"]),
	}
	var baseTS int64
	switch x := raw["base_timestamp"].(type) {
	case float64:
		baseTS = int64(x)
	case int64:
		baseTS = x
	case int:
		baseTS = int64(x)
	}
	rt := fmt.Sprint(raw["raw_text"])
	RunMergeIntentSerial(ctx, deps, taskID, it, baseTS, rt, true)
}

// RunFeedbackMergeJob 顺序处理一批意图。
func RunFeedbackMergeJob(ctx context.Context, deps Deps, taskID string, intents []Intent, baseTS int64, rawText string) {
	for _, it := range intents {
		RunMergeIntentSerial(ctx, deps, taskID, it, baseTS, rawText, false)
	}
}

// FeedbackTargetsSuspendedPage 是否与悬挂页相关。
func FeedbackTargetsSuspendedPage(t *task.Task, intents []Intent) bool {
	suspended := make(map[string]struct{})
	for pid := range t.SuspendedPages {
		suspended[pid] = struct{}{}
	}
	for _, it := range intents {
		if it.ActionType == "global_modify" {
			continue
		}
		pid := strings.TrimSpace(it.TargetPageID)
		if pid != "" {
			if _, ok := suspended[pid]; ok {
				return true
			}
		}
	}
	return false
}

// QueueIntentsOnSuspended §3.9.1
func QueueIntentsOnSuspended(ctx context.Context, deps Deps, t *task.Task, intents []Intent, rawText string) {
	var targetPID string
	for _, it := range intents {
		if it.ActionType == "global_modify" {
			continue
		}
		pid := strings.TrimSpace(it.TargetPageID)
		if _, ok := t.SuspendedPages[pid]; ok {
			targetPID = pid
			break
		}
	}
	if targetPID == "" {
		return
	}
	targetSP := t.SuspendedPages[targetPID]
	if targetSP == nil {
		return
	}
	for _, it := range intents {
		targetSP.PendingFeedbacks = append(targetSP.PendingFeedbacks, map[string]any{
			"action_type":     it.ActionType,
			"target_page_id":  it.TargetPageID,
			"instruction":     it.Instruction,
		})
	}
	t.LastUpdate = task.UTCMS()
	related := false
	if deps.FeedbackRelatedWithLLM != nil {
		r, err := deps.FeedbackRelatedWithLLM(ctx, targetSP, intents, rawText)
		if err == nil {
			related = r
		}
	}
	if !related && deps.VoiceConflictQuestion != nil {
		deps.VoiceConflictQuestion(t.TaskID, targetPID, targetSP.ContextID,
			coalesceStr(targetSP.QuestionForUser, "您有新的修改说明；请先确认上一问题，或一并说明您的偏好。"))
	}
	_ = deps.SaveCanvas(ctx, t)
	deps.Persist(ctx, t)
}

func coalesceStr(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// HandleResolveConflictBranch 返回 ("skip"|"ok"|"err", message)。
func HandleResolveConflictBranch(ctx context.Context, deps Deps, t *task.Task, replyToContextID string, baseTS int64, rawText string, allIntents []Intent) (string, string) {
	hasResolve := false
	for _, it := range allIntents {
		if it.ActionType == "resolve_conflict" {
			hasResolve = true
			break
		}
	}
	if !hasResolve {
		return "skip", ""
	}
	ctxID := strings.TrimSpace(replyToContextID)
	if ctxID == "" {
		return "err", "resolve_conflict 必须携带 reply_to_context_id"
	}
	pid, ok := t.OpenConflictContexts[ctxID]
	if !ok || pid == "" {
		return "err", "无待解决的冲突上下文（context_id 无效或已过期）"
	}
	if deps.CancelSuspendWatcher != nil {
		deps.CancelSuspendWatcher(t.TaskID, pid)
	}
	delete(t.OpenConflictContexts, ctxID)
	if p, ok2 := t.Pages[pid]; ok2 && p != nil {
		p.Status = "completed"
	}
	sp := t.SuspendedPages[pid]
	delete(t.SuspendedPages, pid)

	var lines []string
	for _, it := range allIntents {
		if it.ActionType == "resolve_conflict" {
			lines = append(lines, fmt.Sprintf("[resolve_conflict context_id=%s target=%s] %s", ctxID, it.TargetPageID, it.Instruction))
		}
	}
	if sp != nil {
		for _, pb := range sp.PendingFeedbacks {
			lines = append(lines, "[pending_while_suspended] "+fmt.Sprint(pb))
		}
	}
	block := "\n\n[教师反馈修改]\n" + strings.Join(lines, "\n") + "\n"
	busy := deps.IsDeckBusy(t.TaskID)
	if busy {
		deps.AppendPending(t.TaskID, []string{block})
	} else {
		t.Description += block
	}
	t.LastUpdate = task.UTCMS()

	if !busy && t.Pages[pid] != nil && deps.EditSlideHTML != nil {
		instr := strings.Join(lines, "\n")
		html := slideutil.ExtractHTMLFromPy(t.Pages[pid].PyCode)
		if nh, err := deps.EditSlideHTML(ctx, t.Topic, html, instr, "resolve_conflict"); err == nil {
			p := t.Pages[pid]
			p.PyCode = slideutil.WrapSlideHTML(nh, pid, p.SlideIndex)
			p.Version++
			p.UpdatedAt = task.UTCMS()
		}
	} else if !busy {
		deps.ScheduleRegen(t.TaskID)
	}
	if !busy && deps.PartialRefresh != nil && t.Pages[pid] != nil && deps.EditSlideHTML != nil {
		_ = deps.PartialRefresh(ctx, t.TaskID, []string{pid})
	}
	_ = deps.SaveCanvas(ctx, t)

	var pendingForMerge []Intent
	if sp != nil {
		for _, pb := range sp.PendingFeedbacks {
			at := fmt.Sprint(pb["action_type"])
			if at == "" {
				at = "modify"
			}
			pendingForMerge = append(pendingForMerge, Intent{
				ActionType:   at,
				TargetPageID: fmt.Sprint(pb["target_page_id"]),
				Instruction:  fmt.Sprint(pb["instruction"]),
			})
		}
	}
	if len(pendingForMerge) > 0 && !busy && deps.StartMergeJob != nil {
		deps.StartMergeJob(t.TaskID, pendingForMerge, baseTS, rawText)
	}
	deps.Persist(ctx, t)
	return "ok", ""
}

// IntentsFromMaps 过滤非法项（action 已由上层校验）。
func IntentsFromMaps(maps []map[string]any) []Intent {
	out := make([]Intent, 0, len(maps))
	for _, m := range maps {
		out = append(out, IntentFromMap(m))
	}
	return out
}
