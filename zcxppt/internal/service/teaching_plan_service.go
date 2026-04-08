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
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	openai "github.com/openai/openai-go/v3"
	toolcalling "tool_calling_go"

	"zcxppt/internal/infra/oss"
	"zcxppt/internal/model"
)

var (
	ErrInvalidTeachingPlanRequest = errors.New("invalid teaching plan request")
	ErrPlanNotFound               = errors.New("teaching plan not found")
	ErrRenderScriptNotConfigured  = errors.New("teaching plan render script not configured")
)

// TeachingPlanService generates Word教案 (.docx) from LLM-generated structured content.
type TeachingPlanService struct {
	planRepo    map[string]*teachingPlanJob
	planRepoMu   sync.RWMutex
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
	PlanID      string
	TaskID      string
	Status      string
	PlanContent string
	DOCXURL     string
	Error       string
	CreatedAt   time.Time
}

// NewTeachingPlanService creates a new TeachingPlanService.
func NewTeachingPlanService(llmCfg LLMClientConfig, renderCfg RenderServiceConfig, ossClient *oss.Client) *TeachingPlanService {
	return &TeachingPlanService{
		planRepo:    make(map[string]*teachingPlanJob),
		httpClient:  &http.Client{Timeout: 30 * time.Second},
		llmCfg:      llmCfg,
		renderConfig: renderCfg,
		ossClient:   ossClient,
	}
}

// Generate starts async teaching plan generation and returns immediately with plan_id.
func (s *TeachingPlanService) Generate(ctx context.Context, req model.TeachingPlanRequest) (model.TeachingPlanResponse, error) {
	if strings.TrimSpace(req.Topic) == "" {
		return model.TeachingPlanResponse{}, ErrInvalidTeachingPlanRequest
	}

	planID := "plan_" + uuid.NewString()
	job := &teachingPlanJob{
		PlanID:    planID,
		TaskID:    req.TaskID,
		Status:    "generating",
		CreatedAt: time.Now(),
	}

	s.planRepoMu.Lock()
	s.planRepo[planID] = job
	s.planRepoMu.Unlock()

	go s.runGeneration(planID, req)

	return model.TeachingPlanResponse{
		TaskID: req.TaskID,
		PlanID: planID,
		Status: "generating",
	}, nil
}

func (s *TeachingPlanService) runGeneration(planID string, req model.TeachingPlanRequest) {
	defer func() {
		if r := recover(); r != nil {
			s.planRepoMu.Lock()
			if job, ok := s.planRepo[planID]; ok {
				job.Status = "failed"
				job.Error = fmt.Sprintf("panic: %v", r)
			}
			s.planRepoMu.Unlock()
		}
	}()

	ctx := context.Background()

	planContent, err := s.generatePlanContent(ctx, req)
	if err != nil {
		s.planRepoMu.Lock()
		if job, ok := s.planRepo[planID]; ok {
			job.Status = "failed"
			job.Error = err.Error()
		}
		s.planRepoMu.Unlock()
		return
	}

	s.planRepoMu.Lock()
	if job, ok := s.planRepo[planID]; ok {
		job.PlanContent = planContent
	}
	s.planRepoMu.Unlock()

	docxURL, err := s.renderDOCX(ctx, planID, planContent)
	if err != nil {
		s.planRepoMu.Lock()
		if job, ok := s.planRepo[planID]; ok {
			job.Status = "failed"
			job.Error = err.Error()
		}
		s.planRepoMu.Unlock()
		return
	}

	s.planRepoMu.Lock()
	if job, ok := s.planRepo[planID]; ok {
		job.Status = "completed"
		job.DOCXURL = docxURL
	}
	s.planRepoMu.Unlock()
}

// generatePlanContent uses LLM to generate structured teaching plan in JSON format.
func (s *TeachingPlanService) generatePlanContent(ctx context.Context, req model.TeachingPlanRequest) (string, error) {
	if strings.TrimSpace(s.llmCfg.APIKey) == "" || strings.TrimSpace(s.llmCfg.Model) == "" || strings.TrimSpace(s.llmCfg.BaseURL) == "" {
		return "", errors.New("llm config not complete")
	}

	system := `你是资深教学设计师，请根据用户需求生成一份详细的Word教案。

严格输出 JSON 对象，格式如下：
{
  "title": "教案标题",
  "subject": "学科",
  "grade": "年级",
  "duration": "课时",
  "teaching_goals": ["目标1", "目标2", ...],
  "teaching_focus": ["教学重点1", "教学重点2", ...],
  "teaching_difficulties": ["难点1", "难点2", ...],
  "teaching_methods": ["方法1", "方法2", ...],
  "teaching_process": {
    "warm_up": {"duration": "时长", "content": "热身内容"},
    "introduction": {"duration": "时长", "content": "导入环节内容"},
    "new_teaching": [
      {"step": 1, "title": "步骤标题", "duration": "时长", "content": "详细内容", "activities": ["活动1", "活动2"]},
      {"step": 2, "title": "步骤标题", "duration": "时长", "content": "详细内容", "activities": ["活动1", "活动2"]}
    ],
    "practice": {"duration": "时长", "content": "练习内容"},
    "summary": {"duration": "时长", "content": "总结内容"},
    "homework": ["作业1", "作业2", ...]
  },
  "classroom_activities": [
    {"name": "活动名称", "type": "活动类型", "duration": "时长", "description": "活动描述", "purpose": "活动目的"}
  ],
  "teaching_reflection": "教学反思说明",
  "teaching_aids": ["教具1", "教具2", ...]
}

要求：
1. teaching_process 中的 new_teaching 至少包含3个步骤
2. classroom_activities 至少包含2个活动
3. 每个步骤的 content 要详细，包含教师行为和学生行为
4. 所有内容使用中文输出
5. 不要输出 JSON 之外的任何文字`

	prompt := fmt.Sprintf("topic=%s\nsubject=%s\ndescription=%s\naudience=%s\nduration=%s\nteaching_elements=%s\nstyle_guide=%s",
		req.Topic,
		req.Subject,
		req.Description,
		req.Audience,
		req.Duration,
		formatTeachingElements(req.TeachingElements),
		req.StyleGuide,
	)

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

	// Verify it's valid JSON
	var dummy map[string]any
	if err := json.Unmarshal([]byte(cleaned), &dummy); err != nil {
		return "", fmt.Errorf("llm output is not valid json: %w", err)
	}

	return cleaned, nil
}

// renderDOCX calls Python script to generate .docx from JSON plan content.
func (s *TeachingPlanService) renderDOCX(ctx context.Context, planID, planContent string) (string, error) {
	if s.renderConfig.ScriptPath == "" || s.ossClient == nil {
		return "", ErrRenderScriptNotConfigured
	}

	planJSON, _ := json.Marshal(map[string]any{"plan": planContent})

	script := fmt.Sprintf(`#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Generate Word (.docx) teaching plan from JSON plan content.
"""
import json
import sys
import os

PLAN_JSON = %s

def main():
    plan_data = json.loads(PLAN_JSON["plan"])
    output_path = sys.argv[1] if len(sys.argv) > 1 else "teaching_plan.docx"

    try:
        from docx import Document
        from docx.shared import Pt, RGBColor, Inches, Cm
        from docx.enum.text import WD_ALIGN_PARAGRAPH
        from docx.oxml.ns import qn
        from docx.oxml import OxmlElement
    except ImportError:
        print(json.dumps({"success": False, "docx_path": "", "error": "python-docx not installed"}))
        sys.exit(1)

    doc = Document()

    # Page margins
    sections = doc.sections
    for section in sections:
        section.top_margin = Cm(2.5)
        section.bottom_margin = Cm(2.5)
        section.left_margin = Cm(2.5)
        section.right_margin = Cm(2.5)

    def set_heading(paragraph, text, level=1):
        run = paragraph.add_run(text)
        run.bold = True
        if level == 1:
            run.font.size = Pt(18)
            run.font.color.rgb = RGBColor(31, 78, 121)
        elif level == 2:
            run.font.size = Pt(14)
            run.font.color.rgb = RGBColor(68, 114, 196)
        else:
            run.font.size = Pt(12)
            run.font.color.rgb = RGBColor(0, 0, 0)

    def add_heading(doc, text, level=1):
        p = doc.add_heading("", level=0)
        run = p.add_run(text)
        run.bold = True
        if level == 1:
            run.font.size = Pt(18)
            run.font.color.rgb = RGBColor(31, 78, 121)
            p.alignment = WD_ALIGN_PARAGRAPH.CENTER
        elif level == 2:
            run.font.size = Pt(14)
            run.font.color.rgb = RGBColor(68, 114, 196)
        return p

    def add_paragraph(doc, text, bold=False, indent=False):
        p = doc.add_paragraph()
        if indent:
            p.paragraph_format.left_indent = Cm(1)
        run = p.add_run(text)
        run.font.size = Pt(11)
        run.bold = bold
        return p

    def add_bullet(doc, text, level=0):
        p = doc.add_paragraph(style='List Bullet')
        if level > 0:
            p.paragraph_format.left_indent = Cm(1 + level * 0.5)
        run = p.add_run(text)
        run.font.size = Pt(11)
        return p

    # === Cover / Title ===
    add_heading(doc, plan_data.get("title", "教案"), level=1)
    add_paragraph(doc, "")
    p = doc.add_paragraph()
    p.alignment = WD_ALIGN_PARAGRAPH.CENTER
    run = p.add_run(f"学科：{plan_data.get('subject', '')}    年级：{plan_data.get('grade', '')}    课时：{plan_data.get('duration', '')}")
    run.font.size = Pt(11)
    run.font.color.rgb = RGBColor(80, 80, 80)
    add_paragraph(doc, "")

    # === Teaching Goals ===
    add_heading(doc, "一、教学目标", level=2)
    for goal in plan_data.get("teaching_goals", []):
        add_bullet(doc, goal)

    # === Teaching Focus & Difficulties ===
    add_heading(doc, "二、教学重点与难点", level=2)
    add_paragraph(doc, "教学重点：", bold=True)
    for f in plan_data.get("teaching_focus", []):
        add_bullet(doc, f)
    add_paragraph(doc, "教学难点：", bold=True)
    for d in plan_data.get("teaching_difficulties", []):
        add_bullet(doc, d)

    # === Teaching Methods ===
    add_heading(doc, "三、教学方法", level=2)
    for m in plan_data.get("teaching_methods", []):
        add_bullet(doc, m)

    # === Teaching Aids ===
    if plan_data.get("teaching_aids"):
        add_heading(doc, "四、教学准备", level=2)
        for aid in plan_data.get("teaching_aids", []):
            add_bullet(doc, aid)

    # === Teaching Process ===
    add_heading(doc, "五、教学过程", level=2)
    process = plan_data.get("teaching_process", {})

    # Warm-up
    warm_up = process.get("warm_up", {})
    if warm_up.get("content"):
        add_heading(doc, "（一）热身导入", level=2)
        if warm_up.get("duration"):
            add_paragraph(doc, f"时长：{warm_up['duration']}", indent=True)
        add_paragraph(doc, warm_up["content"])

    # Introduction
    intro = process.get("introduction", {})
    if intro.get("content"):
        add_heading(doc, "（二）新课导入", level=2)
        if intro.get("duration"):
            add_paragraph(doc, f"时长：{intro['duration']}", indent=True)
        add_paragraph(doc, intro["content"])

    # New Teaching
    new_teach = process.get("new_teaching", [])
    add_heading(doc, "（三）新授环节", level=2)
    for step_data in new_teach:
        p = doc.add_paragraph()
        run = p.add_run(f"步骤{step_data.get('step', '')}：{step_data.get('title', '')}")
        run.bold = True
        run.font.size = Pt(12)
        run.font.color.rgb = RGBColor(31, 78, 121)
        if step_data.get("duration"):
            add_paragraph(doc, f"时长：{step_data['duration']}", indent=True)
        add_paragraph(doc, step_data.get("content", ""))
        for act in step_data.get("activities", []):
            add_bullet(doc, act, level=1)

    # Practice
    practice = process.get("practice", {})
    if practice.get("content"):
        add_heading(doc, "（四）课堂练习", level=2)
        if practice.get("duration"):
            add_paragraph(doc, f"时长：{practice['duration']}", indent=True)
        add_paragraph(doc, practice["content"])

    # Summary
    summary = process.get("summary", {})
    if summary.get("content"):
        add_heading(doc, "（五）课堂总结", level=2)
        if summary.get("duration"):
            add_paragraph(doc, f"时长：{summary['duration']}", indent=True)
        add_paragraph(doc, summary["content"])

    # Homework
    homework = process.get("homework", [])
    if homework:
        add_heading(doc, "（六）课后作业", level=2)
        for i, hw in enumerate(homework, 1):
            add_paragraph(doc, f"{i}. {hw}")

    # === Classroom Activities ===
    activities = plan_data.get("classroom_activities", [])
    if activities:
        add_heading(doc, "六、课堂活动设计", level=2)
        for idx, act in enumerate(activities, 1):
            p = doc.add_paragraph()
            run = p.add_run(f"活动{idx}：{act.get('name', '')}（{act.get('type', '')}）")
            run.bold = True
            run.font.size = Pt(12)
            if act.get("duration"):
                add_paragraph(doc, f"时长：{act['duration']}", indent=True)
            if act.get("description"):
                add_paragraph(doc, f"描述：{act['description']}", indent=True)
            if act.get("purpose"):
                add_paragraph(doc, f"目的：{act['purpose']}", indent=True)

    # === Teaching Reflection ===
    reflection = plan_data.get("teaching_reflection", "")
    if reflection:
        add_heading(doc, "七、教学反思", level=2)
        add_paragraph(doc, reflection)

    doc.save(output_path)
    print(json.dumps({"success": True, "docx_path": output_path, "error": ""}))

if __name__ == "__main__":
    main()
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
		Error:      job.Error,
	}, nil
}

func formatTeachingElements(te *model.InitTeachingElements) string {
	if te == nil {
		return "(未提供)"
	}
	b, _ := json.Marshal(te)
	return string(b)
}
