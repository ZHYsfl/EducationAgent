package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	openai "github.com/openai/openai-go/v3"
	toolcalling "tool_calling_go"

	"zcxppt/internal/infra/llm"
	"zcxppt/internal/infra/renderer"
	"zcxppt/internal/model"
	"zcxppt/internal/repository"
)

var (
	ErrInvalidReplyContext         = errors.New("reply_to_context_id is required for resolve_conflict")
	ErrContextNotMatched           = errors.New("reply_to_context_id does not match suspended context")
	ErrNoSuspendedConflict         = errors.New("resolve_conflict requires an active suspended conflict")
	ErrInvalidBatchGenerateRequest = errors.New("invalid batch generate request")
	ErrUnsupportedActionType       = errors.New("unsupported action_type")
	ErrTargetPageNotFound          = errors.New("target page not found")
)

type Notifier interface {
	SendPPTMessage(ctx context.Context, payload map[string]any) error
}

// agentCreator creates a tool-calling agent for page code generation.
// Injectable for testing.
type agentCreator func(cfg toolcalling.LLMConfig) agentInterface

// wrappedAgent wraps *toolcalling.Agent so it satisfies agentInterface.
type wrappedAgent struct {
	real *toolcalling.Agent
}

func (w *wrappedAgent) AddTool(t toolcalling.Tool) {
	w.real.AddTool(t)
}
func (w *wrappedAgent) Chat(ctx context.Context, msgs []openai.ChatCompletionMessageParamUnion) ([]openai.ChatCompletionMessageParamUnion, error) {
	return w.real.Chat(ctx, msgs)
}
func (w *wrappedAgent) ChatText(ctx context.Context, msgs []openai.ChatCompletionMessageParamUnion) (string, error) {
	// tool_calling.Agent doesn't have ChatText; use Chat and extract text.
	resp, err := w.real.Chat(ctx, msgs)
	if err != nil {
		return "", err
	}
	for i := len(resp) - 1; i >= 0; i-- {
		if resp[i].OfAssistant != nil && resp[i].OfAssistant.Content.OfString.Valid() {
			return resp[i].OfAssistant.Content.OfString.Value, nil
		}
	}
	return "", errors.New("no assistant message in response")
}

func defaultAgentCreator(cfg toolcalling.LLMConfig) agentInterface {
	return &wrappedAgent{real: toolcalling.NewAgent(cfg)}
}

// agentInterface is implemented by both *toolcalling.Agent (production) and *testMockAgent (tests).
type agentInterface interface {
	AddTool(t toolcalling.Tool)
	Chat(ctx context.Context, msgs []openai.ChatCompletionMessageParamUnion) ([]openai.ChatCompletionMessageParamUnion, error)
	ChatText(ctx context.Context, msgs []openai.ChatCompletionMessageParamUnion) (string, error)
}

type FeedbackService struct {
	pptRepo       repository.PPTRepository
	feedbackRepo  repository.FeedbackRepository
	llmRuntime    llm.ToolRuntime
	notify        Notifier
	renderer      *renderer.Renderer
	refFusion     *RefFusionService
	timeoutTickMu sync.Mutex
	newAgent      func(cfg toolcalling.LLMConfig) agentInterface
}

func NewFeedbackService(
	pptRepo repository.PPTRepository,
	feedbackRepo repository.FeedbackRepository,
	llmRuntime llm.ToolRuntime,
	notify Notifier,
) *FeedbackService {
	return &FeedbackService{
		pptRepo:      pptRepo,
		feedbackRepo: feedbackRepo,
		llmRuntime:   llmRuntime,
		notify:       notify,
		newAgent:     defaultAgentCreator,
	}
}

func (s *FeedbackService) AttachRenderer(r *renderer.Renderer) {
	s.renderer = r
}

func (s *FeedbackService) AttachRefFusionService(r *RefFusionService) {
	s.refFusion = r
}

func (s *FeedbackService) Handle(ctx context.Context, req model.FeedbackRequest) (model.FeedbackResponse, error) {
	// 1. 验证 resolve_conflict 必填字段
	resolveConflict := hasResolveConflictIntent(req.Intents)
	if resolveConflict && strings.TrimSpace(req.ReplyToContextID) == "" {
		return model.FeedbackResponse{}, ErrInvalidReplyContext
	}

	// 2. 先取页面数据（兜底校验 task_id/page_id 合法性）
	current, err := s.pptRepo.GetPageRender(req.TaskID, req.ViewingPageID)
	if err != nil {
		return model.FeedbackResponse{}, err
	}

	// 3. 检查该页面是否处于悬挂状态
	suspend, suspended, _ := s.feedbackRepo.GetSuspend(req.TaskID, req.ViewingPageID)
	if suspended && !suspend.Resolved {
		if resolveConflict {
			if strings.TrimSpace(req.ReplyToContextID) != suspend.ContextID {
				return model.FeedbackResponse{}, ErrContextNotMatched
			}
			_ = s.feedbackRepo.ResolveSuspend(req.TaskID, req.ViewingPageID)
			if pending, ok, _ := s.feedbackRepo.DequeuePending(req.TaskID, req.ViewingPageID); ok {
				_, _ = s.Handle(ctx, model.FeedbackRequest{
					TaskID:        pending.TaskID,
					ViewingPageID: pending.PageID,
					BaseTimestamp: pending.BaseTimestamp,
					RawText:       pending.RawText,
					Intents:       pending.Intents,
				})
			}
			return model.FeedbackResponse{AcceptedIntents: len(req.Intents), Queued: false}, nil
		}
		pending := model.PendingFeedback{
			TaskID:         req.TaskID,
			PageID:         req.ViewingPageID,
			BaseTimestamp:  req.BaseTimestamp,
			RawText:        req.RawText,
			Intents:        req.Intents,
			CreatedAt:      time.Now().UnixMilli(),
			ReferenceFiles: req.ReferenceFiles,
		}
		_ = s.feedbackRepo.EnqueuePending(req.TaskID, req.ViewingPageID, pending)
		_ = s.notify.SendPPTMessage(ctx, map[string]any{
			"task_id":    req.TaskID,
			"page_id":    req.ViewingPageID,
			"priority":   "high",
			"context_id": suspend.ContextID,
			"tts_text":   suspend.Question,
			"msg_type":   "conflict_question",
		})
		return model.FeedbackResponse{AcceptedIntents: len(req.Intents), Queued: true}, nil
	} else if resolveConflict {
		return model.FeedbackResponse{}, ErrNoSuspendedConflict
	}

	// 4. 处理其他意图
	acceptedCount := 0

	for _, intent := range req.Intents {
		action := strings.ToLower(strings.TrimSpace(intent.ActionType))
		targetID := strings.TrimSpace(intent.TargetPageID)

		switch action {
		case "modify":
			// 修改指定页面：复用上方已取出的 current（仅调用一次 LLM）
			pageID := targetID
			if pageID == "" {
				pageID = req.ViewingPageID
			}
			if pageID != req.ViewingPageID {
				current, err = s.pptRepo.GetPageRender(req.TaskID, pageID)
				if err != nil {
					continue
				}
			}

			// === 反馈阶段重融合：把参考资料再次拉入并重新融合 ===
			fbReq := model.FeedbackRequest{
				TaskID:           req.TaskID,
				ViewingPageID:     pageID,
				BaseTimestamp:     req.BaseTimestamp,
				RawText:           intent.Instruction,
				ReplyToContextID:  "",
				Intents:          []model.Intent{{ActionType: "modify", TargetPageID: pageID, Instruction: intent.Instruction}},
				ReferenceFiles:    req.ReferenceFiles,
			}
			if s.refFusion != nil && len(req.ReferenceFiles) > 0 {
				fusionResult, fusionErr := s.refFusion.FuseForFeedback(ctx, req.ReferenceFiles, intent.Instruction, pageID)
				if fusionErr == nil && fusionResult != nil {
					// 将融合结果序列化后注入 FeedbackRequest
					fbReq.RefFusionResult = &model.FusionResultPayload{
						ExtractedText: s.serializeFusionResult(fusionResult),
						StyleGuide:    s.serializeStyleGuide(&fusionResult.StyleGuide),
						TopicHints:    fusionResult.TopicHints,
					}
				}
			}

			mergeResult, err := s.llmRuntime.RunFeedbackLoop(ctx, fbReq, current)
			if err != nil {
				continue
			}
			_ = s.applyMergeResult(ctx, req.TaskID, pageID, mergeResult)
			acceptedCount++

		case "insert_before", "insert_after":
			// 插入新页面
			refPageID := targetID
			if refPageID == "" {
				refPageID = req.ViewingPageID
			}
			newPageCode, err := s.generateNewPageCode(ctx, req.TaskID, refPageID, intent.Instruction)
			if err != nil {
				continue
			}
			newPageID := "page_" + uuid.NewString()
			now := time.Now().UnixMilli()
			newPage := model.PageRenderResponse{
				PageID:    newPageID,
				TaskID:    req.TaskID,
				PyCode:    newPageCode,
				Status:    "completed",
				RenderURL: "",
				Version:   1,
				UpdatedAt: now,
			}
			if s.renderer != nil && newPageCode != "" {
				result, _ := s.renderer.Render(ctx, renderer.RenderRequest{
					PageIndex: 0,
					PageTitle: newPageID,
					PyCode:    newPageCode,
					RenderConfig: renderer.RenderConfig{
						WidthInches:  10,
						HeightInches: 7.5,
						BgColor:      "FFFFFF",
						FontName:     "Microsoft YaHei",
					},
				})
				if result.Success {
					newPage.RenderURL = result.RenderURL
				}
			}
			if action == "insert_after" {
				_ = s.pptRepo.InsertPageAfter(req.TaskID, refPageID, newPage)
			} else {
				_ = s.pptRepo.InsertPageBefore(req.TaskID, refPageID, newPage)
			}
			acceptedCount++

		case "delete":
			// 删除指定页面
			pageID := targetID
			if pageID == "" {
				pageID = req.ViewingPageID
			}
			if err := s.pptRepo.DeletePage(req.TaskID, pageID); err == nil {
				acceptedCount++
			}

		case "global_modify":
			canvas, err := s.pptRepo.GetCanvasStatus(req.TaskID)
			if err != nil {
				continue
			}
			for _, pageID := range canvas.PageOrder {
				current, err := s.pptRepo.GetPageRender(req.TaskID, pageID)
				if err != nil {
					continue
				}
				fbReq := model.FeedbackRequest{
					TaskID:           req.TaskID,
					ViewingPageID:    pageID,
					BaseTimestamp:    req.BaseTimestamp,
					RawText:          intent.Instruction,
					ReplyToContextID: "",
					Intents:          []model.Intent{{ActionType: "modify", TargetPageID: pageID, Instruction: intent.Instruction}},
				}
				mergeResult, err := s.llmRuntime.RunFeedbackLoop(ctx, fbReq, current)
				if err != nil {
					continue
				}
				_ = s.applyMergeResult(ctx, req.TaskID, pageID, mergeResult)
				acceptedCount++
			}

		case "reorder":
			if err := s.handleReorder(ctx, req.TaskID, intent.Instruction); err == nil {
				acceptedCount++
			}
		}
	}

	return model.FeedbackResponse{AcceptedIntents: acceptedCount, Queued: false}, nil
}

// applyMergeResult applies LLM merge result to a page: update code, render, save URL.
func (s *FeedbackService) applyMergeResult(ctx context.Context, taskID, pageID string, mergeResult model.MergeResult) error {
	if mergeResult.MergeStatus == "ask_human" {
		contextID := "ctx_" + uuid.NewString()
		suspend := model.SuspendState{
			TaskID:     taskID,
			PageID:     pageID,
			ContextID:  contextID,
			Question:   mergeResult.QuestionForUser,
			RetryCount: 0,
			CreatedAt:  time.Now().UnixMilli(),
			ExpiresAt:  time.Now().Add(45 * time.Second).UnixMilli(),
			Resolved:   false,
		}
		_ = s.feedbackRepo.SetSuspend(suspend)
		_ = s.notify.SendPPTMessage(ctx, map[string]any{
			"task_id":    taskID,
			"page_id":    pageID,
			"priority":   "high",
			"context_id": contextID,
			"tts_text":   mergeResult.QuestionForUser,
			"msg_type":   "conflict_question",
		})
		return nil
	}

	mergedCode := mergeResult.MergedPyCode
	current, err := s.pptRepo.GetPageRender(taskID, pageID)
	if err != nil {
		return err
	}

	_, err = s.pptRepo.UpdatePageCode(taskID, pageID, mergedCode, current.RenderURL)
	if err != nil {
		return err
	}

	if s.renderer != nil && mergedCode != "" {
		result, renderErr := s.renderer.Render(ctx, renderer.RenderRequest{
			PageIndex: 0,
			PageTitle: pageID,
			PyCode:    mergedCode,
			RenderConfig: renderer.RenderConfig{
				WidthInches:  10,
				HeightInches: 7.5,
				BgColor:      "FFFFFF",
				FontName:     "Microsoft YaHei",
			},
		})
		if renderErr == nil && result.Success {
			_, _ = s.pptRepo.UpdatePageCode(taskID, pageID, mergedCode, result.RenderURL)
		}
	}
	return nil
}

// generateNewPageCode uses LLM to generate a new page based on instruction.
func (s *FeedbackService) generateNewPageCode(ctx context.Context, taskID, refPageID, instruction string) (string, error) {
	refPage, err := s.pptRepo.GetPageRender(taskID, refPageID)
	if err != nil {
		return "", err
	}

	system := `你是资深教学PPT生成助手，根据用户的插入页面指令，生成新页面的 python-pptx 代码。
严格输出 JSON：{"py_code":"python代码"}
不要输出 JSON 以外的任何文字。

python-pptx 代码规范（必须严格遵循）：
- 幻灯片尺寸：宽10英寸，高7.5英寸
- 标题使用 set_slide_title(slide, "标题文字", font_size=36, color="FFFFFF", bg_color="1F4E79")
- 正文使用 add_textbox(slide, "内容", left, top, width, height, font_size=18, color="000000")
- 背景色块使用 add_rect(left, top, width, height, fill="颜色hex", line="none")
- 字体使用 "Microsoft YaHei"
- 不要导入任何外部图片
- prs/slide 等全局变量已准备好

示例：
add_rect(slide, 0, 0, 10, 1.2, fill="1F4E79")
set_slide_title(slide, "新页面标题", font_size=36, color="FFFFFF", bg_color="1F4E79")
add_textbox(slide, "页面内容在这里", 0.5, 1.5, 9, 1, font_size=20, color="333333")
`
	prompt := fmt.Sprintf("reference_page_code:\n%s\n\nuser_instruction:\n%s", refPage.PyCode, instruction)

	agent := s.agentForPageGen()
	msgs := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(system),
		openai.UserMessage(prompt),
	}
	resp, err := agent.ChatText(ctx, msgs)
	if err != nil {
		return "", err
	}

	// Parse JSON response
	var result struct {
		PyCode string `json:"py_code"`
	}
	if err := json.Unmarshal([]byte(resp), &result); err != nil {
		return "", fmt.Errorf("invalid json from llm: %w", err)
	}
	return result.PyCode, nil
}

// agentForPageGen returns the agent used for new-page code generation.
// Production returns real *toolcalling.Agent; tests can inject a mock.
func (s *FeedbackService) agentForPageGen() agentInterface {
	return s.newAgent(toolcalling.LLMConfig{
		APIKey:  llmConfigFromEnv(),
		BaseURL: "https://api.moonshot.cn/v1",
		Model:   "kimi-k2.5",
	})
}

func llmConfigFromEnv() string {
	// 从环境变量读取 LLM API Key
	// 这里简化处理，由外部传入
	return ""
}

// handleReorder handles page reordering instruction like "page_xxx→2" or "把page_abc移到第1页".
// Supported formats:
//   - "page_abc→2" (direct notation)
//   - "把page_abc移到第2页" (natural language)
func (s *FeedbackService) handleReorder(ctx context.Context, taskID, instruction string) error {
	instruction = strings.TrimSpace(instruction)
	var pageID string
	var targetPos int

	arrowIdx := strings.Index(instruction, "→")
	if arrowIdx >= 0 {
		pageID = strings.TrimSpace(instruction[:arrowIdx])
		posStr := strings.TrimSpace(instruction[arrowIdx+1:])
		fmt.Sscanf(posStr, "%d", &targetPos)
	} else {
		moveIdx := strings.Index(instruction, "移到")
		if moveIdx < 0 {
			return ErrUnsupportedActionType
		}
		before := strings.TrimSpace(instruction[:moveIdx])
		before = strings.TrimPrefix(before, "把")
		before = strings.TrimPrefix(before, "把")
		pageID = strings.TrimSpace(before)
		after := strings.TrimSpace(instruction[moveIdx+2:])
		// Remove "第X页" suffix if present
		after = strings.TrimPrefix(after, "第")
		idx := 0
		for idx < len(after) && after[idx] >= '0' && after[idx] <= '9' {
			idx++
		}
		if idx > 0 {
			fmt.Sscanf(after[:idx], "%d", &targetPos)
		}
	}

	// Adjust to 0-based index
	if targetPos > 0 {
		targetPos--
	}
	if targetPos < 0 {
		targetPos = 0
	}

	// Get the page data before deleting
	page, err := s.pptRepo.GetPageRender(taskID, pageID)
	if err != nil {
		return ErrTargetPageNotFound
	}

	// Delete from current position
	if err := s.pptRepo.DeletePage(taskID, pageID); err != nil {
		return err
	}

	// Re-insert at target position
	newPage := model.PageRenderResponse{
		PageID:    pageID,
		TaskID:    taskID,
		PyCode:    page.PyCode,
		RenderURL: page.RenderURL,
		Status:    page.Status,
		Version:   page.Version,
		UpdatedAt: time.Now().UnixMilli(),
	}

	cvs, err := s.pptRepo.GetCanvasStatus(taskID)
	if err != nil {
		return err
	}
	orderLen := len(cvs.PageOrder)
	if targetPos >= orderLen {
		targetPos = orderLen - 1
	}
	if targetPos < 0 {
		targetPos = 0
	}

	if orderLen == 0 {
		_ = s.pptRepo.InsertPageBefore(taskID, "", newPage)
	} else if targetPos == 0 {
		_ = s.pptRepo.InsertPageBefore(taskID, cvs.PageOrder[0], newPage)
	} else {
		_ = s.pptRepo.InsertPageAfter(taskID, cvs.PageOrder[targetPos-1], newPage)
	}
	return nil
}

// serializeFusionResult converts a FusionResult to a human-readable string for LLM injection.
func (s *FeedbackService) serializeFusionResult(fr *FusionResult) string {
	return FusionResultToPrompt(fr, 0, "")
}

// serializeStyleGuide converts a StyleGuide to a prompt-friendly string.
func (s *FeedbackService) serializeStyleGuide(sg *StyleGuide) string {
	if sg == nil {
		return ""
	}
	var parts []string
	if len(sg.ThemeColors) > 0 {
		parts = append(parts, "主色调: "+strings.Join(sg.ThemeColors, ", "))
	}
	if len(sg.Fonts) > 0 {
		parts = append(parts, "字体: "+strings.Join(sg.Fonts, ", "))
	}
	if len(sg.Layouts) > 0 {
		parts = append(parts, "版式: "+strings.Join(sg.Layouts, ", "))
	}
	return strings.Join(parts, "\n")
}

func hasResolveConflictIntent(intents []model.Intent) bool {
	for _, it := range intents {
		if strings.EqualFold(strings.TrimSpace(it.ActionType), "resolve_conflict") {
			return true
		}
	}
	return false
}

func (s *FeedbackService) ProcessTimeoutTick(ctx context.Context) error {
	s.timeoutTickMu.Lock()
	defer s.timeoutTickMu.Unlock()

	expired, err := s.feedbackRepo.ListExpiredSuspends(time.Now())
	if err != nil {
		return err
	}
	for _, item := range expired {
		if item.RetryCount < 3 {
			item.RetryCount++
			item.ExpiresAt = time.Now().Add(45 * time.Second).UnixMilli()
			_ = s.feedbackRepo.SetSuspend(item)
			_ = s.notify.SendPPTMessage(ctx, map[string]any{
				"task_id":    item.TaskID,
				"page_id":    item.PageID,
				"priority":   "high",
				"context_id": item.ContextID,
				"tts_text":   item.Question,
				"msg_type":   "conflict_question",
			})
			continue
		}

		_ = s.feedbackRepo.ResolveSuspend(item.TaskID, item.PageID)
		pending, ok, _ := s.feedbackRepo.DequeuePending(item.TaskID, item.PageID)
		if ok {
			_, _ = s.Handle(ctx, model.FeedbackRequest{
				TaskID:        pending.TaskID,
				ViewingPageID: pending.PageID,
				BaseTimestamp: pending.BaseTimestamp,
				RawText:       pending.RawText,
				Intents:       pending.Intents,
			})
		}
	}
	return nil
}

// GeneratePages runs merge+write for multiple pages concurrently.
// Current project does not have a real "renderer", so this method writes a mock render_url.
func (s *FeedbackService) GeneratePages(ctx context.Context, req model.BatchGeneratePagesRequest) (model.BatchGeneratePagesResponse, error) {
	taskID := strings.TrimSpace(req.TaskID)
	if taskID == "" || req.BaseTimestamp <= 0 || strings.TrimSpace(req.RawText) == "" {
		return model.BatchGeneratePagesResponse{}, ErrInvalidBatchGenerateRequest
	}

	var pageIDs []string
	if len(req.PageIDs) > 0 {
		pageIDs = make([]string, 0, len(req.PageIDs))
		for _, id := range req.PageIDs {
			id = strings.TrimSpace(id)
			if id != "" {
				pageIDs = append(pageIDs, id)
			}
		}
	} else {
		canvas, err := s.pptRepo.GetCanvasStatus(taskID)
		if err != nil {
			return model.BatchGeneratePagesResponse{}, err
		}
		pageIDs = canvas.PageOrder
	}
	if len(pageIDs) == 0 {
		return model.BatchGeneratePagesResponse{}, ErrInvalidBatchGenerateRequest
	}

	maxParallel := req.MaxParallel
	if maxParallel <= 0 {
		maxParallel = 4
	}
	if maxParallel > len(pageIDs) {
		maxParallel = len(pageIDs)
	}
	if maxParallel <= 0 {
		maxParallel = 1
	}

	results := make([]model.BatchGeneratePageResult, len(pageIDs))
	sem := make(chan struct{}, maxParallel)
	var wg sync.WaitGroup
	var mu sync.Mutex

	for i, pageID := range pageIDs {
		i := i
		pageID := pageID

		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			current, err := s.pptRepo.GetPageRender(taskID, pageID)
			if err != nil {
				mu.Lock()
				results[i] = model.BatchGeneratePageResult{
					PageID: pageID, Status: "failed", Error: err.Error(),
				}
				mu.Unlock()
				return
			}

			perPageIntents := make([]model.Intent, len(req.Intents))
			for j := range req.Intents {
				perPageIntents[j] = req.Intents[j]
				perPageIntents[j].TargetPageID = pageID
			}

			fbReq := model.FeedbackRequest{
				TaskID:           taskID,
				BaseTimestamp:    req.BaseTimestamp,
				ViewingPageID:    pageID,
				ReplyToContextID: "",
				RawText:          req.RawText,
				Intents:          perPageIntents,
				ReferenceFiles:    req.ReferenceFiles,
			}

			if s.refFusion != nil && len(req.ReferenceFiles) > 0 {
				fusionResult, fusionErr := s.refFusion.FuseForFeedback(ctx, req.ReferenceFiles, req.RawText, pageID)
				if fusionErr == nil && fusionResult != nil {
					fbReq.RefFusionResult = &model.FusionResultPayload{
						ExtractedText: s.serializeFusionResult(fusionResult),
						StyleGuide:    s.serializeStyleGuide(&fusionResult.StyleGuide),
						TopicHints:    fusionResult.TopicHints,
					}
				}
			}

			mergeResult, err := s.llmRuntime.RunFeedbackLoop(ctx, fbReq, current)
			if err != nil {
				mu.Lock()
				results[i] = model.BatchGeneratePageResult{
					PageID: pageID, Status: "failed", Error: err.Error(),
				}
				mu.Unlock()
				return
			}

			if mergeResult.MergeStatus != "auto_resolved" {
				mu.Lock()
				results[i] = model.BatchGeneratePageResult{
					PageID: pageID, Status: "failed", Error: mergeResult.QuestionForUser,
				}
				mu.Unlock()
				return
			}

			renderURL := strings.TrimSpace(current.RenderURL)
			if s.renderer != nil && mergeResult.MergedPyCode != "" {
				ctx2, cancel2 := context.WithTimeout(ctx, 2*time.Minute)
				result, renderErr := s.renderer.Render(ctx2, renderer.RenderRequest{
					PageIndex: i,
					PageTitle: pageID,
					PyCode:    mergeResult.MergedPyCode,
					RenderConfig: renderer.RenderConfig{
						WidthInches:  10,
						HeightInches: 7.5,
						BgColor:      "FFFFFF",
						FontName:     "Microsoft YaHei",
					},
				})
				cancel2()
				if renderErr == nil && result.Success {
					renderURL = result.RenderURL
				}
			}
			if renderURL == "" {
				renderURL = fmt.Sprintf("mock://render/%s/%s", taskID, pageID)
			}
			updated, err := s.pptRepo.UpdatePageCode(taskID, pageID, mergeResult.MergedPyCode, renderURL)
			if err != nil {
				mu.Lock()
				results[i] = model.BatchGeneratePageResult{
					PageID: pageID, Status: "failed", RenderURL: renderURL, Error: err.Error(),
				}
				mu.Unlock()
				return
			}

			mu.Lock()
			results[i] = model.BatchGeneratePageResult{
				PageID:    pageID,
				Status:    "completed",
				RenderURL: updated.RenderURL,
				Version:   updated.Version,
			}
			mu.Unlock()
		}()
	}

	wg.Wait()
	return model.BatchGeneratePagesResponse{
		TaskID:  taskID,
		Results: results,
	}, nil
}
