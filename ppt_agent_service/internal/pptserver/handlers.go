package pptserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"educationagent/ppt_agent_service_go/internal/api"
	"educationagent/ppt_agent_service_go/internal/auth"
	"educationagent/ppt_agent_service_go/internal/canvas"
	"educationagent/ppt_agent_service_go/internal/ecode"
	"educationagent/ppt_agent_service_go/internal/exportmd"
	"educationagent/ppt_agent_service_go/internal/gen"
	"educationagent/ppt_agent_service_go/internal/merge"
	"educationagent/ppt_agent_service_go/internal/redisx"
	"educationagent/ppt_agent_service_go/internal/renderhtml"
	"educationagent/ppt_agent_service_go/internal/slideutil"
	"educationagent/ppt_agent_service_go/internal/task"
	"educationagent/ppt_agent_service_go/internal/toolllm"
)

func (s *Server) saveCanvasFromTask(ctx context.Context, t *task.Task, override string) error {
	if s.Redis == nil || !s.Redis.OK {
		return nil
	}
	doc := canvas.TaskToCanvasDocument(t, task.UTCMS(), override)
	return s.Redis.SaveCanvasDocument(ctx, t.TaskID, doc)
}

// runGeneration 后台循环：生成完成后若有排队反馈则合并 description 再跑一轮（对齐 Python generate_task_background）。
// 须由 runGenerationLocked / waitDeckAndRunGeneration 持有 deckRunning 后在本 goroutine 内调用。
func (s *Server) runGeneration(tid string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	for {
		t := s.getTask(ctx, tid)
		if t == nil {
			return
		}
		t.Version++
		t.Status = "generating"
		t.LastUpdate = task.UTCMS()
		s.persist(ctx, t)
		_ = s.saveCanvasFromTask(ctx, t, "rendering")
		err := gen.RunDeck(ctx, s.LLM, t, s.Store, s.Redis, func() { s.persist(ctx, t) })
		if err != nil {
			t = s.getTask(ctx, tid)
			if t != nil {
				t.Status = "failed"
				t.LastUpdate = task.UTCMS()
				s.persist(ctx, t)
				_ = s.saveCanvasFromTask(ctx, t, "")
			}
			return
		}
		pending := s.Store.TakePendingFeedbackLines(tid)
		if len(pending) == 0 {
			return
		}
		t = s.getTask(ctx, tid)
		if t == nil {
			return
		}
		t.Description = t.Description + "\n\n[教师反馈修改]\n" + strings.Join(pending, "\n")
		t.LastUpdate = task.UTCMS()
		s.persist(ctx, t)
	}
}

func (s *Server) runGenerationLocked(tid string) {
	if _, loaded := s.deckRunning.LoadOrStore(tid, true); loaded {
		return
	}
	go func() {
		defer s.deckRunning.Delete(tid)
		s.runGeneration(tid)
	}()
}

// startBackgroundRegen 在已完成任务上触发整册重生成；若当前仍在生成则轮询等待（避免与 init 的 runGeneration 并发）。
func (s *Server) startBackgroundRegen(tid string) {
	go s.waitDeckAndRunGeneration(tid)
}

func (s *Server) waitDeckAndRunGeneration(tid string) {
	tick := time.NewTicker(400 * time.Millisecond)
	defer tick.Stop()
	deadline := time.After(30 * time.Minute)
	for {
		select {
		case <-deadline:
			return
		case <-tick.C:
			if _, loaded := s.deckRunning.LoadOrStore(tid, true); !loaded {
				func() {
					defer s.deckRunning.Delete(tid)
					s.runGeneration(tid)
				}()
				return
			}
		}
	}
}

func (s *Server) pptInit(w http.ResponseWriter, r *http.Request) {
	var body struct {
		UserID           string           `json:"user_id"`
		Topic            string           `json:"topic"`
		Description      string           `json:"description"`
		TotalPages       int              `json:"total_pages"`
		Audience         string           `json:"audience"`
		GlobalStyle      string           `json:"global_style"`
		SessionID        string           `json:"session_id"`
		TeachingElements json.RawMessage  `json:"teaching_elements"`
		ReferenceFiles   []map[string]any `json:"reference_files"`
		ExtraContext     string           `json:"extra_context"`
		RetrievalTrace   json.RawMessage  `json:"retrieval_trace"`
		ContextInjections []map[string]any `json:"context_injections"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		api.WriteErr(w, ecode.Param, api.InvalidJSONMessage(err))
		return
	}
	if strings.TrimSpace(body.Topic) == "" {
		api.WriteErr(w, ecode.Param, "参数 topic 不能为空")
		return
	}
	if strings.TrimSpace(body.Description) == "" {
		api.WriteErr(w, ecode.Param, "参数 description 不能为空")
		return
	}
	if strings.TrimSpace(body.UserID) == "" {
		api.WriteErr(w, ecode.Param, "参数 user_id 不能为空")
		return
	}
	if strings.TrimSpace(body.SessionID) == "" {
		api.WriteErr(w, ecode.Param, "参数 session_id 不能为空")
		return
	}
	if body.TotalPages < 0 {
		api.WriteErr(w, ecode.Param, "total_pages 不能为负")
		return
	}
	if s.assertUserMatch(w, r, body.UserID) {
		return
	}
	if msg := verifyUserAllowed(body.UserID); msg != "" {
		api.WriteErr(w, ecode.NotFound, msg)
		return
	}
	_ = s.DB.EnsureUserSession(r.Context(), body.UserID, body.SessionID)

	tid := task.NewTaskID()
	t := task.NewTask()
	t.TaskID = tid
	t.UserID = strings.TrimSpace(body.UserID)
	t.Topic = strings.TrimSpace(body.Topic)
	t.Description = buildEffectiveDescriptionInit(body.Description, body.ReferenceFiles, body.TeachingElements)
	t.TotalPages = body.TotalPages
	t.Audience = strings.TrimSpace(body.Audience)
	t.GlobalStyle = strings.TrimSpace(body.GlobalStyle)
	t.SessionID = strings.TrimSpace(body.SessionID)
	t.ReferenceFiles = body.ReferenceFiles
	t.TeachingElements = body.TeachingElements
	t.ExtraContext = strings.TrimSpace(body.ExtraContext)
	t.RetrievalTrace = body.RetrievalTrace
	t.ContextInjections = body.ContextInjections
	t.Status = "pending"
	t.LastUpdate = task.UTCMS()

	s.Store.Set(tid, t)
	s.persist(r.Context(), t)
	_ = s.saveCanvasFromTask(r.Context(), t, "")

	go s.runGenerationLocked(tid)

	api.WriteOK(w, map[string]string{"task_id": tid})
}

func (s *Server) createTask(w http.ResponseWriter, r *http.Request) {
	var body struct {
		UserID      string `json:"user_id"`
		Topic       string `json:"topic"`
		Description string `json:"description"`
		TotalPages  int    `json:"total_pages"`
		Audience    string `json:"audience"`
		GlobalStyle string `json:"global_style"`
		SessionID   string `json:"session_id"`
		ExtraContext string `json:"extra_context"`
		RetrievalTrace json.RawMessage `json:"retrieval_trace"`
		ContextInjections []map[string]any `json:"context_injections"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		api.WriteErr(w, ecode.Param, api.InvalidJSONMessage(err))
		return
	}
	if strings.TrimSpace(body.Topic) == "" {
		api.WriteErr(w, ecode.Param, "参数 topic 不能为空")
		return
	}
	if strings.TrimSpace(body.Description) == "" {
		api.WriteErr(w, ecode.Param, "参数 description 不能为空")
		return
	}
	if strings.TrimSpace(body.UserID) == "" {
		api.WriteErr(w, ecode.Param, "参数 user_id 不能为空")
		return
	}
	if strings.TrimSpace(body.SessionID) == "" {
		api.WriteErr(w, ecode.Param, "参数 session_id 不能为空")
		return
	}
	if body.TotalPages < 0 {
		api.WriteErr(w, ecode.Param, "total_pages 不能为负")
		return
	}
	if s.assertUserMatch(w, r, body.UserID) {
		return
	}
	if msg := verifyUserAllowed(body.UserID); msg != "" {
		api.WriteErr(w, ecode.NotFound, msg)
		return
	}
	_ = s.DB.EnsureUserSession(r.Context(), body.UserID, body.SessionID)

	tid := task.NewTaskID()
	t := task.NewTask()
	t.TaskID = tid
	t.UserID = strings.TrimSpace(body.UserID)
	t.Topic = strings.TrimSpace(body.Topic)
	t.Description = strings.TrimSpace(body.Description)
	t.TotalPages = body.TotalPages
	t.Audience = strings.TrimSpace(body.Audience)
	t.GlobalStyle = strings.TrimSpace(body.GlobalStyle)
	t.SessionID = strings.TrimSpace(body.SessionID)
	t.ExtraContext = strings.TrimSpace(body.ExtraContext)
	t.RetrievalTrace = body.RetrievalTrace
	t.ContextInjections = body.ContextInjections
	t.Status = "pending"
	t.LastUpdate = task.UTCMS()

	s.Store.Set(tid, t)
	s.persist(r.Context(), t)
	_ = s.saveCanvasFromTask(r.Context(), t, "")

	go s.runGenerationLocked(tid)

	api.WriteOK(w, map[string]string{"task_id": tid})
}

func (s *Server) vad(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TaskID        string `json:"task_id"`
		Timestamp     int64  `json:"timestamp"`
		ViewingPageID string `json:"viewing_page_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		api.WriteErr(w, ecode.Param, api.InvalidJSONMessage(err))
		return
	}
	if strings.TrimSpace(body.TaskID) == "" {
		api.WriteErr(w, ecode.Param, "参数 task_id 不能为空")
		return
	}
	if body.Timestamp <= 0 {
		api.WriteErr(w, ecode.Param, "参数 timestamp 无效，应为 Unix 毫秒")
		return
	}
	t := s.getTask(r.Context(), body.TaskID)
	if t == nil {
		api.WriteErr(w, ecode.NotFound, "task_id 不存在")
		return
	}
	if s.assertTaskAccess(w, r, t) {
		return
	}
	if reason := redisx.UnavailableReason(s.Redis); reason != "" {
		api.WriteErr(w, ecode.Dependency, "依赖服务不可用（Redis）："+reason)
		return
	}
	doc := canvas.TaskToCanvasDocument(t, task.UTCMS(), "")
	_ = s.Redis.SaveCanvasDocument(r.Context(), t.TaskID, doc)
	if err := s.Redis.VADDeepCopySnapshot(r.Context(), t.TaskID, body.Timestamp, body.ViewingPageID, doc); err != nil {
		api.WriteErr(w, ecode.Dependency, err.Error())
		return
	}
	if vid := strings.TrimSpace(body.ViewingPageID); vid != "" {
		t.CurrentViewingPageID = vid
		t.LastUpdate = task.UTCMS()
	}
	_ = s.saveCanvasFromTask(r.Context(), t, "")
	s.persist(r.Context(), t)
	api.WriteOK(w, map[string]bool{"accepted": true})
}

func (s *Server) feedback(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TaskID           string           `json:"task_id"`
		BaseTimestamp    int64            `json:"base_timestamp"`
		ViewingPageID    string           `json:"viewing_page_id"`
		ReplyToContextID string           `json:"reply_to_context_id"`
		RawText          string           `json:"raw_text"`
		Intents          []map[string]any `json:"intents"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		api.WriteErr(w, ecode.Param, api.InvalidJSONMessage(err))
		return
	}
	t := s.getTask(r.Context(), body.TaskID)
	if t == nil {
		api.WriteErr(w, ecode.NotFound, "task_id 不存在")
		return
	}
	if s.assertTaskAccess(w, r, t) {
		return
	}
	if vid := strings.TrimSpace(body.ViewingPageID); vid != "" && t.Pages[vid] != nil {
		t.CurrentViewingPageID = vid
		t.LastUpdate = task.UTCMS()
		_ = s.saveCanvasFromTask(r.Context(), t, "")
		s.persist(r.Context(), t)
	}
	if _, c, msg := validateFeedbackIntents(t, body.Intents, body.ReplyToContextID, body.RawText); c != 0 {
		api.WriteErr(w, c, msg)
		return
	}
	if t.Status == "failed" || t.Status == "exporting" {
		api.WriteErr(w, ecode.Conflict, "任务已终止，不接受反馈")
		return
	}

	allIntents := merge.IntentsFromMaps(body.Intents)
	rc, rmsg := merge.HandleResolveConflictBranch(r.Context(), s.mergeDeps(r.Context()), t, body.ReplyToContextID, body.BaseTimestamp, body.RawText, allIntents)
	if rc == "err" {
		api.WriteErr(w, ecode.Param, coalesceNonEmpty(rmsg, "冲突处理失败"))
		return
	}
	t = s.getTask(r.Context(), body.TaskID)
	if t == nil {
		api.WriteErr(w, ecode.NotFound, "task_id 不存在")
		return
	}

	var intentsMerge []merge.Intent
	for _, m := range body.Intents {
		if strings.TrimSpace(fmt.Sprint(m["action_type"])) == "resolve_conflict" {
			continue
		}
		intentsMerge = append(intentsMerge, merge.IntentFromMap(m))
	}
	if len(intentsMerge) == 0 {
		api.WriteOK(w, map[string]any{
			"accepted_intents": len(body.Intents),
			"queued":           rc == "ok",
		})
		return
	}

	var mergeMaps []map[string]any
	for _, m := range body.Intents {
		if strings.TrimSpace(fmt.Sprint(m["action_type"])) == "resolve_conflict" {
			continue
		}
		mergeMaps = append(mergeMaps, m)
	}
	linesMerge, c2, msg2 := validateFeedbackIntents(t, mergeMaps, "", body.RawText)
	if c2 != 0 {
		api.WriteErr(w, c2, msg2)
		return
	}

	switch t.Status {
	case "pending", "generating":
		t.PendingFeedbackLines = append(t.PendingFeedbackLines, linesMerge...)
		t.LastUpdate = task.UTCMS()
		s.persist(r.Context(), t)
		api.WriteOK(w, map[string]any{"accepted_intents": len(body.Intents), "queued": true})
	case "completed":
		if merge.FeedbackTargetsSuspendedPage(t, intentsMerge) {
			merge.QueueIntentsOnSuspended(r.Context(), s.mergeDeps(r.Context()), t, intentsMerge, body.RawText)
			api.WriteOK(w, map[string]any{
				"accepted_intents":  len(body.Intents),
				"queued":            true,
				"suspended_queued": true,
			})
			return
		}
		deps := s.mergeDeps(context.Background())
		go merge.RunFeedbackMergeJob(context.Background(), deps, t.TaskID, intentsMerge, body.BaseTimestamp, body.RawText)
		api.WriteOK(w, map[string]any{"accepted_intents": len(body.Intents), "queued": true})
	default:
		api.WriteErr(w, ecode.Conflict, "任务状态不允许反馈")
	}
}

func coalesceNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

func (s *Server) editSlideHTML(ctx context.Context, topic, currentHTML, instruction, actionType string) (string, error) {
	prompt := fmt.Sprintf(
		"你是课件幻灯片 HTML 编辑。只输出一段 HTML 片段（不要 DOCTYPE、不要 html/head/body 外壳），用于 16:9 幻灯片区域。\n"+
			"课程主题：%s\n操作类型：%s\n\n【当前幻灯片 HTML】\n%s\n\n【教师修改指令】\n%s\n\n"+
			"要求：教学向排版，可用内联 style；禁止 script、禁止 Markdown；只输出替换后的主体 HTML。",
		topic, actionType, truncateRunes(currentHTML, 8000), instruction,
	)
	return s.LLM.Complete(ctx, prompt, "只输出 HTML 片段。", &toolllm.Options{})
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// 对 html2image/PIL 缺失的判定
func renderFailureIsDependency(msg string) bool {
	m := strings.ToLower(msg)
	if strings.Contains(m, "未安装") || strings.Contains(m, "html2image") || strings.Contains(m, "pil") {
		return true
	}
	if strings.Contains(m, "deps") {
		return true
	}
	if strings.Contains(m, "chromedp") || strings.Contains(m, "chrome") || strings.Contains(m, "chromium") {
		return true
	}
	if strings.Contains(m, "executable") && (strings.Contains(m, "not found") || strings.Contains(m, "找不到")) {
		return true
	}
	return false
}

func (s *Server) writeSlidePlaceholder(taskID string, slideIndex int) error {
	dir := filepath.Join(s.Store.TaskDir(taskID), "renders")
	_ = os.MkdirAll(dir, 0o755)
	p := filepath.Join(dir, fmt.Sprintf("slide_%04d.jpg", slideIndex))
	return os.WriteFile(p, gen.MinimalJPEG(), 0o644)
}

// writeSlidePreview 将 innerHTML 写入 renders/slide_XXXX.jpg。
// failHard=true：chromedp 失败则返回错误（供 /canvas/render/execute）；false：失败则回退占位图（合并/悬挂等后台路径）。
func (s *Server) writeSlidePreview(ctx context.Context, taskID string, slideIndex int, innerHTML string, failHard bool) (depUnavailable bool, err error) {
	mode := strings.ToLower(strings.TrimSpace(s.Cfg.CanvasRenderMode))
	if mode == "" {
		mode = "placeholder"
	}
	taskDir := s.Store.TaskDir(taskID)

	switch mode {
	case "chromedp":
		e := renderhtml.WriteSlideJPEG(ctx, taskDir, slideIndex, innerHTML, s.Cfg.ChromePath)
		if e == nil {
			return false, nil
		}
		if failHard {
			return renderFailureIsDependency(e.Error()), e
		}
		return false, s.writeSlidePlaceholder(taskID, slideIndex)
	case "auto":
		if e := renderhtml.WriteSlideJPEG(ctx, taskDir, slideIndex, innerHTML, s.Cfg.ChromePath); e != nil {
			return false, s.writeSlidePlaceholder(taskID, slideIndex)
		}
		return false, nil
	default:
		return false, s.writeSlidePlaceholder(taskID, slideIndex)
	}
}

func (s *Server) canvasStatus(w http.ResponseWriter, r *http.Request) {
	tid := strings.TrimSpace(r.URL.Query().Get("task_id"))
	if tid == "" {
		api.WriteErr(w, ecode.Param, "缺少查询参数 task_id")
		return
	}
	t := s.getTask(r.Context(), tid)
	if t == nil {
		api.WriteErr(w, ecode.NotFound, "task_id 不存在")
		return
	}
	if s.assertTaskAccess(w, r, t) {
		return
	}
	var doc map[string]any
	if s.Redis != nil && s.Redis.OK {
		doc, _ = s.Redis.LoadCanvasDocument(r.Context(), tid)
	}
	api.WriteOK(w, buildCanvasStatusResponse(t, doc, s.Cfg.PublicBaseURL))
}

func buildCanvasStatusResponse(t *task.Task, doc map[string]any, publicBase string) map[string]any {
	if doc == nil || len(doc) == 0 {
		pagesInfo := make([]map[string]any, 0, len(t.PageOrder))
		for _, pid := range t.PageOrder {
			p := t.Pages[pid]
			if p == nil {
				continue
			}
			pagesInfo = append(pagesInfo, map[string]any{
				"page_id":     pid,
				"status":      p.Status,
				"last_update": firstNonZero(p.UpdatedAt, t.LastUpdate),
				"render_url":  redisx.PublicMediaURL(publicBase, p.RenderURL),
			})
		}
		return map[string]any{
			"task_id":                 t.TaskID,
			"page_order":              append([]string(nil), t.PageOrder...),
			"current_viewing_page_id": t.CurrentViewingPageID,
			"pages_info":              pagesInfo,
		}
	}
	pageOrder := toStringSlice(doc["page_order"])
	if len(pageOrder) == 0 {
		pageOrder = append([]string(nil), t.PageOrder...)
	}
	curVid, _ := doc["current_viewing_page_id"].(string)
	if strings.TrimSpace(curVid) == "" {
		curVid = t.CurrentViewingPageID
	}
	rdPages, _ := doc["pages"].(map[string]any)
	pageDisplay, _ := doc["page_display"].(map[string]any)
	pagesInfo := make([]map[string]any, 0, len(pageOrder))
	for _, pid := range pageOrder {
		if pid == "" {
			continue
		}
		rd := map[string]any{}
		if rdPages != nil {
			if m, ok := rdPages[pid].(map[string]any); ok {
				rd = m
			}
		}
		mem := t.Pages[pid]
		disp := map[string]any{}
		if pageDisplay != nil {
			if m, ok := pageDisplay[pid].(map[string]any); ok {
				disp = m
			}
		}
		renderURL := ""
		if mem != nil && mem.RenderURL != "" {
			renderURL = mem.RenderURL
		} else {
			renderURL = fmt.Sprint(disp["render_url"])
			if renderURL == "" {
				renderURL = fmt.Sprint(rd["render_url"])
			}
		}
		status := fmt.Sprint(rd["status"])
		if status == "" && mem != nil {
			status = mem.Status
		}
		if status == "" {
			status = "completed"
		}
		lu := int64FromAny(disp["last_update"])
		if lu == 0 {
			lu = int64FromAny(rd["last_update"])
		}
		if lu == 0 && mem != nil {
			lu = mem.UpdatedAt
		}
		if lu == 0 {
			lu = t.LastUpdate
		}
		pagesInfo = append(pagesInfo, map[string]any{
			"page_id":     pid,
			"status":      status,
			"last_update": lu,
			"render_url":  redisx.PublicMediaURL(publicBase, renderURL),
		})
	}
	return map[string]any{
		"task_id":                 t.TaskID,
		"page_order":              pageOrder,
		"current_viewing_page_id": curVid,
		"pages_info":              pagesInfo,
	}
}

func toStringSlice(v any) []string {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, x := range arr {
		out = append(out, fmt.Sprint(x))
	}
	return out
}

func int64FromAny(v any) int64 {
	switch x := v.(type) {
	case float64:
		return int64(x)
	case int64:
		return x
	case int:
		return int64(x)
	case json.Number:
		i, _ := x.Int64()
		return i
	default:
		return 0
	}
}

func firstNonZero(a, b int64) int64 {
	if a != 0 {
		return a
	}
	return b
}

func (s *Server) taskPreview(w http.ResponseWriter, r *http.Request) {
	tid := chi.URLParam(r, "taskID")
	t := s.getTask(r.Context(), tid)
	if t == nil {
		api.WriteErr(w, ecode.NotFound, "task_id 不存在")
		return
	}
	if s.assertTaskAccess(w, r, t) {
		return
	}
	pages := make([]map[string]any, 0, len(t.PageOrder))
	for _, pid := range t.PageOrder {
		p := t.Pages[pid]
		if p == nil {
			continue
		}
		pages = append(pages, map[string]any{
			"page_id":     p.PageID,
			"status":      p.Status,
			"last_update": firstNonZero(p.UpdatedAt, t.LastUpdate),
			"render_url":  redisx.PublicMediaURL(s.Cfg.PublicBaseURL, p.RenderURL),
		})
	}
	api.WriteOK(w, map[string]any{
		"task_id":                 t.TaskID,
		"status":                  t.Status,
		"page_order":              append([]string(nil), t.PageOrder...),
		"current_viewing_page_id": t.CurrentViewingPageID,
		"pages":                   pages,
	})
}

func (s *Server) getTaskDetail(w http.ResponseWriter, r *http.Request) {
	tid := chi.URLParam(r, "taskID")
	t := s.getTask(r.Context(), tid)
	if t == nil {
		api.WriteErr(w, ecode.NotFound, "task_id 不存在")
		return
	}
	if s.assertTaskAccess(w, r, t) {
		return
	}
	api.WriteOK(w, map[string]any{
		"task_id":                 t.TaskID,
		"session_id":              t.SessionID,
		"user_id":                 t.UserID,
		"topic":                   t.Topic,
		"description":             t.Description,
		"total_pages":             t.TotalPages,
		"audience":                t.Audience,
		"global_style":            t.GlobalStyle,
		"status":                  t.Status,
		"version":                 t.Version,
		"last_update":             t.LastUpdate,
		"current_viewing_page_id": t.CurrentViewingPageID,
		"page_order":              append([]string(nil), t.PageOrder...),
	})
}

func (s *Server) updateTaskStatus(w http.ResponseWriter, r *http.Request) {
	tid := chi.URLParam(r, "taskID")
	t := s.getTask(r.Context(), tid)
	if t == nil {
		api.WriteErr(w, ecode.NotFound, "task_id 不存在")
		return
	}
	if s.assertTaskAccess(w, r, t) {
		return
	}
	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		api.WriteErr(w, ecode.Param, api.InvalidJSONMessage(err))
		return
	}
	st := strings.TrimSpace(body.Status)
	if _, ok := allowedTaskStatus[st]; !ok {
		allowed := make([]string, 0, len(allowedTaskStatus))
		for k := range allowedTaskStatus {
			allowed = append(allowed, k)
		}
		sort.Strings(allowed)
		api.WriteErr(w, ecode.Param, "非法 status，允许："+strings.Join(allowed, ", "))
		return
	}
	t.Status = st
	t.LastUpdate = task.UTCMS()
	s.persist(r.Context(), t)
	api.WriteOK(w, map[string]string{"task_id": tid, "status": st})
}

func (s *Server) listTasks(w http.ResponseWriter, r *http.Request) {
	sid := strings.TrimSpace(r.URL.Query().Get("session_id"))
	if sid == "" {
		api.WriteErr(w, ecode.Param, "缺少查询参数 session_id")
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	ps, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if ps < 1 {
		ps = 20
	}
	if ps > 100 {
		ps = 100
	}
	uidFilter := ""
	if s.Cfg.Enforced() {
		if ac := auth.FromRequest(r); ac != nil && !ac.IsInternal && ac.UserID != "" {
			uidFilter = ac.UserID
		}
	}
	items, total, err := s.DB.ListTasksLight(r.Context(), sid, page, ps, uidFilter)
	if err != nil {
		api.WriteErr(w, ecode.Dependency, err.Error())
		return
	}
	outItems := make([]map[string]any, 0, len(items))
	for _, m := range items {
		outItems = append(outItems, map[string]any{
			"task_id":     fmt.Sprint(m["id"]),
			"session_id":  fmt.Sprint(m["session_id"]),
			"user_id":     fmt.Sprint(m["user_id"]),
			"topic":       fmt.Sprint(m["topic"]),
			"status":      fmt.Sprint(m["status"]),
			"last_update": anyToInt64(m["updated_at"]),
		})
	}
	api.WriteOK(w, map[string]any{
		"total":      total,
		"page":       page,
		"page_size":  ps,
		"items":      outItems,
	})
}

func anyToInt64(v any) int64 {
	switch x := v.(type) {
	case float64:
		return int64(x)
	case int64:
		return x
	case int:
		return int64(x)
	case json.Number:
		i, _ := x.Int64()
		return i
	default:
		return 0
	}
}

func (s *Server) pptExport(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TaskID string `json:"task_id"`
		Format string `json:"format"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		api.WriteErr(w, ecode.Param, api.InvalidJSONMessage(err))
		return
	}
	t := s.getTask(r.Context(), body.TaskID)
	if t == nil {
		api.WriteErr(w, ecode.NotFound, "task_id 不存在")
		return
	}
	if s.assertTaskAccess(w, r, t) {
		return
	}
	fmtNorm := strings.TrimSpace(strings.ToLower(body.Format))
	if fmtNorm != "pptx" && fmtNorm != "md" && fmtNorm != "html5" && fmtNorm != "docx" {
		api.WriteErr(w, ecode.Param, "format 仅支持 pptx/docx/md/html5")
		return
	}
	eid := task.NewExportID()
	exp := &task.Export{
		ExportID: eid, TaskID: t.TaskID, Format: fmtNorm,
		Status: "generating", LastUpdate: task.UTCMS(),
	}
	s.Store.SetExport(exp)
	s.persistExport(r.Context(), exp)

	go s.runExportBackground(eid, t.TaskID, fmtNorm)

	api.WriteOK(w, map[string]any{
		"export_id": eid, "status": "generating", "estimated_seconds": 30,
	})
}

func (s *Server) persistExport(ctx context.Context, e *task.Export) {
	if s.DB == nil {
		return
	}
	_ = s.DB.SaveExport(ctx, map[string]any{
		"export_id":    e.ExportID,
		"task_id":      e.TaskID,
		"format":       e.Format,
		"status":       e.Status,
		"download_url": e.DownloadURL,
		"file_size":    e.FileSize,
		"last_update":  e.LastUpdate,
	})
}

func (s *Server) runExportBackground(eid, tid, format string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	exp := s.Store.GetExport(eid)
	if exp == nil {
		return
	}
	t := s.getTask(ctx, tid)
	if t == nil {
		exp.Status = "failed"
		exp.LastUpdate = task.UTCMS()
		s.persistExport(ctx, exp)
		return
	}
	exp.Status = "generating"
	exp.LastUpdate = task.UTCMS()
	s.persistExport(ctx, exp)

	// 与 Python 一致：导出前等待任务生成结束（轮询内存/DB 任务状态）。
	waitCtx, waitCancel := context.WithTimeout(ctx, 10*time.Minute)
	defer waitCancel()
	tick := time.NewTicker(400 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-waitCtx.Done():
			exp.Status = "failed"
			exp.LastUpdate = task.UTCMS()
			s.persistExport(ctx, exp)
			return
		case <-tick.C:
			t = s.getTask(ctx, tid)
			if t == nil {
				exp.Status = "failed"
				exp.LastUpdate = task.UTCMS()
				s.persistExport(ctx, exp)
				return
			}
			switch t.Status {
			case "failed":
				exp.Status = "failed"
				exp.LastUpdate = task.UTCMS()
				s.persistExport(ctx, exp)
				return
			case "completed":
				goto doExport
			}
		}
	}

doExport:
	dir := filepath.Join(s.Store.TaskDir(tid), "exports")
	_ = os.MkdirAll(dir, 0o755)
	var outPath string
	switch format {
	case "pptx":
		if t.OutputPptxPath == "" {
			exp.Status = "failed"
			break
		}
		outPath = filepath.Join(dir, eid+".pptx")
		b, err := os.ReadFile(t.OutputPptxPath)
		if err != nil {
			exp.Status = "failed"
			break
		}
		_ = os.WriteFile(outPath, b, 0o644)
	case "html5":
		outPath = filepath.Join(dir, eid+".html")
		var sb strings.Builder
		sb.WriteString("<!DOCTYPE html>\n<html lang=\"zh-CN\"><head><meta charset=\"utf-8\"/><title>PPT Export</title></head><body>\n")
		for _, pid := range t.PageOrder {
			p := t.Pages[pid]
			if p == nil {
				continue
			}
			inner := slideutil.ExtractHTMLFromPy(p.PyCode)
			sb.WriteString(`<section class="slide" data-page-id="` + pid + `">` + inner + `</section>` + "\n")
		}
		sb.WriteString("</body></html>")
		_ = os.WriteFile(outPath, []byte(sb.String()), 0o644)
	case "md":
		outPath = filepath.Join(dir, eid+".md")
		md := exportmd.BuildLessonPlanMarkdown(t)
		_ = os.WriteFile(outPath, []byte(md), 0o644)
	case "docx":
		outPath = filepath.Join(dir, eid+".docx")
		md := exportmd.BuildLessonPlanMarkdown(t)
		if err := exportmd.WriteLessonPlanDOCX(outPath, md); err != nil {
			exp.Status = "failed"
			break
		}
	default:
		exp.Status = "failed"
	}
	if exp.Status != "failed" && outPath != "" {
		st, err := os.Stat(outPath)
		if err != nil {
			exp.Status = "failed"
		} else {
			exp.Status = "completed"
			exp.DownloadURL = "/static/runs/" + tid + "/exports/" + filepath.Base(outPath)
			exp.FileSize = st.Size()
		}
	}
	exp.LastUpdate = task.UTCMS()
	s.persistExport(ctx, exp)
	if exp.Status == "completed" {
		s.notifyPPTExportComplete(tid, format)
	}
}

func exportFromMap(m map[string]any) *task.Export {
	if m == nil {
		return nil
	}
	return &task.Export{
		ExportID:    fmt.Sprint(m["export_id"]),
		TaskID:      fmt.Sprint(m["task_id"]),
		Format:      fmt.Sprint(m["format"]),
		Status:      fmt.Sprint(m["status"]),
		DownloadURL: fmt.Sprint(m["download_url"]),
		FileSize:    anyToInt64(m["file_size"]),
		LastUpdate:  anyToInt64(m["last_update"]),
	}
}

func (s *Server) getExport(w http.ResponseWriter, r *http.Request) {
	eid := chi.URLParam(r, "exportID")
	exp := s.Store.GetExport(eid)
	if exp == nil && s.DB != nil {
		if m, _ := s.DB.LoadExport(r.Context(), eid); m != nil {
			exp = exportFromMap(m)
			if exp != nil {
				s.Store.SetExport(exp)
			}
		}
	}
	if exp == nil {
		api.WriteErr(w, ecode.NotFound, "export_id 不存在")
		return
	}
	t := s.getTask(r.Context(), exp.TaskID)
	if t == nil {
		api.WriteErr(w, ecode.NotFound, "task_id 不存在")
		return
	}
	if s.assertTaskAccess(w, r, t) {
		return
	}
	api.WriteOK(w, map[string]any{
		"export_id":    exp.ExportID,
		"status":       exp.Status,
		"download_url": redisx.PublicMediaURL(s.Cfg.PublicBaseURL, exp.DownloadURL),
		"format":       exp.Format,
		"file_size":    exp.FileSize,
	})
}

func (s *Server) pageRender(w http.ResponseWriter, r *http.Request) {
	tid := strings.TrimSpace(r.URL.Query().Get("task_id"))
	pid := chi.URLParam(r, "pageID")
	if tid == "" {
		api.WriteErr(w, ecode.Param, "缺少查询参数 task_id")
		return
	}
	t := s.getTask(r.Context(), tid)
	if t == nil {
		api.WriteErr(w, ecode.NotFound, "task_id 不存在")
		return
	}
	if s.assertTaskAccess(w, r, t) {
		return
	}
	p := t.Pages[pid]
	if p == nil {
		api.WriteErr(w, ecode.NotFound, "page_id 不存在")
		return
	}
	api.WriteOK(w, map[string]any{
		"page_id":    p.PageID,
		"task_id":    t.TaskID,
		"status":     p.Status,
		"render_url": redisx.PublicMediaURL(s.Cfg.PublicBaseURL, p.RenderURL),
		"py_code":    p.PyCode,
		"version":    p.Version,
		"updated_at": p.UpdatedAt,
	})
}

func (s *Server) renderExecute(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TaskID string `json:"task_id"`
		PageID string `json:"page_id"`
		PyCode string `json:"py_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		api.WriteErr(w, ecode.Param, api.InvalidJSONMessage(err))
		return
	}
	tid := strings.TrimSpace(body.TaskID)
	if tid == "" {
		api.WriteErr(w, ecode.Param, "task_id 不能为空")
		return
	}
	t := s.getTask(r.Context(), tid)
	if t == nil {
		api.WriteErr(w, ecode.NotFound, "task_id 不存在")
		return
	}
	if s.assertTaskAccess(w, r, t) {
		return
	}
	pid := strings.TrimSpace(body.PageID)
	if pid == "" || t.Pages[pid] == nil {
		api.WriteErr(w, ecode.NotFound, "page_id 不存在")
		return
	}
	p := t.Pages[pid]
	if strings.TrimSpace(body.PyCode) != "" {
		p.PyCode = body.PyCode
		p.Version++
		p.UpdatedAt = task.UTCMS()
	}
	inner, perr := slideutil.ExtractHTMLFromPyStrict(p.PyCode)
	if perr != nil {
		api.WriteErr(w, ecode.Param, "解析 py_code: "+perr.Error())
		return
	}
	dep, err := s.writeSlidePreview(r.Context(), t.TaskID, p.SlideIndex, inner, true)
	if err != nil {
		em := err.Error()
		if dep {
			api.WriteErr(w, ecode.LLMDependency, "渲染依赖不可用："+em)
			return
		}
		api.WriteErr(w, ecode.Internal, em)
		return
	}
	ru := fmt.Sprintf("/static/runs/%s/renders/slide_%04d.jpg", t.TaskID, p.SlideIndex)
	p.RenderURL = ru
	t.LastUpdate = task.UTCMS()
	_ = s.saveCanvasFromTask(r.Context(), t, "")
	s.persist(r.Context(), t)
	api.WriteOK(w, map[string]any{
		"success":    true,
		"error":      "",
		"render_url": redisx.PublicMediaURL(s.Cfg.PublicBaseURL, ru),
	})
}
