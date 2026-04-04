package payload

import (
	"encoding/json"
	"fmt"

	"educationagent/ppt_agent_service_go/internal/task"
)

type suspendedWire struct {
	PageID           string           `json:"page_id"`
	ContextID        string           `json:"context_id"`
	Reason           string           `json:"reason"`
	QuestionForUser  string           `json:"question_for_user"`
	SuspendedAt      int64            `json:"suspended_at"`
	LastAskedAt      int64            `json:"last_asked_at"`
	AskCount         int              `json:"ask_count"`
	PendingFeedbacks []map[string]any `json:"pending_feedbacks"`
}

type pageMergeWire struct {
	IsRunning          bool              `json:"is_running"`
	PendingIntents     []map[string]any  `json:"pending_intents"`
	ChainBaselinePages map[string]string `json:"chain_baseline_pages"`
	ChainVADTimestamp  int64             `json:"chain_vad_timestamp"`
}

// agentPayload 与 Python task_to_agent_payload 一致。
type agentPayload struct {
	Version              int                       `json:"version"`
	LastUpdate           int64                     `json:"last_update"`
	PageOrder            []string                  `json:"page_order"`
	CurrentViewingPageID string                    `json:"current_viewing_page_id"`
	Pages                map[string]pageWire       `json:"pages"`
	OutputPptxPath       *string                   `json:"output_pptx_path,omitempty"`
	ReferenceFiles       []map[string]any          `json:"reference_files"`
	TeachingElements     json.RawMessage           `json:"teaching_elements"`
	ExtraContext         string                    `json:"extra_context,omitempty"`
	RetrievalTrace       json.RawMessage           `json:"retrieval_trace,omitempty"`
	ContextInjections    []map[string]any          `json:"context_injections,omitempty"`
	PendingFeedbackLines []string                  `json:"pending_feedback_lines"`
	OpenConflictContexts map[string]string         `json:"open_conflict_contexts"`
	SuspendedPages       map[string]suspendedWire  `json:"suspended_pages"`
	PageMerges           map[string]pageMergeWire  `json:"page_merges"`
}

type pageWire struct {
	PageID     string `json:"page_id"`
	SlideIndex int    `json:"slide_index"`
	Status     string `json:"status"`
	RenderURL  string `json:"render_url"`
	PyCode     string `json:"py_code"`
	Version    int    `json:"version"`
	UpdatedAt  int64  `json:"updated_at"`
}

func TaskToSaveMap(t *task.Task) map[string]any {
	ap := agentPayload{
		Version:              t.Version,
		LastUpdate:           t.LastUpdate,
		PageOrder:            append([]string(nil), t.PageOrder...),
		CurrentViewingPageID: t.CurrentViewingPageID,
		Pages:                make(map[string]pageWire),
		ReferenceFiles:       t.ReferenceFiles,
		TeachingElements:     t.TeachingElements,
		ExtraContext:         t.ExtraContext,
		RetrievalTrace:       t.RetrievalTrace,
		ContextInjections:    t.ContextInjections,
		PendingFeedbackLines: append([]string(nil), t.PendingFeedbackLines...),
		OpenConflictContexts: map[string]string{},
		SuspendedPages:       make(map[string]suspendedWire),
		PageMerges:           make(map[string]pageMergeWire),
	}
	for k, v := range t.OpenConflictContexts {
		ap.OpenConflictContexts[k] = v
	}
	for pid, p := range t.Pages {
		if p == nil {
			continue
		}
		ap.Pages[pid] = pageWire{
			PageID: p.PageID, SlideIndex: p.SlideIndex, Status: p.Status,
			RenderURL: p.RenderURL, PyCode: p.PyCode, Version: p.Version, UpdatedAt: p.UpdatedAt,
		}
	}
	for pid, sp := range t.SuspendedPages {
		if sp == nil {
			continue
		}
		ap.SuspendedPages[pid] = suspendedWire{
			PageID: sp.PageID, ContextID: sp.ContextID, Reason: sp.Reason,
			QuestionForUser: sp.QuestionForUser, SuspendedAt: sp.SuspendedAt,
			LastAskedAt: sp.LastAskedAt, AskCount: sp.AskCount,
			PendingFeedbacks: append([]map[string]any(nil), sp.PendingFeedbacks...),
		}
	}
	for k, pm := range t.PageMerges {
		if pm == nil {
			continue
		}
		cb := map[string]string{}
		for a, b := range pm.ChainBaselinePages {
			cb[a] = b
		}
		ap.PageMerges[k] = pageMergeWire{
			IsRunning:          false,
			PendingIntents:     append([]map[string]any(nil), pm.PendingIntents...),
			ChainBaselinePages: cb,
			ChainVADTimestamp:  pm.ChainVADTimestamp,
		}
	}
	if t.OutputPptxPath != "" {
		s := t.OutputPptxPath
		ap.OutputPptxPath = &s
	}
	if len(ap.TeachingElements) == 0 {
		ap.TeachingElements = nil
	}
	return map[string]any{
		"id":            t.TaskID,
		"session_id":    t.SessionID,
		"user_id":       t.UserID,
		"topic":         t.Topic,
		"description":   t.Description,
		"total_pages":   t.TotalPages,
		"audience":      t.Audience,
		"global_style":  t.GlobalStyle,
		"status":        t.Status,
		"created_at":    0,
		"updated_at":    t.LastUpdate,
		"agent_payload": ap,
	}
}

func ApplyWire(t *task.Task, d map[string]any) error {
	t.TaskID = str(d["id"])
	t.SessionID = str(d["session_id"])
	t.UserID = str(d["user_id"])
	t.Topic = str(d["topic"])
	t.Description = str(d["description"])
	t.TotalPages = numToInt(d["total_pages"])
	t.Audience = str(d["audience"])
	t.GlobalStyle = str(d["global_style"])
	t.Status = str(d["status"])
	t.LastUpdate = int64(numToInt(d["updated_at"]))
	if t.LastUpdate == 0 {
		t.LastUpdate = task.UTCMS()
	}
	raw, _ := json.Marshal(d["agent_payload"])
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var ap agentPayload
	if err := json.Unmarshal(raw, &ap); err != nil {
		return err
	}
	t.Version = ap.Version
	if ap.LastUpdate > 0 {
		t.LastUpdate = ap.LastUpdate
	}
	t.PageOrder = append([]string(nil), ap.PageOrder...)
	t.CurrentViewingPageID = ap.CurrentViewingPageID
	t.Pages = make(map[string]*task.Page)
	for pid, pw := range ap.Pages {
		t.Pages[pid] = &task.Page{
			PageID: pw.PageID, SlideIndex: pw.SlideIndex, Status: pw.Status,
			RenderURL: pw.RenderURL, PyCode: pw.PyCode, Version: pw.Version, UpdatedAt: pw.UpdatedAt,
		}
	}
	if ap.OutputPptxPath != nil {
		t.OutputPptxPath = *ap.OutputPptxPath
	}
	t.ReferenceFiles = ap.ReferenceFiles
	t.TeachingElements = ap.TeachingElements
	t.ExtraContext = ap.ExtraContext
	t.RetrievalTrace = ap.RetrievalTrace
	t.ContextInjections = ap.ContextInjections
	t.PendingFeedbackLines = append([]string(nil), ap.PendingFeedbackLines...)
	t.OpenConflictContexts = make(map[string]string)
	for k, v := range ap.OpenConflictContexts {
		t.OpenConflictContexts[k] = v
	}
	t.SuspendedPages = make(map[string]*task.SuspendedPage)
	for pid, sw := range ap.SuspendedPages {
		t.SuspendedPages[pid] = &task.SuspendedPage{
			PageID: sw.PageID, ContextID: sw.ContextID, Reason: sw.Reason,
			QuestionForUser: sw.QuestionForUser, SuspendedAt: sw.SuspendedAt,
			LastAskedAt: sw.LastAskedAt, AskCount: sw.AskCount,
			PendingFeedbacks: append([]map[string]any(nil), sw.PendingFeedbacks...),
		}
	}
	t.PageMerges = make(map[string]*task.PageMerge)
	for k, mw := range ap.PageMerges {
		cb := map[string]string{}
		for a, b := range mw.ChainBaselinePages {
			cb[a] = b
		}
		t.PageMerges[k] = &task.PageMerge{
			PendingIntents:     append([]map[string]any(nil), mw.PendingIntents...),
			ChainBaselinePages: cb,
			ChainVADTimestamp:  mw.ChainVADTimestamp,
		}
	}
	return nil
}

func numToInt(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case int:
		return x
	case int64:
		return int(x)
	case json.Number:
		i, _ := x.Int64()
		return int(i)
	default:
		return 0
	}
}

func str(v any) string {
	if v == nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return x
	case fmt.Stringer:
		return x.String()
	default:
		return fmt.Sprint(x)
	}
}
