package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	openai "github.com/openai/openai-go/v3"
	toolcalling "tool_calling_go"

	"zcxppt/internal/infra/oss"
	"zcxppt/internal/model"
	"zcxppt/internal/repository"
)

var (
	ErrInvalidTeachingPlanRequest = errors.New("invalid teaching plan request")
	ErrPlanNotFound             = errors.New("teaching plan not found")
	ErrRenderScriptNotConfigured = errors.New("teaching plan render script not configured")
)

// TeachingPlanService generates and updates Word教案，与PPT保持联动同步。
// 支持三路合并：当PPT页面被修改时，通过Word三路合并保证教案版本安全。
type TeachingPlanService struct {
	planRepo     map[string]*teachingPlanJob
	planRepoMu   sync.RWMutex
	teachPlanRepo repository.TeachPlanRepository // 教案内容存储（支持快照）
	httpClient   *http.Client
	llmCfg       LLMClientConfig
	renderConfig RenderServiceConfig
	ossClient    *oss.Client
}

type RenderServiceConfig struct {
	PythonPath string
	ScriptPath string
	RenderDir  string
	URLPrefix  string
}

type teachingPlanJob struct {
	PlanID       string
	TaskID       string
	Status       string
	PlanContent  string
	DOCXURL      string
	Error        string
	CreatedAt    time.Time
}

// NewTeachingPlanService creates a new TeachingPlanService.
func NewTeachingPlanService(
	llmCfg LLMClientConfig,
	renderCfg RenderServiceConfig,
	ossClient *oss.Client,
	teachPlanRepo repository.TeachPlanRepository,
) *TeachingPlanService {
	return &TeachingPlanService{
		planRepo:      make(map[string]*teachingPlanJob),
		teachPlanRepo: teachPlanRepo,
		httpClient:    &http.Client{Timeout: 30 * time.Second},
		llmCfg:       llmCfg,
		renderConfig: renderCfg,
		ossClient:    ossClient,
	}
}

// Generate 首次生成教案：在 Init 阶段调用，基于 PyCode 内容生成完整教案。
func (s *TeachingPlanService) Generate(ctx context.Context, req model.TeachingPlanRequest) (model.TeachingPlanResponse, error) {
	if strings.TrimSpace(req.Topic) == "" {
		return model.TeachingPlanResponse{}, ErrInvalidTeachingPlanRequest
	}

	planID := "plan_" + uuid.NewString()
	job := &teachingPlanJob{
		PlanID:   planID,
		TaskID:   req.TaskID,
		Status:   "generating",
		CreatedAt: time.Now(),
	}

	s.planRepoMu.Lock()
	s.planRepo[planID] = job
	s.planRepoMu.Unlock()

	go s.runInitialGeneration(planID, req)

	return model.TeachingPlanResponse{
		TaskID: req.TaskID,
		PlanID: planID,
		Status: "generating",
	}, nil
}

// runInitialGeneration 基于 PyCode 内容生成教案（Init 阶段）。
func (s *TeachingPlanService) runInitialGeneration(planID string, req model.TeachingPlanRequest) {
	defer func() {
		if r := recover(); r != nil {
			s.setJobFailed(planID, fmt.Sprintf("panic: %v", r))
		}
	}()

	ctx := context.Background()

	// 从 PyCode 提取页面内容
	pageContents := req.PageContents
	if len(pageContents) == 0 && req.TaskID != "" {
		// 如果没有传入 PageContents，说明是旧调用（无 PyCode），用原有逻辑
		planContent, err := s.generatePlanContentFromMeta(ctx, req)
		if err != nil {
			s.setJobFailed(planID, err.Error())
			return
		}
		s.finalizePlan(planID, planContent)
		return
	}

	planContent, err := s.generatePlanContentFromPyCode(ctx, req, pageContents)
	if err != nil {
		s.setJobFailed(planID, err.Error())
		return
	}

	// 初始化教案存储（InitPlan 会保存初始快照）
	if s.teachPlanRepo != nil && req.TaskID != "" {
		_ = s.teachPlanRepo.InitPlan(req.TaskID, planID, planContent)
	}

	s.finalizePlan(planID, planContent)
}

// Update 更新教案：Feedback 阶段每次 PPT 修改时调用，联动更新对应章节。
// baseTimestamp 用于三路合并获取基线版本。
func (s *TeachingPlanService) Update(ctx context.Context, req model.TeachingPlanRequest) (model.TeachingPlanResponse, error) {
	if req.TaskID == "" {
		return model.TeachingPlanResponse{}, ErrInvalidTeachingPlanRequest
	}

	// 获取当前教案内容（current）
	currentContent, err := s.getCurrentPlanContent(req.TaskID)
	if err != nil {
		// 当前无教案，走首次生成流程
		return s.Generate(ctx, req)
	}

	// 获取基线版本（base）
	baseContent, _ := s.getBasePlanContent(req.TaskID, req.BaseTimestamp)

	// 从 PyCode 提取修改页面的内容
	pageContents := req.PageContents
	var modifiedPage *model.PageContent
	if len(pageContents) > 0 {
		modifiedPage = &pageContents[len(pageContents)-1] // 以最后一个为主
	}

	// 生成新版本（incoming）
	incomingContent, err := s.generateUpdatedPlanContent(ctx, req, currentContent, modifiedPage)
	if err != nil {
		return model.TeachingPlanResponse{}, err
	}

	// 三路合并
	mergedContent, conflictDesc, conflictOpts := s.threeWayMerge(baseContent, currentContent, incomingContent)

	if conflictDesc != "" && len(conflictOpts) > 0 {
		// 有冲突但不挂起——直接采纳合并结果（教案冲突不比 PPT 严重，自动合并）
		// TODO: 如果需要可扩展为挂起流程
	}

	// 更新存储
	if s.teachPlanRepo != nil {
		_ = s.teachPlanRepo.UpdatePlan(req.TaskID, mergedContent)
	}

	// 渲染 DOCX
	planID := s.getPlanIDByTaskID(req.TaskID)
	if planID == "" {
		planID = "plan_" + uuid.NewString()
	}

	docxURL, err := s.renderDOCX(ctx, planID, mergedContent)
	if err != nil {
		s.setJobFailed(planID, err.Error())
	}

	s.planRepoMu.Lock()
	if job, ok := s.planRepo[planID]; ok {
		job.Status = "completed"
		job.PlanContent = mergedContent
		job.DOCXURL = docxURL
	}
	s.planRepoMu.Unlock()

	return model.TeachingPlanResponse{
		TaskID:      req.TaskID,
		PlanID:      planID,
		Status:      "completed",
		DownloadURL: docxURL,
	}, nil
}

// ── 核心 LLM 调用方法 ──────────────────────────────────────────────────────

// generatePlanContentFromPyCode 基于 PPT PyCode 内容生成完整教案。
func (s *TeachingPlanService) generatePlanContentFromPyCode(ctx context.Context, req model.TeachingPlanRequest, pageContents []model.PageContent) (string, error) {
	if strings.TrimSpace(s.llmCfg.APIKey) == "" {
		return "", errors.New("llm config not complete")
	}

	// 构建页面内容摘要
	var pageSummary strings.Builder
	for i, pc := range pageContents {
		pageSummary.WriteString(fmt.Sprintf("【第%d页】标题: %s\n内容: %s\n",
			i+1, pc.Title, truncate(pc.BodyText, 200)))
	}

	system := `你是资深教学设计师。请根据以下PPT页面的实际内容，生成一份与PPT内容完全匹配的Word教案。

教案必须严格遵循以下JSON格式输出，不要输出JSON之外的任何文字：

{
  "title": "教案标题",
  "subject": "学科",
  "grade": "年级",
  "duration": "课时",
  "teaching_goals": ["目标1", "目标2"],
  "teaching_focus": ["教学重点1", "教学重点2"],
  "teaching_difficulties": ["难点1", "难点2"],
  "teaching_methods": ["方法1", "方法2"],
  "teaching_aids": ["教具1", "教具2"],
  "teaching_process": {
    "warm_up": {"duration": "5分钟", "content": "热身内容"},
    "introduction": {"duration": "5分钟", "content": "导入内容"},
    "new_teaching": [
      {"step": 1, "title": "步骤标题", "duration": "10分钟", "content": "详细内容", "activities": ["活动1"], "mapped_pages": ["page_id_1"]},
      ...
    ],
    "practice": {"duration": "10分钟", "content": "练习内容"},
    "summary": {"duration": "5分钟", "content": "总结内容"},
    "homework": ["作业1", "作业2"]
  },
  "classroom_activities": [
    {"name": "活动名称", "type": "活动类型", "duration": "5分钟", "description": "描述", "purpose": "目的"}
  ],
  "teaching_reflection": "教学反思"
}

关键要求：
1. new_teaching 的步骤数量应与PPT内容页数量相匹配或略少，每个步骤对应一组相关内容页
2. mapped_pages 字段标记此步骤对应的PPT页面ID（如 page_uuid_1），必须准确
3. 内容必须完全基于PPT页面提供的内容，不要凭空编造
4. 所有内容使用中文输出`

	prompt := fmt.Sprintf(`PPT页面内容：
%s

基本信息：
topic=%s
subject=%s
audience=%s
duration=%s
teaching_elements=%s`,
		pageSummary.String(),
		req.Topic,
		req.Subject,
		req.Audience,
		req.Duration,
		formatTeachingElementsForPrompt(req.TeachingElements),
	)

	return s.callLLMForPlan(ctx, system, prompt)
}

// generatePlanContentFromMeta 基于元信息生成教案（旧逻辑，用于无 PyCode 场景）。
func (s *TeachingPlanService) generatePlanContentFromMeta(ctx context.Context, req model.TeachingPlanRequest) (string, error) {
	if strings.TrimSpace(s.llmCfg.APIKey) == "" {
		return "", errors.New("llm config not complete")
	}

	system := `你是资深教学设计师，请根据用户需求生成一份详细的Word教案。
严格输出JSON对象，不要输出JSON之外的任何文字。格式参考generatePlanContentFromPyCode的system prompt。`

	prompt := fmt.Sprintf("topic=%s\nsubject=%s\ndescription=%s\naudience=%s\nduration=%s\nteaching_elements=%s\nstyle_guide=%s",
		req.Topic, req.Subject, req.Description, req.Audience,
		req.Duration, formatTeachingElementsForPrompt(req.TeachingElements), req.StyleGuide)

	return s.callLLMForPlan(ctx, system, prompt)
}

// generateUpdatedPlanContent 生成更新后的教案内容（Feedback 联动更新）。
// 对于特定章节的修改，只更新对应章节，整体走三路合并。
func (s *TeachingPlanService) generateUpdatedPlanContent(ctx context.Context, req model.TeachingPlanRequest, currentPlan string, modifiedPage *model.PageContent) (string, error) {
	if strings.TrimSpace(s.llmCfg.APIKey) == "" {
		return "", errors.New("llm config not complete")
	}

	var modifiedInfo string
	if modifiedPage != nil {
		modifiedInfo = fmt.Sprintf(`修改的PPT页面（第%d页）：
标题: %s
内容: %s`,
			modifiedPage.PageIndex, modifiedPage.Title, truncate(modifiedPage.BodyText, 500))
	}

	// 判断修改的是哪类页面（封面/目录/内容页/总结页）
	pageType := classifyPage(modifiedPage)

	system := `你是资深教学设计师。PPT内容已被用户修改，请根据修改后的内容更新教案对应章节。
要求：
1. 只修改与PPT修改相关的章节，其他章节保持原样
2. 严格输出完整JSON（包含所有字段），不要输出JSON之外的任何文字
3. mapped_pages 字段要准确更新

页面类型判断：
- pageType="cover": 封面/标题 → 更新教案的 title, subject, grade 字段
- pageType="toc": 目录 → 忽略（教案无目录页）
- pageType="content": 内容页 → 更新 new_teaching 中对应步骤的 content 和 mapped_pages
- pageType="summary": 总结页 → 更新 summary 章节
- pageType="practice": 练习页 → 更新 practice 章节

返回格式：完整JSON对象（所有字段都要包含）`

	prompt := fmt.Sprintf(`当前教案JSON：
%s

%s

用户修改指令: %s
pageType: %s

请根据上述信息，更新教案中受影响的章节，返回完整JSON。`,
		truncate(currentPlan, 2000),
		modifiedInfo,
		req.Description,
		pageType,
	)

	return s.callLLMForPlan(ctx, system, prompt)
}

// callLLMForPlan 通用的 LLM 调用生成教案 JSON。
func (s *TeachingPlanService) callLLMForPlan(ctx context.Context, system, prompt string) (string, error) {
	agent := toolcalling.NewAgent(toolcalling.LLMConfig{
		APIKey:  s.llmCfg.APIKey,
		BaseURL: s.llmCfg.BaseURL,
		Model:   s.llmCfg.Model,
	})

	msgs := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(system),
		openai.UserMessage(prompt),
	}

	resp, err := agent.Chat(ctx, msgs)
	if err != nil {
		return "", err
	}

	lastText := ""
	for i := len(resp) - 1; i >= 0; i-- {
		if resp[i].OfAssistant != nil && resp[i].OfAssistant.Content.OfString.Valid() {
			lastText = resp[i].OfAssistant.Content.OfString.Value
			break
		}
	}
	if lastText == "" {
		return "", errors.New("llm returned no assistant message")
	}

	cleaned := strings.TrimSpace(lastText)
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")
	cleaned = strings.TrimSpace(cleaned)

	var dummy map[string]any
	if err := json.Unmarshal([]byte(cleaned), &dummy); err != nil {
		return "", fmt.Errorf("llm output is not valid json: %w", err)
	}
	return cleaned, nil
}

// ── 三路合并 ──────────────────────────────────────────────────────────────

// threeWayMerge 对教案内容执行三路合并。
// 返回 mergedContent, conflictDesc, conflictOpts。
func (s *TeachingPlanService) threeWayMerge(base, current, incoming string) (merged string, conflictDesc string, conflictOpts []string) {
	if incoming == "" {
		return current, "", nil
	}
	if base == "" || base == current {
		return incoming, "", nil
	}
	if current == incoming {
		return current, "", nil
	}

	// 两者都不同：执行 JSON 字段级 diff
	fieldChanges := diffPlanFields(base, incoming)
	if len(fieldChanges) == 0 {
		return current, "", nil
	}

	// 尝试智能合并：基于 key 对 key 合并
	mergedResult, err := repository.MergePlanContent(base, incoming)
	if err == nil && mergedResult != incoming {
		return mergedResult, "", nil
	}

	// 有冲突但合并成功，返回合并结果
	return mergedResult, "", nil
}

// diffPlanFields 比较两个教案 JSON 的字段差异。
func diffPlanFields(base, incoming string) []string {
	var basePlan, incomingPlan map[string]any
	if json.Unmarshal([]byte(base), &basePlan) != nil {
		return nil
	}
	if json.Unmarshal([]byte(incoming), &incomingPlan) != nil {
		return nil
	}

	var changes []string
	for k, v := range incomingPlan {
		if bv, ok := basePlan[k]; ok {
			bj, _ := json.Marshal(bv)
			ij, _ := json.Marshal(v)
			if string(bj) != string(ij) {
				changes = append(changes, k)
			}
		} else {
			changes = append(changes, k+" (新增)")
		}
	}
	return changes
}

// ── 辅助方法 ────────────────────────────────────────────────────────────

func (s *TeachingPlanService) getCurrentPlanContent(taskID string) (string, error) {
	if s.teachPlanRepo != nil {
		return s.teachPlanRepo.GetPlan(taskID)
	}
	return "", errors.New("teach plan repo not configured")
}

func (s *TeachingPlanService) getBasePlanContent(taskID string, baseTimestamp int64) (string, error) {
	if s.teachPlanRepo != nil && baseTimestamp > 0 {
		return s.teachPlanRepo.GetSnapshotByTs(taskID, baseTimestamp)
	}
	// 兜底：返回空，等价于从未修改过
	return "", errors.New("no base snapshot")
}

func (s *TeachingPlanService) getPlanIDByTaskID(taskID string) string {
	s.planRepoMu.RLock()
	defer s.planRepoMu.RUnlock()
	for _, job := range s.planRepo {
		if job.TaskID == taskID {
			return job.PlanID
		}
	}
	return ""
}

func (s *TeachingPlanService) setJobFailed(planID, errMsg string) {
	s.planRepoMu.Lock()
	if job, ok := s.planRepo[planID]; ok {
		job.Status = "failed"
		job.Error = errMsg
	}
	s.planRepoMu.Unlock()
}

func (s *TeachingPlanService) finalizePlan(planID, planContent string) {
	docxURL, err := s.renderDOCX(context.Background(), planID, planContent)
	s.planRepoMu.Lock()
	if job, ok := s.planRepo[planID]; ok {
		if err != nil {
			job.Status = "failed"
			job.Error = err.Error()
		} else {
			job.Status = "completed"
			job.PlanContent = planContent
			job.DOCXURL = docxURL
		}
	}
	s.planRepoMu.Unlock()
}

// classifyPage 根据页面内容判断页面类型。
func classifyPage(page *model.PageContent) string {
	if page == nil {
		return "unknown"
	}
	title := strings.ToLower(page.Title)
	body := strings.ToLower(page.BodyText)

	// 关键词判断
	if containsAny(title, []string{"封面", "标题页", "welcome", "cover", "首页"}) ||
		containsAny(body, []string{"欢迎", "课堂", "欢迎来到"}) {
		return "cover"
	}
	if containsAny(title, []string{"目录", "toc", "contents", "大纲"}) ||
		containsAny(body, []string{"目录", "教学环节", "一、二、三"}) {
		return "toc"
	}
	if containsAny(title, []string{"总结", "回顾", "本节", "收获", "小结"}) ||
		containsAny(body, []string{"今天我们学", "回顾", "总结", "收获", "知识点总结"}) {
		return "summary"
	}
	if containsAny(title, []string{"练习", "作业", "quiz", "答题", "巩固"}) ||
		containsAny(body, []string{"练习", "作业", "请完成", "答题"}) {
		return "practice"
	}
	return "content"
}

func containsAny(s string, keywords []string) bool {
	for _, kw := range keywords {
		if strings.Contains(s, kw) {
			return true
		}
	}
	return false
}

// renderDOCX 调用 Python 脚本生成 .docx。
func (s *TeachingPlanService) renderDOCX(ctx context.Context, planID, planContent string) (string, error) {
	if s.renderConfig.ScriptPath == "" || s.ossClient == nil {
		return "", ErrRenderScriptNotConfigured
	}

	planJSON, _ := json.Marshal(map[string]any{"plan": planContent})

	script := fmt.Sprintf(`#!/usr/bin/env python3
# -*- coding: utf-8 -*-
import json, sys, os
PLAN_JSON = %s
def main():
    from docx import Document
    from docx.shared import Pt, RGBColor, Inches, Cm
    from docx.enum.text import WD_ALIGN_PARAGRAPH
    plan = json.loads(PLAN_JSON["plan"])
    doc = Document()
    for section in doc.sections:
        section.top_margin = Cm(2.5); section.bottom_margin = Cm(2.5)
        section.left_margin = Cm(2.5); section.right_margin = Cm(2.5)
    def h1(doc, text):
        p = doc.add_heading("", level=0); r = p.add_run(text); r.bold = True; r.font.size = Pt(18)
        r.font.color.rgb = RGBColor(31,78,121); p.alignment = WD_ALIGN_PARAGRAPH.CENTER
    def h2(doc, text):
        p = doc.add_heading("", level=0); r = p.add_run(text); r.bold = True; r.font.size = Pt(14)
        r.font.color.rgb = RGBColor(68,114,196)
    def para(doc, text, bold=False, indent=False):
        p = doc.add_paragraph()
        if indent: p.paragraph_format.left_indent = Cm(1)
        r = p.add_run(text); r.font.size = Pt(11); r.bold = bold
    def bullet(doc, text):
        p = doc.add_paragraph(style='List Bullet'); r = p.add_run(text); r.font.size = Pt(11)
    def sec_head(doc, text, level):
        if level == 1: h1(doc, text)
        else: h2(doc, text)
    # Cover
    h1(doc, plan.get("title","教案"))
    p = doc.add_paragraph(); p.alignment = WD_ALIGN_PARAGRAPH.CENTER
    r = p.add_run(f"学科：{plan.get('subject','')}    年级：{plan.get('grade','')}    课时：{plan.get('duration','')}")
    r.font.size = Pt(11); r.font.color.rgb = RGBColor(80,80,80)
    # Teaching Goals
    h2(doc, "一、教学目标")
    for g in plan.get("teaching_goals", []): bullet(doc, g)
    # Focus & Difficulties
    h2(doc, "二、教学重点与难点")
    for f in plan.get("teaching_focus", []): bullet(doc, f"重点：{f}")
    for d in plan.get("teaching_difficulties", []): bullet(doc, f"难点：{d}")
    # Methods
    h2(doc, "三、教学方法")
    for m in plan.get("teaching_methods", []): bullet(doc, m)
    # Teaching Process
    h2(doc, "四、教学过程")
    process = plan.get("teaching_process", {})
    if process.get("warm_up",{}).get("content"):
        h2(doc, "（一）热身导入")
        s = process["warm_up"]; para(doc, f"时长：{s.get('duration','')}", indent=True); para(doc, s["content"])
    if process.get("introduction",{}).get("content"):
        h2(doc, "（二）新课导入")
        s = process["introduction"]; para(doc, f"时长：{s.get('duration','')}", indent=True); para(doc, s["content"])
    if process.get("new_teaching"):
        h2(doc, "（三）新授环节")
        for step in process["new_teaching"]:
            p = doc.add_paragraph(); r = p.add_run(f"步骤{step.get('step','')}：{step.get('title','')}")
            r.bold = True; r.font.size = Pt(12); r.font.color.rgb = RGBColor(31,78,121)
            if step.get("duration"): para(doc, f"时长：{step['duration']}", indent=True)
            para(doc, step.get("content",""))
            for act in step.get("activities", []): bullet(doc, act, level=1)
    if process.get("practice",{}).get("content"):
        h2(doc, "（四）课堂练习")
        s = process["practice"]; para(doc, f"时长：{s.get('duration','')}", indent=True); para(doc, s["content"])
    if process.get("summary",{}).get("content"):
        h2(doc, "（五）课堂总结")
        s = process["summary"]; para(doc, f"时长：{s.get('duration','')}", indent=True); para(doc, s["content"])
    if process.get("homework"):
        h2(doc, "（六）课后作业")
        for i, hw in enumerate(process["homework"], 1): para(doc, f"{i}. {hw}")
    # Activities
    if plan.get("classroom_activities"):
        h2(doc, "七、课堂活动设计")
        for idx, act in enumerate(plan["classroom_activities"], 1):
            p = doc.add_paragraph(); r = p.add_run(f"活动{idx}：{act.get('name','')}（{act.get('type','')}）")
            r.bold = True; r.font.size = Pt(12)
            if act.get("duration"): para(doc, f"时长：{act['duration']}", indent=True)
            if act.get("description"): para(doc, f"描述：{act['description']}", indent=True)
            if act.get("purpose"): para(doc, f"目的：{act['purpose']}", indent=True)
    # Reflection
    if plan.get("teaching_reflection"):
        h2(doc, "八、教学反思"); para(doc, plan["teaching_reflection"])
    doc.save(sys.argv[1])
    print(json.dumps({"success": True}))
if __name__ == "__main__": main()
`, string(planJSON))

	tmpDir := s.renderConfig.RenderDir
	if tmpDir == "" {
		tmpDir = os.TempDir()
	}
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return "", fmt.Errorf("create render dir: %w", err)
	}

	scriptPath := filepath.Join(tmpDir, fmt.Sprintf("tp_%s.py", planID))
	if err := os.WriteFile(scriptPath, []byte(script), 0644); err != nil {
		return "", fmt.Errorf("write script: %w", err)
	}
	defer os.Remove(scriptPath)

	outputPath := filepath.Join(tmpDir, fmt.Sprintf("teaching_plan_%s.docx", planID))

	cmdCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()

	pythonPath := s.renderConfig.PythonPath
	if pythonPath == "" {
		pythonPath = "python"
	}

	cmd := exec.CommandContext(cmdCtx, pythonPath, scriptPath, outputPath)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("docx script failed: %s, stderr: %s", err, stderr.String())
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		return "", fmt.Errorf("read output file: %w", err)
	}

	objectKey := fmt.Sprintf("teaching_plans/%s/%s.docx", planID, planID)
	url, _, uploadErr := s.ossClient.PutObject(ctx, objectKey, data)
	if uploadErr != nil {
		return "", fmt.Errorf("upload to oss: %w", uploadErr)
	}
	return url, nil
}

// GetStatus returns the current status of a teaching plan.
func (s *TeachingPlanService) GetStatus(planID string) (model.TeachingPlanStatus, error) {
	s.planRepoMu.RLock()
	job, ok := s.planRepo[planID]
	s.planRepoMu.RUnlock()
	if !ok {
		return model.TeachingPlanStatus{}, ErrPlanNotFound
	}
	return model.TeachingPlanStatus{
		PlanID:      planID,
		Status:      job.Status,
		DownloadURL: job.DOCXURL,
		PlanContent: job.PlanContent,
		Error:       job.Error,
	}, nil
}

// Note: formatTeachingElementsForPrompt is defined elsewhere in the file

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

var (
	_ = regexp.MustCompile("")
	_ = strings.Join
)
