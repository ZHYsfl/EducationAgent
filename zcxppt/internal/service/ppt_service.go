package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	openai "github.com/openai/openai-go/v3"
	toolcalling "tool_calling_go"

	"zcxppt/internal/infra/renderer"
	"zcxppt/internal/model"
	"zcxppt/internal/repository"
)

var ErrInvalidVADRequest = errors.New("invalid vad event request")

type LLMClientConfig struct {
	APIKey     string
	Model      string
	BaseURL    string
	KBToolURL  string // KB service base URL; if empty, kb_query tool is not registered
}

type PPTService struct {
	taskRepo       repository.TaskRepository
	pptRepo        repository.PPTRepository
	feedback       repository.FeedbackRepository
	httpClient     *http.Client
	kbBaseURL      string
	llmConfig      LLMClientConfig
	renderer       *renderer.Renderer
	refFusion      *RefFusionService
	notify         Notifier

	// 联动服务：在 Init 时自动生成教案和内容多样性
	teachingPlanService     *TeachingPlanService
	contentDiversityService *ContentDiversityService

	// feedbackService 用于在 HandleVADEvent resolve 悬挂后处理 pending 反馈
	feedbackService *FeedbackService
}

func NewPPTService(taskRepo repository.TaskRepository, pptRepo repository.PPTRepository, feedbackRepo repository.FeedbackRepository) *PPTService {
	return &PPTService{
		taskRepo:   taskRepo,
		pptRepo:    pptRepo,
		feedback:   feedbackRepo,
		httpClient: &http.Client{Timeout: 20 * time.Second},
	}
}

func (s *PPTService) AttachRenderer(r *renderer.Renderer) {
	s.renderer = r
}

func (s *PPTService) AttachRefFusionService(r *RefFusionService) {
	s.refFusion = r
}

// AttachTeachingPlanService sets the TeachingPlanService for auto-generation on Init.
func (s *PPTService) AttachTeachingPlanService(ts *TeachingPlanService) {
	s.teachingPlanService = ts
}

// AttachContentDiversityService sets the ContentDiversityService for auto-generation on Init.
func (s *PPTService) AttachContentDiversityService(cds *ContentDiversityService) {
	s.contentDiversityService = cds
}

// AttachFeedbackService sets the FeedbackService for handling pending feedback on VAD resolve.
func (s *PPTService) AttachFeedbackService(fb *FeedbackService) {
	s.feedbackService = fb
}

// AttachNotifier sets the Notifier for sending status notifications to Voice Agent.
func (s *PPTService) AttachNotifier(n Notifier) {
	s.notify = n
}

func (s *PPTService) ConfigureInitGenerator(kbBaseURL string, llmCfg LLMClientConfig) {
	if s == nil {
		return
	}
	s.kbBaseURL = strings.TrimRight(strings.TrimSpace(kbBaseURL), "/")
	s.llmConfig = llmCfg
}

func (s *PPTService) canGenerateInitWithKB() bool {
	if s == nil {
		return false
	}
	return s.httpClient != nil && strings.TrimSpace(s.kbBaseURL) != "" &&
		strings.TrimSpace(s.llmConfig.APIKey) != "" &&
		strings.TrimSpace(s.llmConfig.Model) != "" &&
		strings.TrimSpace(s.llmConfig.BaseURL) != ""
}

func (s *PPTService) Init(req model.PPTInitRequest) (taskID string, apiStatus string, err error) {
	task, err := s.taskRepo.Create(model.Task{
		SessionID: strings.TrimSpace(req.SessionID),
		Topic:     strings.TrimSpace(req.Topic),
		Status:    "generating",
		Progress:  10,
	})
	if err != nil {
		return "", "", err
	}

	_, err = s.pptRepo.InitCanvas(task.TaskID, req.TotalPages)
	if err != nil {
		_, _ = s.taskRepo.UpdateStatus(task.TaskID, "failed", 0)
		return "", "", err
	}

	if s.canGenerateInitWithKB() {
		go s.runInitGeneration(req, task.TaskID)
		return task.TaskID, "processing", nil
	}

	_, err = s.taskRepo.UpdateStatus(task.TaskID, "completed", 100)
	if err != nil {
		return "", "", err
	}
	return task.TaskID, "completed", nil
}

func (s *PPTService) runInitGeneration(req model.PPTInitRequest, taskID string) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("ppt init generation panic: task_id=%s err=%v", taskID, r)
			_, _ = s.taskRepo.UpdateStatus(taskID, "failed", 0)
		}
	}()

	ctx := context.Background()
	kbSummary, kbErr := s.queryKBParse(ctx, req)
	if kbErr != nil {
		// KB 查询失败不阻断主流程，fallback 为空字符串，继续生成
		log.Printf("kb query failed (fallback): task_id=%s err=%v, continuing without KB summary", taskID, kbErr)
		kbSummary = ""
		// 通知 Voice Agent：KB 查询失败，但继续生成
		if s.notify != nil {
			_ = s.notify.SendPPTMessage(ctx, map[string]any{
				"task_id":    taskID,
				"event_type": "ppt_status",
				"status":     "generating",
				"progress":   5,
				"tts_text":   "知识库查询未成功，将基于您提供的信息生成课件",
			})
		}
	}

	var fusionResult *FusionResult
	if s.refFusion != nil && len(req.ReferenceFiles) > 0 {
		var fuseErr error
		fusionResult, fuseErr = s.refFusion.Fuse(ctx, req, 0, req.Topic)
		if fuseErr != nil {
			log.Printf("ref fusion failed (ignored): task_id=%s err=%v, continuing with KB content only", taskID, fuseErr)
			// 即使融合失败，也构造空结果以保证 prompt 结构完整（LLM 仍使用 KB 内容和教学要素生成）
			fusionResult = &FusionResult{}
		}
	}

	generatedPages, err := s.generateInitialPages(ctx, req, kbSummary, fusionResult)
	if err != nil {
		_, _ = s.taskRepo.UpdateStatus(taskID, "failed", 0)
		return
	}

	_, _ = s.taskRepo.UpdateStatus(taskID, "generating", 50)

	canvas, err := s.pptRepo.GetCanvasStatus(taskID)
	if err != nil {
		_, _ = s.taskRepo.UpdateStatus(taskID, "failed", 0)
		return
	}

	type pageResult struct {
		pageID    string
		pyCode    string
		pageTitle string
		renderURL string
		renderErr error
	}
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	results := make([]pageResult, len(canvas.PageOrder))
	for i, pageID := range canvas.PageOrder {
		if i >= len(generatedPages) {
			break
		}
		pyCode := strings.TrimSpace(generatedPages[i].PyCode)
		pageTitle := generatedPages[i].Title
		idx := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			url := fmt.Sprintf("mock://render/%s/%s", taskID, pageID)
			if s.renderer != nil && pyCode != "" {
				renderCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				defer cancel()
				result, err := s.renderer.Render(renderCtx, renderer.RenderRequest{
					PageIndex: idx,
					PageTitle: pageTitle,
					PyCode:    pyCode,
					RenderConfig: renderer.RenderConfig{
						WidthInches:  10,
						HeightInches: 7.5,
						BgColor:      "FFFFFF",
						FontName:     "Microsoft YaHei",
					},
				})
				if err == nil && result.Success {
					url = result.RenderURL
				}
			}
			results[idx] = pageResult{pageID: pageID, pyCode: pyCode, pageTitle: pageTitle, renderURL: url, renderErr: nil}
		}()
	}
	wg.Wait()

	for _, res := range results {
		if res.pageID == "" {
			continue
		}
		if _, err := s.pptRepo.UpdatePageCode(taskID, res.pageID, res.pyCode, res.renderURL); err != nil {
			_, _ = s.taskRepo.UpdateStatus(taskID, "failed", 0)
			return
		}
	}
	_, _ = s.taskRepo.UpdateStatus(taskID, "generating", 95)

	_, err = s.taskRepo.UpdateStatus(taskID, "completed", 100)
	if err != nil {
		_, _ = s.taskRepo.UpdateStatus(taskID, "failed", 0)
	}

	// ── 联动生成：教案 + 内容多样性 ──────────────────────────────────
	// 两个 goroutine 均与 PPT 生成解耦，并发执行
	if req.AutoGenerateTeachingPlan && s.teachingPlanService != nil {
		go func() {
			// 收集所有已渲染页面的 PyCode，提取文本内容用于生成教案
			var pageContents []model.PageContent
			for i, pageID := range canvas.PageOrder {
				page, err := s.pptRepo.GetPageRender(taskID, pageID)
				if err != nil || page.PyCode == "" {
					continue
				}
				pageContents = append(pageContents, model.PageContent{
					PageID:    pageID,
					PageIndex: i + 1,
					Title:     extractPageTitle(page.PyCode),
					BodyText:  extractPageBody(page.PyCode),
					PyCode:    page.PyCode,
				})
			}
			planReq := model.TeachingPlanRequest{
				TaskID:           taskID,
				Topic:            req.Topic,
				Subject:          req.Subject,
				Description:      req.Description,
				Audience:         req.Audience,
				Duration:         req.TeachingElements.Duration,
				TeachingElements: req.TeachingElements,
				PageContents:     pageContents, // 基于 PyCode 内容生成
			}
			planResp, planErr := s.teachingPlanService.Generate(context.Background(), planReq)
			if planErr != nil {
				log.Printf("auto teaching_plan generation failed: task_id=%s err=%v", taskID, planErr)
				return
			}
			_ = s.taskRepo.UpdatePlanID(taskID, planResp.PlanID)
			// 保存教案基线时间戳（用于后续三路合并）
			if tr, ok := s.taskRepo.(interface{ UpdatePlanBaseTimestamp(taskID string, ts int64) error }); ok {
				_ = tr.UpdatePlanBaseTimestamp(taskID, time.Now().UnixMilli())
			}
			log.Printf("auto teaching_plan generated: task_id=%s plan_id=%s", taskID, planResp.PlanID)
		}()
	}

	if req.AutoGenerateContentDiversity && s.contentDiversityService != nil {
		go func() {
			divReq := model.ContentDiversityRequest{
				TaskID:           taskID,
				Topic:            req.Topic,
				Subject:          req.Subject,
				KBSummary:        kbSummary,
				Type:             coalesce(req.ContentDiversityType, "both"),
				GameType:         coalesce(req.ContentDiversityGameType, "random"),
				AnimationStyle:   coalesce(req.ContentDiversityAnimationStyle, "all"),
			}
			divResp, divErr := s.contentDiversityService.Generate(context.Background(), divReq)
			if divErr != nil {
				log.Printf("auto content_diversity generation failed: task_id=%s err=%v", taskID, divErr)
				return
			}
			_ = s.taskRepo.UpdateContentResultID(taskID, divResp.ResultID)
			log.Printf("auto content_diversity generated: task_id=%s result_id=%s", taskID, divResp.ResultID)
		}()
	}
}

// coalesce returns the first non-empty string, or "" if none.
func coalesce(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

type kbParseRequest struct {
	Subject        string  `json:"subject,omitempty"`
	UserID         string  `json:"user_id"`
	Query          string  `json:"query"`
	TopK           int     `json:"top_k"`
	ScoreThreshold float64 `json:"score_threshold,omitempty"`
	CollectionID  string  `json:"collection_id,omitempty"`
}

type apiResponseEnvelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func (s *PPTService) queryKBParse(ctx context.Context, req model.PPTInitRequest) (string, error) {
	if s.httpClient == nil || s.kbBaseURL == "" {
		return "", errors.New("kb service is not configured")
	}

	query := strings.TrimSpace(req.Description)
	if query == "" {
		query = strings.TrimSpace(req.Topic)
	}
	if query == "" {
		return "", errors.New("kb parse query is empty")
	}

	payload := kbParseRequest{
		Subject:        strings.TrimSpace(req.Subject),
		UserID:         strings.TrimSpace(req.UserID),
		Query:          query,
		TopK:           8,
		ScoreThreshold: 0.4,
	}
	rawBody, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	endpoint := s.kbBaseURL + "/api/v1/kb/parse"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(rawBody))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("kb parse failed: status=%d body=%s", resp.StatusCode, string(respBody))
	}

	var env apiResponseEnvelope
	if err := json.Unmarshal(respBody, &env); err == nil && (env.Code != 0 || len(env.Data) > 0) {
		if env.Code != 0 && env.Code != 200 {
			return "", fmt.Errorf("kb parse failed: code=%d message=%s", env.Code, env.Message)
		}
		return extractSummaryFromKBData(env.Data), nil
	}

	return extractSummaryFromKBData(respBody), nil
}

func extractSummaryFromKBData(data []byte) string {
	if len(data) == 0 {
		return ""
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return strings.TrimSpace(string(data))
	}
	for _, key := range []string{"summary", "answer", "content", "text"} {
		if v, ok := m[key]; ok {
			if s := strings.TrimSpace(fmt.Sprintf("%v", v)); s != "" {
				return s
			}
		}
	}
	if docs, ok := m["documents"].([]any); ok {
		parts := make([]string, 0, len(docs))
		for _, d := range docs {
			parts = append(parts, strings.TrimSpace(fmt.Sprintf("%v", d)))
		}
		return strings.TrimSpace(strings.Join(parts, "\n"))
	}
	return strings.TrimSpace(string(data))
}

type generatedPPT struct {
	Pages []generatedPage `json:"pages"`
}

type generatedPage struct {
	Title     string `json:"title"`
	PyCode    string `json:"py_code"`
	RenderURL string `json:"render_url,omitempty"`
}

func (s *PPTService) generateInitialPages(ctx context.Context, req model.PPTInitRequest, kbSummary string, fusionResult *FusionResult) ([]generatedPage, error) {
	if strings.TrimSpace(s.llmConfig.APIKey) == "" || strings.TrimSpace(s.llmConfig.Model) == "" || strings.TrimSpace(s.llmConfig.BaseURL) == "" {
		return nil, errors.New("llm config is not complete")
	}

	kbToolURL := strings.TrimSpace(s.llmConfig.KBToolURL)
	kbToolDesc := ""
	if kbToolURL != "" {
		kbToolDesc = "\n\n[可用工具]\n当你需要查询具体知识、定义或事实时，可以调用 kb_query 工具随机调用知识库获取补充内容。工具参数：query（搜索词）、subject（学科，可选）、top_k（返回数量，默认5）。"
	}

	system := `你是资深教学PPT生成助手。请基于用户需求和知识库检索内容，直接生成可用于后续渲染的初版多页PPT代码。
严格输出 JSON 对象，格式：{"pages":[{"title":"页面标题","py_code":"python代码","render_url":""}]}
要求：
1) pages 数量应尽量等于 total_pages；
2) py_code 必须是可执行的 python-pptx 代码，生成单张幻灯片内容，完整可运行；` + kbToolDesc + `
3) 不要输出任何 JSON 之外的文字。

python-pptx 代码规范（必须严格遵循）：
- 幻灯片尺寸：宽10英寸，高7.5英寸
- 背景色默认白色 "FFFFFF"，可用 add_rect 画背景
- 标题使用 set_slide_title(slide, "标题文字", font_size=36, color="FFFFFF", bg_color="1F4E79")
- 正文文本使用 add_textbox(slide, "内容", left, top, width, height, font_size=18, color="000000")
- 数字编号使用 add_textbox(...)，左对齐，适当留白
- 可用 add_rect(left, top, width, height, fill="颜色hex", line="none") 画色块装饰
- 可用 add_oval(left, top, width, height, fill="颜色hex") 画圆形图标
- 字体名使用 "Microsoft YaHei" 或 "SimHei"
- 所有数值单位为英寸（inches）
- 不要导入任何外部图片
- 代码必须完整可执行，不能有语法错误

` + s.buildStyleGuideSection(fusionResult) + `

` + s.buildFusionPromptSection(fusionResult) + `

示例 py_code：
add_rect(slide, 0, 0, 10, 1.2, fill="1F4E79")
set_slide_title(slide, "欢迎来到课堂教学", font_size=36, color="FFFFFF", bg_color="1F4E79")
add_textbox(slide, "本次课程将学习以下内容：", 0.5, 1.5, 9, 0.5, font_size=20, bold=True, color="333333")
add_textbox(slide, "1. 什么是人工智能", 1.0, 2.2, 7, 0.5, font_size=16, color="555555")
add_textbox(slide, "2. 机器学习基础概念", 1.0, 2.8, 7, 0.5, font_size=16, color="555555")
add_textbox(slide, "3. 深度学习入门", 1.0, 3.4, 7, 0.5, font_size=16, color="555555")
`

	prompt := fmt.Sprintf("topic=%s\nsubject=%s\ndescription=%s\naudience=%s\nglobal_style=%s\ntotal_pages=%d\n%s\nknowledge_from_kb=\n%s",
		req.Topic,
		req.Subject,
		req.Description,
		req.Audience,
		req.GlobalStyle,
		req.TotalPages,
		formatTeachingElementsForPrompt(req.TeachingElements),
		kbSummary,
	)

	agent := toolcalling.NewAgent(toolcalling.LLMConfig{APIKey: s.llmConfig.APIKey, BaseURL: s.llmConfig.BaseURL, Model: s.llmConfig.Model})

	// 注册 KB 查询工具（LLM 可在需要时随机调用知识库）
	if kbToolURL != "" {
		kbTool := toolcalling.Tool{
			Name:        "kb_query",
			Description: "Query the Knowledge Base to retrieve relevant knowledge. Use this when you need factual knowledge, definitions, or domain-specific information. Returns structured KB results including summaries, source titles, and relevance scores.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "Search query in natural language, e.g. '什么是导数的几何意义'",
					},
					"subject": map[string]any{
						"type":        "string",
						"description": "Subject domain (optional), e.g. '数学', '物理', '化学'",
					},
					"top_k": map[string]any{
						"type":        "number",
						"description": "Max results to return (default 5, max 10)",
					},
				},
				"required": []any{"query"},
			},
			Function: s.buildKBToolFunc(kbToolURL),
		}
		agent.AddTool(kbTool)
	}

	msgs := []openai.ChatCompletionMessageParamUnion{openai.SystemMessage(system), openai.UserMessage(prompt)}
	resp, err := agent.Chat(ctx, msgs)
	if err != nil {
		return nil, err
	}

	// 从最后一条 assistant 消息提取 JSON
	lastText := ""
	for i := len(resp) - 1; i >= 0; i-- {
		if resp[i].OfAssistant != nil && resp[i].OfAssistant.Content.OfString.Valid() {
			lastText = resp[i].OfAssistant.Content.OfString.Value
			break
		}
	}
	if lastText == "" {
		return nil, errors.New("llm returned no assistant message")
	}

	var out generatedPPT
	// 尝试从文本中提取 JSON（去掉 markdown 代码块）
	cleaned := strings.TrimSpace(lastText)
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")
	cleaned = strings.TrimSpace(cleaned)
	if err := json.Unmarshal([]byte(cleaned), &out); err != nil {
		return nil, fmt.Errorf("llm output is not valid json: %w, raw: %s", err, lastText)
	}
	if len(out.Pages) == 0 {
		return nil, errors.New("llm returned empty pages")
	}
	return out.Pages, nil
}

// buildKBToolFunc 返回一个 KB 查询工具函数。
func (s *PPTService) buildKBToolFunc(kbBaseURL string) toolcalling.ToolFunc {
	client := &http.Client{Timeout: 15 * time.Second}
	return func(ctx context.Context, args map[string]any) (string, error) {
		query, _ := args["query"].(string)
		query = strings.TrimSpace(query)
		if query == "" {
			return "", fmt.Errorf("query must not be empty")
		}
		topK := 5
		if v, ok := args["top_k"].(float64); ok && int(v) > 0 {
			topK = int(v)
			if topK > 10 {
				topK = 10
			}
		}
		// Split query into keywords
		keywords := strings.FieldsFunc(query, func(r rune) bool {
			return r == ' ' || r == ',' || r == '，' || r == '、'
		})
		if len(keywords) == 0 {
			keywords = []string{query}
		}
		payload := map[string]any{
			"keywords":        keywords,
			"top_k":           topK,
			"score_threshold": 0.35,
		}
		body, err := json.Marshal(payload)
		if err != nil {
			return "", err
		}
		endpoint := strings.TrimSuffix(kbBaseURL, "/") + "/api/v1/kb/query-chunks"
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if err != nil {
			return "", err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		if resp.StatusCode >= 300 {
			return "", fmt.Errorf("kb returned %d: %s", resp.StatusCode, string(b))
		}
		var env struct {
			Code    int             `json:"code"`
			Message string          `json:"message"`
			Data    json.RawMessage `json:"data"`
		}
		if json.Unmarshal(b, &env) == nil && env.Data != nil {
			return string(env.Data), nil
		}
		return string(b), nil
	}
}

func (s *PPTService) GetCanvasStatus(taskID string) (model.CanvasStatusResponse, error) {
	status, err := s.pptRepo.GetCanvasStatus(strings.TrimSpace(taskID))
	if err != nil {
		return model.CanvasStatusResponse{}, err
	}
	for i := range status.PagesInfo {
		status.PagesInfo[i].Status = normalizePageStatus(status.PagesInfo[i].Status)
	}
	return status, nil
}

func (s *PPTService) GetPageRender(taskID, pageID string) (model.PageRenderResponse, error) {
	page, err := s.pptRepo.GetPageRender(strings.TrimSpace(taskID), strings.TrimSpace(pageID))
	if err != nil {
		return model.PageRenderResponse{}, err
	}
	page.Status = normalizePageStatus(page.Status)
	return page, nil
}

func (s *PPTService) HandleVADEvent(req model.VADEventRequest) error {
	taskID := strings.TrimSpace(req.TaskID)
	pageID := strings.TrimSpace(req.ViewingPageID)
	if taskID == "" || pageID == "" {
		return ErrInvalidVADRequest
	}
	if req.Timestamp <= 0 {
		return ErrInvalidVADRequest
	}
	if _, err := s.pptRepo.GetPageRender(taskID, pageID); err != nil {
		return err
	}
	if err := s.pptRepo.SetCurrentViewingPageID(taskID, pageID); err != nil {
		return err
	}
	_, suspended, err := s.feedback.GetSuspend(taskID, pageID)
	if err != nil {
		return err
	}
	if suspended {
		if err := s.feedback.ResolveSuspend(taskID, pageID); err != nil {
			return err
		}
		ctx := context.Background()
		// 用户开始说话了，悬挂的冲突页面被 resolve，通知 Voice Agent 重新推送冲突问题
		if s.notify != nil {
			_ = s.notify.SendPPTMessage(ctx, map[string]any{
				"task_id":    taskID,
				"page_id":    pageID,
				"priority":   "high",
				"event_type": "vad_resolved",
				"tts_text":   "好的，请说",
			})
		}
		// 处理因悬挂而排入队列的 pending 反馈
		if pending, ok, _ := s.feedback.DequeuePending(taskID, pageID); ok {
			if s.feedbackService != nil {
				fbReq := model.FeedbackRequest{
					TaskID:         pending.TaskID,
					ViewingPageID:  pending.PageID,
					BaseTimestamp:  pending.BaseTimestamp,
					RawText:        pending.RawText,
					Intents:        pending.Intents,
					ReferenceFiles: pending.ReferenceFiles,
				}
				_, _ = s.feedbackService.Handle(ctx, fbReq)
			}
		}
	}
	return nil
}

// buildStyleGuideSection returns the style guide section for the system prompt.
func (s *PPTService) buildStyleGuideSection(fr *FusionResult) string {
	if fr == nil || len(fr.StyleGuide.ThemeColors) == 0 && len(fr.StyleGuide.Fonts) == 0 && len(fr.StyleGuide.Layouts) == 0 {
		return ""
	}
	var parts []string
	if len(fr.StyleGuide.ThemeColors) > 0 {
		parts = append(parts, "主题颜色: "+strings.Join(fr.StyleGuide.ThemeColors, ", "))
	}
	if len(fr.StyleGuide.Fonts) > 0 {
		parts = append(parts, "推荐字体: "+strings.Join(fr.StyleGuide.Fonts, ", "))
	}
	if len(fr.StyleGuide.Layouts) > 0 {
		parts = append(parts, "参考版式: "+strings.Join(fr.StyleGuide.Layouts, ", "))
	}
	return "[样式指南]\n" + strings.Join(parts, "\n") + "\n请尽量遵循上述样式进行代码生成。\n"
}

// buildFusionPromptSection returns the extracted reference content section for the user prompt.
func (s *PPTService) buildFusionPromptSection(fr *FusionResult) string {
	if fr == nil {
		return ""
	}
	return FusionResultToPrompt(fr, 0, "")
}

func normalizePageStatus(status string) string {
	status = strings.TrimSpace(strings.ToLower(status))
	switch status {
	case "rendering", "completed", "failed", "suspended_for_human":
		return status
	default:
		return "rendering"
	}
}

func formatTeachingElementsForPrompt(te *model.InitTeachingElements) string {
	if te == nil {
		return "teaching_elements=(未提供，请尽量依据 topic/description 推断教学结构)"
	}
	b, err := json.Marshal(te)
	if err != nil {
		return "teaching_elements=(序列化失败)"
	}
	return "teaching_elements=" + string(b)
}

// extractPageTitle 从 python-pptx 代码中提取标题文本。
func extractPageTitle(pyCode string) string {
	lines := strings.Split(pyCode, "\n")
	var candidates []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.Contains(trimmed, "add_rect(") || strings.Contains(trimmed, "add_oval(") ||
			strings.Contains(trimmed, "slide.layout") || strings.Contains(trimmed, "prs.save") ||
			strings.Contains(trimmed, "from pptx") || strings.Contains(trimmed, "import ") ||
			strings.Contains(trimmed, "prs =") || strings.Contains(trimmed, "Presentation(") {
			continue
		}
		if strings.Contains(trimmed, "\"") || strings.Contains(trimmed, "'") {
			candidates = append(candidates, extractQuotedStrings(trimmed)...)
		}
	}
	if len(candidates) > 0 {
		return strings.Join(candidates[:min(len(candidates), 3)], " ")
	}
	return "未命名页面"
}

// extractPageBody 从 python-pptx 代码中提取正文文本（标题之后的内容）。
func extractPageBody(pyCode string) string {
	lines := strings.Split(pyCode, "\n")
	var candidates []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.Contains(trimmed, "add_rect(") || strings.Contains(trimmed, "add_oval(") ||
			strings.Contains(trimmed, "slide.layout") || strings.Contains(trimmed, "prs.save") ||
			strings.Contains(trimmed, "from pptx") || strings.Contains(trimmed, "import ") ||
			strings.Contains(trimmed, "prs =") || strings.Contains(trimmed, "Presentation(") {
			continue
		}
		if strings.Contains(trimmed, "\"") || strings.Contains(trimmed, "'") {
			candidates = append(candidates, extractQuotedStrings(trimmed)...)
		}
	}
	if len(candidates) > 3 {
		return strings.Join(candidates[3:], " ")
	}
	return ""
}

// extractQuotedStrings 提取一行中所有引号内的文本。
func extractQuotedStrings(line string) []string {
	var results []string
	rest := line
	for {
		idx1 := -1
		var delim byte = '"'
		for i := 0; i < len(rest); i++ {
			if rest[i] == '"' || rest[i] == '\'' {
				idx1 = i
				delim = rest[i]
				break
			}
		}
		if idx1 < 0 {
			break
		}
		rest = rest[idx1+1:]
		idx2 := -1
		for i := 0; i < len(rest); i++ {
			if rest[i] == delim {
				idx2 = i
				break
			}
		}
		if idx2 < 0 {
			break
		}
		text := strings.TrimSpace(rest[:idx2])
		if text != "" && len(text) > 1 {
			results = append(results, text)
		}
		rest = rest[idx2+1:]
	}
	return results
}
