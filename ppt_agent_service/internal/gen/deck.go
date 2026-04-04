package gen

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"educationagent/ppt_agent_service_go/internal/canvas"
	"educationagent/ppt_agent_service_go/internal/deckgen"
	"educationagent/ppt_agent_service_go/internal/parseflow"
	"educationagent/ppt_agent_service_go/internal/redisx"
	"educationagent/ppt_agent_service_go/internal/slideutil"
	"educationagent/ppt_agent_service_go/internal/task"
	"educationagent/ppt_agent_service_go/internal/toolllm"
)

var minimalJPEG []byte

func init() {
	minimalJPEG, _ = base64.StdEncoding.DecodeString("/9j/4AAQSkZJRgABAQEASABIAAD/2wBDAP//////////////////////////////////////////////////////////////////////////////////////2wBDAf//////////////////////////////////////////////////////////////////////////////////////wAARCABAAEADASIAAhEBAxEB/8QAFwABAQEBAAAAAAAAAAAAAAAAAAIDBf/EABgBAQEBAQEAAAAAAAAAAAAAAAABAgME/8QAFBEBAAAAAAAAAAAAAAAAAAAAAP/aAAwDAQACEAMQAAAB0H//2Q==")
	if len(minimalJPEG) < 4 {
		minimalJPEG = []byte{0xff, 0xd8, 0xff, 0xd9}
	}
}

// MinimalJPEG 占位预览图字节（与全册生成一致）。
func MinimalJPEG() []byte { return append([]byte(nil), minimalJPEG...) }

type generatePresRequest struct {
	TaskDir          string          `json:"task_dir,omitempty"`
	UserID           string          `json:"user_id"`
	Topic            string          `json:"topic"`
	Description      string          `json:"description"`
	TotalPages       int             `json:"total_pages"`
	Audience         string          `json:"audience"`
	GlobalStyle      string          `json:"global_style"`
	SessionID        string          `json:"session_id"`
	TeachingElements json.RawMessage `json:"teaching_elements,omitempty"`
	ReferenceFiles   []map[string]any `json:"reference_files,omitempty"`
	ExtraContext     string           `json:"extra_context,omitempty"`
	RetrievalTrace   json.RawMessage  `json:"retrieval_trace,omitempty"`
	ContextInjections []map[string]any `json:"context_injections,omitempty"`
}

type generatePresResponse struct {
	OK              bool     `json:"ok"`
	SlideHTML       []string `json:"slide_html"`
	PPTXPath        string   `json:"pptx_path"`
	AppliedPPTXPath string   `json:"applied_pptx_path"`
	Error           string   `json:"error"`
}

type envelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

type kbQueryRequest struct {
	CollectionID   string            `json:"collection_id,omitempty"`
	UserID         string            `json:"user_id"`
	Query          string            `json:"query"`
	TopK           int               `json:"top_k,omitempty"`
	ScoreThreshold float64           `json:"score_threshold,omitempty"`
	Filters        map[string]string `json:"filters,omitempty"`
}

type kbChunk struct {
	ChunkID  string  `json:"chunk_id"`
	DocID    string  `json:"doc_id"`
	DocTitle string  `json:"doc_title"`
	Content  string  `json:"content"`
	Score    float64 `json:"score"`
}

type kbQueryData struct {
	Chunks []kbChunk `json:"chunks"`
	Total  int       `json:"total"`
}

type webSearchRequest struct {
	RequestID  string `json:"request_id,omitempty"`
	UserID     string `json:"user_id"`
	Query      string `json:"query"`
	MaxResults int    `json:"max_results,omitempty"`
	Language   string `json:"language,omitempty"`
	SearchType string `json:"search_type,omitempty"`
}

type webSearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
	Source  string `json:"source"`
}

type webSearchData struct {
	RequestID string            `json:"request_id"`
	Status    string            `json:"status"`
	Results   []webSearchResult `json:"results"`
	Summary   string            `json:"summary"`
}

func decodeEnvelopeData(raw []byte, target any) error {
	var env envelope
	if err := json.Unmarshal(raw, &env); err == nil && env.Code != 0 {
		if env.Code != 200 {
			if strings.TrimSpace(env.Message) == "" {
				env.Message = fmt.Sprintf("remote code=%d", env.Code)
			}
			return fmt.Errorf("%s", env.Message)
		}
		if len(env.Data) == 0 || string(env.Data) == "null" {
			return nil
		}
		return json.Unmarshal(env.Data, target)
	}
	return json.Unmarshal(raw, target)
}

func postJSON(ctx context.Context, url string, reqBody any, out any) error {
	b, _ := json.Marshal(reqBody)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(url, "/"), strings.NewReader(string(b)))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var env envelope
		if err := json.Unmarshal(raw, &env); err == nil && strings.TrimSpace(env.Message) != "" {
			return fmt.Errorf("%s", env.Message)
		}
		return fmt.Errorf("http %d", resp.StatusCode)
	}
	return decodeEnvelopeData(raw, out)
}

func compactText(s string, maxRunes int) string {
	s = strings.TrimSpace(s)
	if s == "" || maxRunes <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	return string(r[:maxRunes]) + "…"
}

func buildRetrievalQuery(t *task.Task) string {
	parts := []string{
		strings.TrimSpace(t.Topic),
		strings.TrimSpace(t.Description),
	}
	if len(t.TeachingElements) > 0 && string(t.TeachingElements) != "null" {
		parts = append(parts, string(t.TeachingElements))
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func fetchRetrievalContext(ctx context.Context, t *task.Task) (string, json.RawMessage, []map[string]any) {
	kbURL := strings.TrimSpace(os.Getenv("KB_QUERY_URL"))
	wsURL := strings.TrimSpace(os.Getenv("WEB_SEARCH_QUERY_URL"))
	if kbURL == "" && wsURL == "" {
		return "", nil, nil
	}
	query := buildRetrievalQuery(t)
	if query == "" {
		return "", nil, nil
	}
	type trace struct {
		KB        map[string]any `json:"kb,omitempty"`
		WebSearch map[string]any `json:"web_search,omitempty"`
	}
	tr := trace{}
	injections := make([]map[string]any, 0, 2)
	var extraParts []string

	ctx2, cancel := context.WithTimeout(ctx, 18*time.Second)
	defer cancel()

	if kbURL != "" {
		var d kbQueryData
		err := postJSON(ctx2, kbURL, kbQueryRequest{
			UserID: t.UserID, Query: query, TopK: 5, ScoreThreshold: 0.45,
		}, &d)
		if err != nil {
			tr.KB = map[string]any{"ok": false, "error": err.Error()}
		} else {
			tr.KB = map[string]any{"ok": true, "total": d.Total}
			lines := make([]string, 0, len(d.Chunks))
			for i, c := range d.Chunks {
				if i >= 5 {
					break
				}
				title := strings.TrimSpace(c.DocTitle)
				if title == "" {
					title = c.DocID
				}
				lines = append(lines, fmt.Sprintf("- [%s] %s", title, compactText(c.Content, 220)))
			}
			if len(lines) > 0 {
				txt := "【本地知识库检索】\n" + strings.Join(lines, "\n")
				extraParts = append(extraParts, txt)
				injections = append(injections, map[string]any{
					"source":   "knowledge_base",
					"priority": "normal",
					"msg_type": "rag_chunks",
					"content":  txt,
				})
			}
		}
	}

	if wsURL != "" {
		var d webSearchData
		err := postJSON(ctx2, wsURL, webSearchRequest{
			UserID: t.UserID, Query: query, MaxResults: 5, Language: "zh", SearchType: "general",
		}, &d)
		if err != nil {
			tr.WebSearch = map[string]any{"ok": false, "error": err.Error()}
		} else {
			tr.WebSearch = map[string]any{"ok": true, "request_id": d.RequestID, "status": d.Status, "count": len(d.Results)}
			var lines []string
			if strings.TrimSpace(d.Summary) != "" {
				lines = append(lines, compactText(d.Summary, 800))
			}
			for i, it := range d.Results {
				if i >= 3 {
					break
				}
				lines = append(lines, fmt.Sprintf("- %s (%s)", strings.TrimSpace(it.Title), strings.TrimSpace(it.URL)))
			}
			if len(lines) > 0 {
				txt := "【Web 搜索补充】\n" + strings.Join(lines, "\n")
				extraParts = append(extraParts, txt)
				injections = append(injections, map[string]any{
					"source":   "web_search",
					"priority": "normal",
					"msg_type": "search_result",
					"content":  txt,
				})
			}
		}
	}

	traceRaw, _ := json.Marshal(tr)
	return strings.TrimSpace(strings.Join(extraParts, "\n\n")), traceRaw, injections
}

func kbParseURL() string {
	if v := strings.TrimSpace(os.Getenv("KB_PARSE_URL")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("KB_SERVICE_URL")); v != "" {
		return strings.TrimRight(v, "/") + "/api/v1/kb/parse"
	}
	return ""
}

func fetchReferenceParseContext(ctx context.Context, t *task.Task) string {
	u := kbParseURL()
	if u == "" || len(t.ReferenceFiles) == 0 {
		return ""
	}
	ctx2, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	lines := make([]string, 0, len(t.ReferenceFiles))
	for i, rf := range t.ReferenceFiles {
		if i >= 5 {
			break
		}
		furl := strings.TrimSpace(fmt.Sprint(rf["file_url"]))
		ft := parseflow.NormalizeFileType(fmt.Sprint(rf["file_type"]))
		fid := strings.TrimSpace(fmt.Sprint(rf["file_id"]))
		instr := strings.TrimSpace(fmt.Sprint(rf["instruction"]))
		if furl == "" || ft == "" || !parseflow.IsSupportedFileType(ft) {
			continue
		}
		in := parseflow.ParseInput{
			FileURL:  furl,
			FileType: ft,
			DocID:    fid,
			Config:   parseflow.DefaultChunkConfig(),
		}
		var doc parseflow.ParsedDocument
		if err := postJSON(ctx2, u, in, &doc); err != nil {
			continue
		}
		doc = parseflow.Rechunk(doc, in.Config)
		var samples []string
		for j, ch := range doc.TextChunks {
			if j >= 3 {
				break
			}
			samples = append(samples, compactText(ch.Content, 180))
		}
		if len(samples) == 0 && strings.TrimSpace(doc.Summary) != "" {
			samples = append(samples, compactText(doc.Summary, 220))
		}
		if len(samples) == 0 {
			continue
		}
		title := strings.TrimSpace(doc.Title)
		if title == "" {
			title = fid
		}
		seg := "【参考资料解析】\n" +
			"- title: " + title + "\n" +
			"- file_type: " + ft + "\n" +
			"- instruction: " + instr + "\n" +
			"- chunks: " + strings.Join(samples, " | ")
		lines = append(lines, seg)
	}
	return strings.TrimSpace(strings.Join(lines, "\n\n"))
}

func callGeneratePres(ctx context.Context, url string, req generatePresRequest) (*generatePresResponse, error) {
	b, _ := json.Marshal(req)
	r, _ := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(url, "/"), strings.NewReader(string(b)))
	r.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var env envelope
	if err := json.Unmarshal(raw, &env); err == nil && env.Code != 0 {
		var out generatePresResponse
		_ = json.Unmarshal(env.Data, &out)
		if env.Code != 200 {
			if strings.TrimSpace(out.Error) == "" {
				out.Error = strings.TrimSpace(env.Message)
			}
			if strings.TrimSpace(out.Error) == "" {
				out.Error = fmt.Sprintf("generate-pres code=%d", env.Code)
			}
			return &out, fmt.Errorf("%s", out.Error)
		}
		return &out, nil
	}
	var out generatePresResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if strings.TrimSpace(out.Error) == "" {
			out.Error = fmt.Sprintf("generate-pres http %d", resp.StatusCode)
		}
		return &out, fmt.Errorf("%s", out.Error)
	}
	return &out, nil
}

// RunDeck 调用 LLM 生成整册幻灯片，写入 renders、空 pptx，并更新内存中的 Task。
func RunDeck(ctx context.Context, llm *toolllm.Client, t *task.Task, store *task.Store, red *redisx.Store, onPersist func()) error {
	// 整册重跑前清空悬挂/冲突/合并队列（与 Python 一致）
	t.SuspendedPages = make(map[string]*task.SuspendedPage)
	t.PageMerges = make(map[string]*task.PageMerge)
	t.OpenConflictContexts = make(map[string]string)

	taskDir := store.TaskDir(t.TaskID)
	_ = os.MkdirAll(taskDir, 0o755)
	rendersDir := filepath.Join(taskDir, "renders")
	_ = os.MkdirAll(rendersDir, 0o755)

	var refExtra strings.Builder
	if tpl := strings.TrimSpace(os.Getenv("PPTAGENT_TEMPLATE")); tpl != "" {
		refExtra.WriteString("【PPT模板】")
		refExtra.WriteString(tpl)
		refExtra.WriteString("\n\n")
	}
	for _, rf := range t.ReferenceFiles {
		refExtra.WriteString(fmt.Sprint(rf))
		refExtra.WriteByte('\n')
	}
	// 若未预先注入检索结果，则由 PPT Agent 在生成前主动拉取 KB/WebSearch。
	if strings.TrimSpace(t.ExtraContext) == "" && (len(t.RetrievalTrace) == 0 || string(t.RetrievalTrace) == "null") && len(t.ContextInjections) == 0 {
		extra, trace, injections := fetchRetrievalContext(ctx, t)
		t.ExtraContext = strings.TrimSpace(extra)
		if len(trace) > 0 && string(trace) != "null" {
			t.RetrievalTrace = trace
		}
		if len(injections) > 0 {
			t.ContextInjections = injections
		}
		onPersist()
	}
	if refParsed := fetchReferenceParseContext(ctx, t); strings.TrimSpace(refParsed) != "" {
		if strings.TrimSpace(t.ExtraContext) != "" {
			t.ExtraContext += "\n\n"
		}
		t.ExtraContext += refParsed
		onPersist()
	}

	var slideHTML []string
	pptxOut := ""
	if presURL := strings.TrimSpace(os.Getenv("PPTAGENT_PRES_URL")); presURL != "" {
		presResp, err := callGeneratePres(ctx, presURL, generatePresRequest{
			TaskDir:          taskDir,
			UserID:           t.UserID,
			Topic:            t.Topic,
			Description:      t.Description,
			TotalPages:       t.TotalPages,
			Audience:         t.Audience,
			GlobalStyle:      t.GlobalStyle,
			SessionID:        t.SessionID,
			TeachingElements: t.TeachingElements,
			ReferenceFiles:   t.ReferenceFiles,
			ExtraContext:     t.ExtraContext,
			RetrievalTrace:   t.RetrievalTrace,
			ContextInjections: t.ContextInjections,
		})
		if err != nil || presResp == nil || !presResp.OK {
			t.Status = "failed"
			t.LastUpdate = task.UTCMS()
			onPersist()
			if red != nil && red.OK {
				_ = red.SaveCanvasDocument(ctx, t.TaskID, canvas.TaskToCanvasDocument(t, task.UTCMS(), ""))
			}
			if err != nil {
				return err
			}
			return fmt.Errorf("%s", presResp.Error)
		}
		slideHTML = presResp.SlideHTML
		if strings.TrimSpace(presResp.AppliedPPTXPath) != "" {
			pptxOut = strings.TrimSpace(presResp.AppliedPPTXPath)
		} else {
			pptxOut = strings.TrimSpace(presResp.PPTXPath)
		}
	} else {
		comp := toolllm.DeckCompleter{C: llm}
		req := deckgen.GenerateDeckRequest{
			Topic:            t.Topic,
			Description:      t.Description,
			TotalPages:       t.TotalPages,
			Audience:         t.Audience,
			GlobalStyle:      t.GlobalStyle,
			TeachingElements: t.TeachingElements,
			ExtraContext:     strings.TrimSpace(strings.Join([]string{
				strings.TrimSpace(refExtra.String()),
				strings.TrimSpace(t.ExtraContext),
				func() string {
					if len(t.ContextInjections) == 0 {
						return ""
					}
					b, _ := json.Marshal(t.ContextInjections)
					return "[context_injections]\n" + string(b)
				}(),
				func() string {
					if len(t.RetrievalTrace) == 0 || string(t.RetrievalTrace) == "null" {
						return ""
					}
					return "[retrieval_trace]\n" + string(t.RetrievalTrace)
				}(),
			}, "\n\n")),
		}
		resp, err := deckgen.Generate(ctx, comp, req)
		if err != nil || !resp.OK {
			t.Status = "failed"
			t.LastUpdate = task.UTCMS()
			onPersist()
			if red != nil && red.OK {
				_ = red.SaveCanvasDocument(ctx, t.TaskID, canvas.TaskToCanvasDocument(t, task.UTCMS(), ""))
			}
			if err != nil {
				return err
			}
			return fmt.Errorf("%s", resp.Error)
		}
		slideHTML = resp.SlideHTML
	}

	pages := make(map[string]*task.Page)
	order := make([]string, 0, len(slideHTML))
	now := task.UTCMS()
	for i, html := range slideHTML {
		pid := task.NewPageID()
		si := i + 1
		ru := fmt.Sprintf("/static/runs/%s/renders/slide_%04d.jpg", t.TaskID, si)
		pages[pid] = &task.Page{
			PageID:     pid,
			SlideIndex: si,
			Status:     "completed",
			RenderURL:  ru,
			PyCode:     slideutil.WrapSlideHTML(html, pid, si),
			Version:    t.Version,
			UpdatedAt:  now,
		}
		order = append(order, pid)
		_ = os.WriteFile(filepath.Join(rendersDir, fmt.Sprintf("slide_%04d.jpg", si)), MinimalJPEG(), 0o644)
	}

	pptxPath := pptxOut
	if strings.TrimSpace(pptxPath) == "" {
		pptxPath = filepath.Join(taskDir, "output.pptx")
		_ = os.WriteFile(pptxPath, []byte{}, 0o644)
	}

	t.Pages = pages
	t.PageOrder = order
	if len(order) > 0 {
		t.CurrentViewingPageID = order[0]
	} else {
		t.CurrentViewingPageID = ""
	}
	t.OutputPptxPath = pptxPath
	t.LastUpdate = task.UTCMS()
	t.Status = "completed"
	onPersist()
	if red != nil && red.OK {
		_ = red.SaveCanvasDocument(ctx, t.TaskID, canvas.TaskToCanvasDocument(t, task.UTCMS(), ""))
	}
	return nil
}
