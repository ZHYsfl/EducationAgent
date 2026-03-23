package canvas

import (
	"educationagent/ppt_agent_service_go/internal/task"
)

// TaskToCanvasDocument 对齐 Python task_to_canvas_document。
func TaskToCanvasDocument(t *task.Task, ts int64, pageStatusOverride string) map[string]any {
	pages := make(map[string]any)
	pageDisplay := make(map[string]any)
	for pid, p := range t.Pages {
		if p == nil {
			continue
		}
		st := p.Status
		if pageStatusOverride != "" {
			st = pageStatusOverride
		}
		pages[pid] = map[string]any{
			"page_id": p.PageID,
			"py_code": p.PyCode,
			"status":  st,
		}
		pageDisplay[pid] = map[string]any{
			"render_url":   p.RenderURL,
			"last_update":  p.UpdatedAt,
		}
	}
	return map[string]any{
		"task_id":                 t.TaskID,
		"timestamp":               ts,
		"page_order":              append([]string(nil), t.PageOrder...),
		"current_viewing_page_id": t.CurrentViewingPageID,
		"pages":                   pages,
		"page_display":            pageDisplay,
	}
}
