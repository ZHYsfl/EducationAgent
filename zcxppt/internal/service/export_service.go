package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"zcxppt/internal/infra/oss"
	"zcxppt/internal/model"
	"zcxppt/internal/repository"
)

type ExportService struct {
	exportRepo repository.ExportRepository
	pptRepo    repository.PPTRepository
	ossClient  *oss.Client
}

func NewExportService(exportRepo repository.ExportRepository, ossClient *oss.Client) *ExportService {
	return &ExportService{exportRepo: exportRepo, pptRepo: nil, ossClient: ossClient}
}

func (s *ExportService) AttachPPTRepository(pptRepo repository.PPTRepository) {
	s.pptRepo = pptRepo
}

func (s *ExportService) Create(taskID, format string) (model.ExportCreateResponse, error) {
	job, err := s.exportRepo.Create(taskID, format)
	if err != nil {
		return model.ExportCreateResponse{}, err
	}

	job.Status = "generating"
	job.Progress = 10
	_, _ = s.exportRepo.Update(job)

	ctx := context.Background()

	switch strings.ToLower(format) {
	case "pptx":
		if err := s.exportPPTX(ctx, job, taskID); err != nil {
			job.Status = "failed"
			job.Error = err.Error()
			_, _ = s.exportRepo.Update(job)
			return model.ExportCreateResponse{ExportID: job.ExportID, Status: "failed", EstimatedSeconds: 30}, nil
		}
	case "docx":
		if err := s.exportDOCX(ctx, job, taskID); err != nil {
			job.Status = "failed"
			job.Error = err.Error()
			_, _ = s.exportRepo.Update(job)
			return model.ExportCreateResponse{ExportID: job.ExportID, Status: "failed", EstimatedSeconds: 30}, nil
		}
	case "html":
		if err := s.exportHTML(ctx, job, taskID); err != nil {
			job.Status = "failed"
			job.Error = err.Error()
			_, _ = s.exportRepo.Update(job)
			return model.ExportCreateResponse{ExportID: job.ExportID, Status: "failed", EstimatedSeconds: 30}, nil
		}
	default:
		job.Status = "failed"
		job.Error = fmt.Sprintf("unsupported format: %s", format)
		_, _ = s.exportRepo.Update(job)
		return model.ExportCreateResponse{ExportID: job.ExportID, Status: "failed", EstimatedSeconds: 30}, nil
	}

	job.Status = "completed"
	job.Progress = 100
	_, _ = s.exportRepo.Update(job)
	return model.ExportCreateResponse{ExportID: job.ExportID, Status: "completed", EstimatedSeconds: 30}, nil
}

func (s *ExportService) exportPPTX(ctx context.Context, job model.ExportJob, taskID string) error {
	if s.pptRepo == nil {
		return fmt.Errorf("ppt repository not attached")
	}

	canvas, err := s.pptRepo.GetCanvasStatus(taskID)
	if err != nil {
		return fmt.Errorf("get canvas status: %w", err)
	}

	if len(canvas.PageOrder) == 0 {
		return fmt.Errorf("no pages in canvas")
	}

	// Collect all page codes
	pageCodes := make(map[string]string, len(canvas.PageOrder))
	for _, pageID := range canvas.PageOrder {
		page, err := s.pptRepo.GetPageRender(taskID, pageID)
		if err != nil {
			continue
		}
		pageCodes[pageID] = page.PyCode
	}

	codesJSON, _ := json.Marshal(pageCodes)

	script := fmt.Sprintf(`#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Merge all page Python codes into a single PPTX for task %s.
"""
import json
from pptx import Presentation
from pptx.util import Inches, Pt
from pptx.dml.color import RGBColor
from pptx.enum.text import PP_ALIGN

PAGE_CODES_JSON = %s

def rgb(hex_color):
    h = hex_color.lstrip("#")
    return RGBColor.from_string(h)

def add_rect(slide, left, top, width, height, fill="FFFFFF", line="CCCCCC"):
    shape = slide.shapes.add_shape(
        1, Inches(left), Inches(top), Inches(width), Inches(height)
    )
    shape.fill.solid()
    shape.fill.fore_color.rgb = rgb(fill)
    if line and line != "none":
        shape.line.color.rgb = rgb(line)
        shape.line.width = Pt(0.5)
    else:
        shape.line.fill.background()
    return shape

def add_textbox(slide, text, left, top, width, height,
                font_size=18, bold=False, color="000000",
                align=PP_ALIGN.LEFT, font_name="Microsoft YaHei"):
    txBox = slide.shapes.add_textbox(Inches(left), Inches(top),
                                      Inches(width), Inches(height))
    tf = txBox.text_frame
    tf.word_wrap = True
    p = tf.paragraphs[0]
    p.alignment = align
    run = p.add_run()
    run.text = text
    run.font.size = Pt(font_size)
    run.font.bold = bold
    run.font.color.rgb = rgb(color)
    run.font.name = font_name

def set_slide_title(slide, text, font_size=32, color="FFFFFF",
                   bg_color="1F4E79", height=1.2):
    title_box = slide.shapes.add_textbox(
        Inches(0), Inches(0), Inches(10), Inches(height)
    )
    tf = title_box.text_frame
    tf.word_wrap = True
    p = tf.paragraphs[0]
    p.alignment = PP_ALIGN.CENTER
    run = p.add_run()
    run.text = text
    run.font.size = Pt(font_size)
    run.font.bold = True
    run.font.color.rgb = rgb(color)
    run.font.name = "Microsoft YaHei"

def add_oval(slide, left, top, width, height, fill="4472C4", line="none"):
    shape = slide.shapes.add_shape(
        9, Inches(left), Inches(top), Inches(width), Inches(height)
    )
    shape.fill.solid()
    shape.fill.fore_color.rgb = rgb(fill)
    if line and line != "none":
        shape.line.color.rgb = rgb(line)
    else:
        shape.line.fill.background()
    return shape

def add_image(slide, path, left, top, width, height):
    pic = slide.shapes.add_picture(path, Inches(left), Inches(top),
                                  Inches(width), Inches(height))
    return pic

def main():
    import sys
    output_path = sys.argv[1] if len(sys.argv) > 1 else "output.pptx"

    page_codes = json.loads(PAGE_CODES_JSON)
    merged_prs = Presentation()
    merged_prs.slide_width = Inches(10)
    merged_prs.slide_height = Inches(7.5)

    ordered_ids = sorted(page_codes.keys())

    for page_id in ordered_ids:
        code = page_codes[page_id]
        blank_layout = merged_prs.slide_layouts[6]
        slide = merged_prs.slides.add_slide(blank_layout)
        slide.background.fill.solid()
        slide.background.fill.fore_color.rgb = rgb("FFFFFF")

        user_globals = {
            "prs": merged_prs,
            "slide": slide,
            "width": 10,
            "height": 7.5,
            "rgb": rgb,
            "add_textbox": add_textbox,
            "add_rect": add_rect,
            "add_oval": add_oval,
            "add_image": add_image,
            "set_slide_title": set_slide_title,
            "PP_ALIGN": PP_ALIGN,
            "font_name": "Microsoft YaHei",
        }
        user_globals.update({
            "add_shape": slide.shapes.add_shape,
            "add_table": slide.shapes.add_table,
        })
        exec_globals = {"__builtins__": {}}
        exec_globals.update(user_globals)
        if code and code.strip():
            try:
                exec(code, exec_globals)
            except Exception as e:
                add_textbox(slide, "Page: " + page_id + " | Error: " + str(e)[:100],
                           0.5, 0.5, 9, 6, font_size=12, color="CC0000")

    merged_prs.save(output_path)
    print("MERGE_OK:" + output_path)

if __name__ == "__main__":
    main()
`, taskID, string(codesJSON))

	scriptPath := filepath.Join(os.TempDir(), fmt.Sprintf("merge_%s.py", taskID))
	if err := os.WriteFile(scriptPath, []byte(script), 0644); err != nil {
		return fmt.Errorf("write merge script: %w", err)
	}
	defer os.Remove(scriptPath)

	outputPath := filepath.Join(os.TempDir(), fmt.Sprintf("%s_%s.pptx", taskID, job.ExportID))

	cmdCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "python", scriptPath, outputPath)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("merge script failed: %s, stderr: %s", err, stderr.String())
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		return fmt.Errorf("read output file: %w", err)
	}

	job.Progress = 60
	_, _ = s.exportRepo.Update(job)

	objectKey := fmt.Sprintf("exports/%s/%s.pptx", taskID, job.ExportID)
	url, _, uploadErr := s.ossClient.PutObject(ctx, objectKey, data)
	if uploadErr != nil {
		return fmt.Errorf("upload to oss: %w", uploadErr)
	}

	job.DownloadURL = url
	job.FileSize = int64(len(data))
	job.Progress = 90
	_, _ = s.exportRepo.Update(job)
	return nil
}

func (s *ExportService) exportDOCX(ctx context.Context, job model.ExportJob, taskID string) error {
	if s.pptRepo == nil {
		return fmt.Errorf("ppt repository not attached")
	}

	canvas, err := s.pptRepo.GetCanvasStatus(taskID)
	if err != nil {
		return fmt.Errorf("get canvas status: %w", err)
	}

	var docContent strings.Builder
	docContent.WriteString("# PPT Export\n\n")

	for i, pageID := range canvas.PageOrder {
		page, err := s.pptRepo.GetPageRender(taskID, pageID)
		if err != nil {
			continue
		}
		docContent.WriteString(fmt.Sprintf("\n## 第 %d 页: %s\n\n", i+1, pageID))
		docContent.WriteString("```python\n")
		docContent.WriteString(page.PyCode)
		docContent.WriteString("\n```\n\n")
	}

	content := []byte(docContent.String())

	job.Progress = 60
	_, _ = s.exportRepo.Update(job)

	objectKey := fmt.Sprintf("exports/%s/%s.md", taskID, job.ExportID)
	url, _, uploadErr := s.ossClient.PutObject(ctx, objectKey, content)
	if uploadErr != nil {
		return fmt.Errorf("upload to oss: %w", uploadErr)
	}

	job.DownloadURL = url
	job.FileSize = int64(len(content))
	job.Progress = 90
	_, _ = s.exportRepo.Update(job)
	return nil
}

func (s *ExportService) exportHTML(ctx context.Context, job model.ExportJob, taskID string) error {
	if s.pptRepo == nil {
		return fmt.Errorf("ppt repository not attached")
	}

	canvas, err := s.pptRepo.GetCanvasStatus(taskID)
	if err != nil {
		return fmt.Errorf("get canvas status: %w", err)
	}

	var htmlContent strings.Builder
	htmlContent.WriteString(`<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>PPT Export</title>
<style>
  * { margin: 0; padding: 0; box-sizing: border-box; }
  body { font-family: "Microsoft YaHei", Arial, sans-serif; background: #f5f5f5; }
  .slide { width: 1000px; height: 750px; margin: 20px auto; background: #fff; box-shadow: 0 2px 8px rgba(0,0,0,0.15); overflow: hidden; position: relative; }
  .slide-number { position: absolute; bottom: 10px; right: 20px; font-size: 14px; color: #999; }
  .container { max-width: 1100px; margin: 0 auto; padding: 20px; }
  h1 { text-align: center; margin-bottom: 30px; color: #1F4E79; }
  pre { white-space: pre-wrap; word-wrap: break-word; background: #fafafa; border: 1px solid #eee; padding: 15px; border-radius: 4px; overflow-x: auto; font-family: Consolas, "Courier New", monospace; }
  .code-slide { font-family: Consolas, "Courier New", monospace; font-size: 13px; padding: 20px; line-height: 1.6; white-space: pre-wrap; }
</style>
</head>
<body>
<div class="container">
<h1>PPT Export - `)
	htmlContent.WriteString(taskID)
	htmlContent.WriteString(`</h1>
`)

	for i, pageID := range canvas.PageOrder {
		page, err := s.pptRepo.GetPageRender(taskID, pageID)
		if err != nil {
			continue
		}
		htmlContent.WriteString(fmt.Sprintf(`<div class="slide">
  <div class="code-slide">%s</div>
  <div class="slide-number">Page %d</div>
</div>
`, page.PyCode, i+1))
	}

	htmlContent.WriteString(`
</div>
</body>
</html>`)

	content := []byte(htmlContent.String())

	job.Progress = 60
	_, _ = s.exportRepo.Update(job)

	objectKey := fmt.Sprintf("exports/%s/%s.html", taskID, job.ExportID)
	url, _, uploadErr := s.ossClient.PutObject(ctx, objectKey, content)
	if uploadErr != nil {
		return fmt.Errorf("upload to oss: %w", uploadErr)
	}

	job.DownloadURL = url
	job.FileSize = int64(len(content))
	job.Progress = 90
	_, _ = s.exportRepo.Update(job)
	return nil
}

func (s *ExportService) Get(exportID string) (model.ExportStatusResponse, error) {
	job, err := s.exportRepo.Get(exportID)
	if err != nil {
		return model.ExportStatusResponse{}, err
	}
	return model.ExportStatusResponse{
		ExportID:    job.ExportID,
		Status:      job.Status,
		DownloadURL: job.DownloadURL,
		Format:      job.Format,
		FileSize:    job.FileSize,
		Error:       job.Error,
	}, nil
}
