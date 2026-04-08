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

	f := strings.ToLower(strings.TrimSpace(format))
	switch f {
	case "pptx", "docx", "html", "html5":
	default:
		job.Status = "failed"
		job.Error = fmt.Sprintf("unsupported format: %s", format)
		_, _ = s.exportRepo.Update(job)
		return model.ExportCreateResponse{ExportID: job.ExportID, Status: "failed", EstimatedSeconds: 30}, nil
	}

	job.Status = "generating"
	job.Progress = 10
	_, _ = s.exportRepo.Update(job)

	go s.runExportJob(job.ExportID, taskID, f)

	return model.ExportCreateResponse{ExportID: job.ExportID, Status: "generating", EstimatedSeconds: 30}, nil
}

func (s *ExportService) runExportJob(exportID, taskID, format string) {
	defer func() {
		if r := recover(); r != nil {
			job, err := s.exportRepo.Get(exportID)
			if err == nil {
				job.Status = "failed"
				job.Error = fmt.Sprintf("export panic: %v", r)
				_, _ = s.exportRepo.Update(job)
			}
		}
	}()

	ctx := context.Background()
	job, err := s.exportRepo.Get(exportID)
	if err != nil {
		return
	}

	var exportErr error
	switch format {
	case "pptx":
		exportErr = s.exportPPTX(ctx, job, taskID)
	case "docx":
		exportErr = s.exportDOCX(ctx, job, taskID)
	case "html", "html5":
		exportErr = s.exportHTML(ctx, job, taskID)
	default:
		job.Status = "failed"
		job.Error = fmt.Sprintf("unsupported format: %s", format)
		_, _ = s.exportRepo.Update(job)
		return
	}

	if exportErr != nil {
		job, err = s.exportRepo.Get(exportID)
		if err == nil {
			job.Status = "failed"
			job.Error = exportErr.Error()
			_, _ = s.exportRepo.Update(job)
		}
		return
	}

	job, err = s.exportRepo.Get(exportID)
	if err != nil {
		return
	}
	job.Status = "completed"
	job.Progress = 100
	_, _ = s.exportRepo.Update(job)
}

// execPython runs a Python script and returns output path + error.
func (s *ExportService) execPython(ctx context.Context, script string, args ...string) (string, error) {
	tmpDir := os.TempDir()
	scriptPath := filepath.Join(tmpDir, fmt.Sprintf("export_%d.py", time.Now().UnixNano()))
	if err := os.WriteFile(scriptPath, []byte(script), 0644); err != nil {
		return "", fmt.Errorf("write script: %w", err)
	}
	defer os.Remove(scriptPath)

	outputPath := args[0]
	if outputPath == "" {
		outputPath = filepath.Join(tmpDir, "output.bin")
	}

	cmdCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	pythonPath := "python"
	cmd := exec.CommandContext(cmdCtx, pythonPath, append([]string{scriptPath}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("script failed: %s, stderr: %s", err, stderr.String())
	}

	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		return "", fmt.Errorf("output file not created")
	}

	return outputPath, nil
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

	pageCodes := make(map[string]string, len(canvas.PageOrder))
	for _, pageID := range canvas.PageOrder {
		page, err := s.pptRepo.GetPageRender(taskID, pageID)
		if err != nil {
			continue
		}
		pageCodes[pageID] = page.PyCode
	}

	codesJSON, _ := json.Marshal(pageCodes)

	// Build Python merge script using string concatenation (no raw strings)
	script := strings.Join([]string{
		"#!/usr/bin/env python3\n# -*- coding: utf-8 -*-\n",
		"import json,sys\n",
		"from pptx import Presentation\n",
		"from pptx.util import Inches,Pt\n",
		"from pptx.dml.color import RGBColor\n",
		"from pptx.enum.text import PP_ALIGN\n\n",
		"PAGE_CODES_JSON = " + string(codesJSON) + "\n\n",
		"def rgb(h): return RGBColor.from_string(h.lstrip(\"#\"))\n\n",
		"def add_rect(slide,l,t,w,h,fill=\"FFFFFF\",line=\"CCCCCC\"):\n",
		"  sh=slide.shapes.add_shape(1,Inches(l),Inches(t),Inches(w),Inches(h))\n",
		"  sh.fill.solid();sh.fill.fore_color.rgb=rgb(fill)\n",
		"  if line and line!=\"none\":sh.line.color.rgb=rgb(line);sh.line.width=Pt(0.5)\n",
		"  else:sh.line.fill.background()\n",
		"  return sh\n\n",
		"def add_textbox(slide,text,l,t,w,h,font_size=18,bold=False,color=\"000000\",align=PP_ALIGN.LEFT):\n",
		"  tb=slide.shapes.add_textbox(Inches(l),Inches(t),Inches(w),Inches(h))\n",
		"  tf=tb.text_frame;tf.word_wrap=True;p=tf.paragraphs[0];p.alignment=align\n",
		"  run=p.add_run();run.text=text;run.font.size=Pt(font_size)\n",
		"  run.font.bold=bold;run.font.color.rgb=rgb(color);run.font.name=\"Microsoft YaHei\"\n\n",
		"def set_slide_title(slide,text,font_size=32,color=\"FFFFFF\",bg_color=\"1F4E79\",height=1.2):\n",
		"  tb=slide.shapes.add_textbox(Inches(0),Inches(0),Inches(10),Inches(height))\n",
		"  tf=tb.text_frame;tf.word_wrap=True;p=tf.paragraphs[0];p.alignment=PP_ALIGN.CENTER\n",
		"  run=p.add_run();run.text=text;run.font.size=Pt(font_size)\n",
		"  run.font.bold=True;run.font.color.rgb=rgb(color);run.font.name=\"Microsoft YaHei\"\n\n",
		"def add_oval(slide,l,t,w,h,fill=\"4472C4\",line=\"none\"):\n",
		"  sh=slide.shapes.add_shape(9,Inches(l),Inches(t),Inches(w),Inches(h))\n",
		"  sh.fill.solid();sh.fill.fore_color.rgb=rgb(fill)\n",
		"  if line and line!=\"none\":sh.line.color.rgb=rgb(line)\n",
		"  else:sh.line.fill.background()\n",
		"  return sh\n\n",
		"def add_image(slide,path,l,t,w,h):\n",
		"  return slide.shapes.add_picture(path,Inches(l),Inches(t),Inches(w),Inches(h))\n\n",
		"def main():\n",
		"  out=sys.argv[1] if len(sys.argv)>1 else \"output.pptx\"\n",
		"  pc=json.loads(PAGE_CODES_JSON)\n",
		"  prs=Presentation()\n",
		"  prs.slide_width=Inches(10);prs.slide_height=Inches(7.5)\n",
		"  for pid in sorted(pc.keys()):\n",
		"    code=pc[pid]\n",
		"    sl=prs.slides.add_slide(prs.slide_layouts[6])\n",
		"    sl.background.fill.solid();sl.background.fill.fore_color.rgb=rgb(\"FFFFFF\")\n",
		"    g={\"prs\":prs,\"slide\":sl,\"width\":10,\"height\":7.5,\"rgb\":rgb,\n",
		"       \"add_textbox\":add_textbox,\"add_rect\":add_rect,\"add_oval\":add_oval,\n",
		"       \"add_image\":add_image,\"set_slide_title\":set_slide_title,\n",
		"       \"PP_ALIGN\":PP_ALIGN,\"font_name\":\"Microsoft YaHei\",\n",
		"       \"add_shape\":sl.shapes.add_shape,\"add_table\":sl.shapes.add_table}\n",
		"    eg={\"__builtins__\":{}};eg.update(g)\n",
		"    if code and code.strip():\n",
		"      try:exec(code,eg)\n",
		"      except Exception as e:\n",
		"        add_textbox(sl,\"Error: \"+str(e)[:100],0.5,0.5,9,6,font_size=12,color=\"CC0000\")\n",
		"  prs.save(out)\n",
		"  print(\"OK:\"+out)\n\n",
		"if __name__==\"__main__\":main()\n",
	}, "")

	outputPath := filepath.Join(os.TempDir(), fmt.Sprintf("%s_%s.pptx", taskID, job.ExportID))
	createdPath, err := s.execPython(ctx, script, outputPath)
	if err != nil {
		return err
	}
	if createdPath != "" {
		outputPath = createdPath
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		return fmt.Errorf("read output: %w", err)
	}

	job.Progress = 60
	_, _ = s.exportRepo.Update(job)

	objectKey := fmt.Sprintf("exports/%s/%s.pptx", taskID, job.ExportID)
	url, _, uploadErr := s.ossClient.PutObject(ctx, objectKey, data)
	if uploadErr != nil {
		return fmt.Errorf("upload: %w", uploadErr)
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

	var summaries []map[string]string
	for i, pageID := range canvas.PageOrder {
		page, err := s.pptRepo.GetPageRender(taskID, pageID)
		if err != nil {
			continue
		}
		summaries = append(summaries, map[string]string{
			"index":   fmt.Sprintf("%d", i+1),
			"page_id": pageID,
			"py_code": page.PyCode,
		})
	}

	summariesJSON, _ := json.Marshal(summaries)

	script := strings.Join([]string{
		"#!/usr/bin/env python3\n# -*- coding: utf-8 -*-\n",
		"import json,sys\n",
		"from datetime import datetime as dt\n",
		"PAGE_DATA = " + string(summariesJSON) + "\n\n",
		"def main():\n",
		"  out=sys.argv[1] if len(sys.argv)>1 else \"ppt_export.docx\"\n",
		"  try:\n",
		"    from docx import Document\n",
		"    from docx.shared import Pt,RGBColor,Cm\n",
		"    from docx.enum.text import WD_ALIGN_PARAGRAPH\n",
		"  except:\n",
		"    print(json.dumps({\"ok\":False,\"err\":\"python-docx not installed\"}));return\n",
		"  doc=Document()\n",
		"  for sec in doc.sections:\n",
		"    sec.top_margin=Cm(2);sec.bottom_margin=Cm(2)\n",
		"    sec.left_margin=Cm(2.5);sec.right_margin=Cm(2.5)\n",
		"  def hd(text,lv=1):\n",
		"    p=doc.add_heading(text,0)\n",
		"    for r in p.runs:\n",
		"      if lv==1:r.font.color.rgb=RGBColor(31,78,121);r.font.size=Pt(16)\n",
		"      else:r.font.color.rgb=RGBColor(68,114,196);r.font.size=Pt(13)\n",
		"    if lv==1:p.alignment=WD_ALIGN_PARAGRAPH.CENTER\n",
		"  def para(text,bold=False):\n",
		"    p=doc.add_paragraph()\n",
		"    r=p.add_run(text);r.font.size=Pt(11);r.bold=bold\n",
		"  def codeblock(code):\n",
		"    p=doc.add_paragraph()\n",
		"    r=p.add_run(code);r.font.name=\"Courier New\";r.font.size=Pt(9)\n",
		"    r.font.color.rgb=RGBColor(80,80,80);p.paragraph_format.left_indent=Cm(0.5)\n",
		"  hd(\"\\u8bfe\\u4ef6\\u5bfc\\u51fa\",1)\n",
		"  para(\"\\u5bfc\\u51fa\\u65f6\\u95f4: \"+dt.now().strftime(\"%Y-%m-%d %H:%M:%S\"))\n",
		"  para(\"Task ID: " + taskID + "\")\n",
		"  para(\"\")\n",
		"  hd(\"\\u9875\\u9762\\u5217\\u8868\",2)\n",
		"  for s in PAGE_DATA:\n",
		"    p=doc.add_paragraph()\n",
		"    r=p.add_run(\"\\u7b2c\"+s.get(\"index\",\"\")+\"\\u9875: \"+s.get(\"page_id\",\"\"))\n",
		"    r.bold=True;r.font.size=Pt(11);r.font.color.rgb=RGBColor(31,78,121)\n",
		"    if s.get(\"py_code\"):\n",
		"      p2=doc.add_paragraph()\n",
		"      r2=p2.add_run(\"Python\\u6e32\\u67d3\\u4ee3\\u7801:\")\n",
		"      r2.bold=True;r2.font.size=Pt(10)\n",
		"      codeblock(s[\"py_code\"])\n",
		"    doc.add_paragraph(\"\")\n",
		"  doc.save(out)\n",
		"  print(json.dumps({\"ok\":True,\"path\":out}))\n\n",
		"if __name__==\"__main__\":main()\n",
	}, "")

	outputPath := filepath.Join(os.TempDir(), fmt.Sprintf("%s_%s.docx", taskID, job.ExportID))
	createdPath, err := s.execPython(ctx, script, outputPath)
	if err != nil {
		return err
	}
	if createdPath != "" {
		outputPath = createdPath
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		return fmt.Errorf("read output: %w", err)
	}

	job.Progress = 60
	_, _ = s.exportRepo.Update(job)

	objectKey := fmt.Sprintf("exports/%s/%s.docx", taskID, job.ExportID)
	url, _, uploadErr := s.ossClient.PutObject(ctx, objectKey, data)
	if uploadErr != nil {
		return fmt.Errorf("upload: %w", uploadErr)
	}

	job.DownloadURL = url
	job.FileSize = int64(len(data))
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

	type pageInfo struct {
		Index     string `json:"index"`
		PageID   string `json:"page_id"`
		PyCode   string `json:"py_code"`
		RenderURL string `json:"render_url"`
	}
	var pages []pageInfo
	for i, pageID := range canvas.PageOrder {
		page, err := s.pptRepo.GetPageRender(taskID, pageID)
		if err != nil {
			continue
		}
		pages = append(pages, pageInfo{
			Index:     fmt.Sprintf("%d", i+1),
			PageID:   pageID,
			PyCode:   page.PyCode,
			RenderURL: page.RenderURL,
		})
	}

	totalPages := len(pages)
	pagesJSON, _ := json.Marshal(pages)

	// Use string concatenation to build script (avoids backtick issues)
	script := "#!/usr/bin/env python3\n# -*- coding: utf-8 -*-\n" +
		"import json,sys,html as hmod\n" +
		"PAGES=" + string(pagesJSON) + "\n" +
		"TID=\"" + taskID + "\"\n" +
		"TP=" + fmt.Sprintf("%d", totalPages) + "\n\n" +
		"def esc(s):\n" +
		"  return hmod.escape(s).replace(\"\\n\",\"<br>\").replace(\" \",\"&nbsp;\")\n\n" +
		"def main():\n" +
		"  out=sys.argv[1] if len(sys.argv)>1 else \"ppt_export.html\"\n" +
		"  slides=[]\n" +
		"  for p in PAGES:\n" +
		"    code=esc(p.get(\"py_code\",\"\"))\n" +
		"    s=(\"  <div class=\\\"slide\\\" id=\\\"slide_\"+p[\"index\"]+\"\\\">\\n\" +\n" +
		"       \"    <div class=\\\"slide-header\\\">\\n\" +\n" +
		"       \"      <span class=\\\"slide-title\\\">\\u7b2c \"+p[\"index\"]+\" \\u9875</span>\\n\" +\n" +
		"       \"      <span class=\\\"slide-id\\\">\"+p[\"page_id\"]+\"</span>\\n\" +\n" +
		"       \"    </div>\\n\" +\n" +
		"       \"    <div class=\\\"slide-content\\\">\\n\" +\n" +
		"       \"      <div class=\\\"slide-code\\\">\"+code+\"</div>\\n\" +\n" +
		"       \"    </div>\\n\" +\n" +
		"       \"    <div class=\\\"slide-footer\\\">\\n\" +\n" +
		"       \"      <span>\\u7b2c \"+p[\"index\"]+\" / \"+str(TP)+\" \\u9875</span>\\n\" +\n" +
		"       \"    </div>\\n\" +\n" +
		"       \"  </div>\")\n" +
		"    slides.append(s)\n" +
		"  slides_html=\"\\n\".join(slides)\n" +
		"  html=(\n" +
		"    \"<!DOCTYPE html>\\n<html lang=\\\"zh-CN\\\">\\n<head>\\n\"+\n" +
		"    \"<meta charset=\\\"UTF-8\\\">\\n\"+\n" +
		"    \"<meta name=\\\"viewport\\\" content=\\\"width=device-width,initial-scale=1.0\\\">\\n\"+\n" +
		"    \"<title>PPT \\u8bfe\\u4ef6\\u5bfc\\u51fa - \"+TID+\"</title>\\n\"+\n" +
		"    \"<style>\\n\"+\n" +
		"    \"  *{margin:0;padding:0;box-sizing:border-box}\\n\"+\n" +
		"    \"  body{font-family:\\u201cMicrosoft YaHei\\u201d,Arial,sans-serif;background:#1a1a2e;color:#eee}\\n\"+\n" +
		"    \"  .slideshow{width:100vw;height:100vh;overflow:hidden;position:relative}\\n\"+\n" +
		"    \"  .slide{display:none;width:100%;height:100%;background:#16213e;padding:40px;position:relative;flex-direction:column;animation:fadeIn .4s ease}\\n\"+\n" +
		"    \"  .slide.active{display:flex}\\n\"+\n" +
		"    \"  @keyframes fadeIn{from{opacity:0;transform:translateY(10px)}to{opacity:1;transform:translateY(0)}}\\n\"+\n" +
		"    \"  .slide-header{display:flex;justify-content:space-between;align-items:center;padding:10px 0;border-bottom:2px solid #1F4E79;margin-bottom:20px}\\n\"+\n" +
		"    \"  .slide-title{font-size:22px;font-weight:bold;color:#4fc3f7}\\n\"+\n" +
		"    \"  .slide-id{font-size:12px;color:#666}\\n\"+\n" +
		"    \"  .slide-content{flex:1;overflow-y:auto;background:#fff;border-radius:8px;padding:30px;color:#222;box-shadow:0 4px 20px rgba(0,0,0,.3)}\\n\"+\n" +
		"    \"  .slide-code{font-family:\\u201cCourier New\\u201d,Consolas,monospace;font-size:14px;line-height:1.8;white-space:pre-wrap;color:#333}\\n\"+\n" +
		"    \"  .slide-footer{text-align:center;padding:15px 0;color:#555;font-size:13px}\\n\"+\n" +
		"    \"  .nav-controls{position:fixed;bottom:30px;left:50%;transform:translateX(-50%);display:flex;gap:15px;align-items:center;z-index:100}\\n\"+\n" +
		"    \"  .nav-btn{background:#1F4E79;color:#fff;border:none;padding:10px 25px;border-radius:25px;cursor:pointer;font-size:14px;font-family:inherit;transition:background .2s}\\n\"+\n" +
		"    \"  .nav-btn:hover{background:#2a6fb0}\\n\"+\n" +
		"    \"  .nav-btn:disabled{background:#444;cursor:not-allowed}\\n\"+\n" +
		"    \"  .page-indicator{color:#fff;font-size:14px;min-width:60px;text-align:center}\\n\"+\n" +
		"    \"  .progress-bar{position:fixed;top:0;left:0;height:4px;background:linear-gradient(90deg,#4fc3f7,#1F4E79);z-index:101;transition:width .3s}\\n\"+\n" +
		"    \"</style>\\n\"+\n" +
		"    \"</head>\\n<body>\\n\"+\n" +
		"    \"<div class=\\\"progress-bar\\\" id=\\\"progressBar\\\"></div>\\n\"+\n" +
		"    \"<div class=\\\"slideshow\\\" id=\\\"slideshow\\\">\\n\"+\n" +
		"    \"+slides_html+\"\\n\"+\n" +
		"    \"</div>\\n\"+\n" +
		"    \"<div class=\\\"nav-controls\\\">\\n\"+\n" +
		"    \"  <button class=\\\"nav-btn\\\" id=\\\"prevBtn\\\" onclick=\\\"changeSlide(-1)\\\">\\u4e0a\\u4e00\\u9875</button>\\n\"+\n" +
		"    \"  <span class=\\\"page-indicator\\\" id=\\\"pageIndicator\\\">1 / \"+str(TP)+\"</span>\\n\"+\n" +
		"    \"  <button class=\\\"nav-btn\\\" id=\\\"nextBtn\\\" onclick=\\\"changeSlide(1)\\\">\\u4e0b\\u4e00\\u9875</button>\\n\"+\n" +
		"    \"</div>\\n\"+\n" +
		"    \"<script>\\n\"+\n" +
		"    \"var cur=0;var sls=document.querySelectorAll(\\\".slide\\\");var tot=sls.length;\\n\"+\n" +
		"    \"var pb=document.getElementById(\\\"progressBar\\\");var pi=document.getElementById(\\\"pageIndicator\\\");\\n\"+\n" +
		"    \"var pr=document.getElementById(\\\"prevBtn\\\");var nx=document.getElementById(\\\"nextBtn\\\");\\n\"+\n" +
		"    \"function ss(n){sls.forEach(function(s){s.classList.remove(\\\"active\\\")});\\n\"+\n" +
		"      if(n<0)n=0;if(n>=tot)n=tot-1;cur=n;sls[cur].classList.add(\\\"active\\\");\\n\"+\n" +
		"      pb.style.width=((cur+1)/tot*100)+\"%\";pi.textContent=(cur+1)+\" / \"+tot;\\n\"+\n" +
		"      pr.disabled=cur===0;nx.disabled=cur===tot-1}\\n\"+\n" +
		"    \"function cs(d){ss(cur+d)}\\n\"+\n" +
		"    \"document.addEventListener(\\\"keydown\\\",function(e){\\n\"+\n" +
		"      if(e.key===\\\"ArrowLeft\\\"||e.key===\\\"ArrowUp\\\")cs(-1);\\n\"+\n" +
		"      if(e.key===\\\"ArrowRight\\\"||e.key===\\\"ArrowDown\\\"||e.key===\\\" \\\")cs(1);\\n\"+\n" +
		"      if(e.key===\\\"Home\\\")ss(0);if(e.key===\\\"End\\\")ss(tot-1)})\\n\"+\n" +
		"    \"document.addEventListener(\\\"click\\\",function(e){\\n\"+\n" +
		"      if(e.target.closest(\\\".nav-controls\\\")||e.target.closest(\\\".slide-content\\\"))return;cs(1)})\\n\"+\n" +
		"    \"ss(0)\\n\"+\n" +
		"    \"</script>\\n</body>\\n</html>\"\n" +
		"  )\n" +
		"  with open(out,\"w\",encoding=\"utf-8\") as f:f.write(html)\n" +
		"  print(json.dumps({\"ok\":True,\"path\":out}))\n\n" +
		"if __name__==\"__main__\":main()\n"

	outputPath := filepath.Join(os.TempDir(), fmt.Sprintf("%s_%s.html", taskID, job.ExportID))
	createdPath, err := s.execPython(ctx, script, outputPath)
	if err != nil {
		return err
	}
	if createdPath != "" {
		outputPath = createdPath
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		return fmt.Errorf("read output: %w", err)
	}

	job.Progress = 60
	_, _ = s.exportRepo.Update(job)

	objectKey := fmt.Sprintf("exports/%s/%s.html", taskID, job.ExportID)
	url, _, uploadErr := s.ossClient.PutObject(ctx, objectKey, data)
	if uploadErr != nil {
		return fmt.Errorf("upload: %w", uploadErr)
	}

	job.DownloadURL = url
	job.FileSize = int64(len(data))
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
