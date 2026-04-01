package infra

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"zcxppt/internal/model"
)

// FileParser performs format-specific extraction on uploaded reference files.
type FileParser struct {
	client       *http.Client
	ocrBaseURL   string
	transBaseURL string
}

func NewFileParser(ocrBaseURL, transBaseURL string) *FileParser {
	return &FileParser{
		client: &http.Client{Timeout: 60 * time.Second},
		ocrBaseURL:   strings.TrimSuffix(strings.TrimSpace(ocrBaseURL), "/"),
		transBaseURL: strings.TrimSuffix(strings.TrimSpace(transBaseURL), "/"),
	}
}

// ParseResult is the structured output of parsing a single reference file.
type ParseResult struct {
	FileID      string            `json:"file_id"`
	FileType    string            `json:"file_type"`
	Instruction string            `json:"instruction"` // echo the original instruction from the caller
	TextContent string            `json:"text_content"`
	StyleHints  map[string]string `json:"style_hints"`
	Metadata    map[string]string `json:"metadata"`
	Error       string            `json:"error,omitempty"`
}

// Parse fetches and parses a single ReferenceFile according to its type.
// fileURL can be a local path, OSS URL, or HTTP URL.
func (p *FileParser) Parse(ctx context.Context, f model.ReferenceFile) ParseResult {
	ft := strings.ToLower(strings.TrimSpace(f.FileType))

	var text, styleMeta string
	var meta map[string]string
	var parseErr error

	switch ft {
	case "pdf":
		text, meta, parseErr = p.parsePDF(ctx, f.FileURL, f.Instruction)
	case "docx":
		text, meta, parseErr = p.parseDOCX(ctx, f.FileURL, f.Instruction)
	case "pptx":
		text, styleMeta, meta, parseErr = p.parsePPTX(ctx, f.FileURL, f.Instruction)
	case "image", "img", "png", "jpg", "jpeg", "gif", "webp":
		text, meta, parseErr = p.parseImage(ctx, f.FileURL, f.Instruction)
	case "video", "mp4", "webm", "mov":
		text, meta, parseErr = p.parseVideo(ctx, f.FileURL, f.Instruction)
	default:
		parseErr = fmt.Errorf("unsupported file type: %s", ft)
	}

	if parseErr != nil {
		return ParseResult{
			FileID: f.FileID,
			FileType: ft,
			Error: parseErr.Error(),
		}
	}

	styleHints := make(map[string]string)
	if styleMeta != "" {
		styleHints["style_guide"] = styleMeta
	}
	for k, v := range meta {
		styleHints[k] = v
	}

	return ParseResult{
		FileID:      f.FileID,
		FileType:    ft,
		TextContent: text,
		StyleHints:  styleHints,
		Metadata:    meta,
	}
}

// resolveFileURL returns the contents of a file URL.
// Supports: http(s)://, file://, oss://, and local absolute paths.
func (p *FileParser) resolveFileURL(ctx context.Context, rawURL string) ([]byte, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, fmt.Errorf("empty file URL")
	}

	// Local file path
	if !strings.Contains(rawURL, "://") {
		return os.ReadFile(rawURL)
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}

	switch u.Scheme {
	case "http", "https":
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			return nil, err
		}
		resp, err := p.client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		return io.ReadAll(resp.Body)

	case "file":
		return os.ReadFile(u.Path)

	default:
		return nil, fmt.Errorf("unsupported URL scheme: %s", u.Scheme)
	}
}

// parsePDF extracts text from a PDF via the configured OCR/transcription service.
// Falls back to basic text extraction if the service is not configured.
func (p *FileParser) parsePDF(ctx context.Context, fileURL, instruction string) (string, map[string]string, error) {
	data, err := p.resolveFileURL(ctx, fileURL)
	if err != nil {
		return "", nil, err
	}

	if p.transBaseURL == "" {
		return p.extractTextFromPDFBytes(data, instruction), nil, nil
	}

	payload := map[string]any{
		"data":        base64.StdEncoding.EncodeToString(data),
		"instruction": instruction,
		"file_type":   "pdf",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.transBaseURL+"/api/v1/parse/pdf", bytes.NewReader(body))
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()

	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return p.extractTextFromPDFBytes(data, instruction), nil, nil
	}

	var env struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(b, &env) == nil && env.Data != nil {
		var out struct {
			Text     string `json:"text"`
			Metadata map[string]string `json:"metadata"`
		}
		if json.Unmarshal(env.Data, &out) == nil {
			return out.Text, out.Metadata, nil
		}
		return strings.TrimSpace(string(env.Data)), nil, nil
	}
	return strings.TrimSpace(string(b)), nil, nil
}

// parseDOCX extracts text from a DOCX via transcription service or direct parsing.
func (p *FileParser) parseDOCX(ctx context.Context, fileURL, instruction string) (string, map[string]string, error) {
	data, err := p.resolveFileURL(ctx, fileURL)
	if err != nil {
		return "", nil, err
	}

	if p.transBaseURL == "" {
		return "", nil, fmt.Errorf("DOCX parsing requires transcription service URL")
	}

	payload := map[string]any{
		"data":        base64.StdEncoding.EncodeToString(data),
		"instruction": instruction,
		"file_type":   "docx",
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.transBaseURL+"/api/v1/parse/docx", bytes.NewReader(body))
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()

	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return "", nil, fmt.Errorf("docx parse failed: %d %s", resp.StatusCode, string(b))
	}

	var env struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(b, &env) == nil && env.Data != nil {
		var out struct {
			Text     string            `json:"text"`
			Metadata map[string]string `json:"metadata"`
		}
		if json.Unmarshal(env.Data, &out) == nil {
			return out.Text, out.Metadata, nil
		}
		return strings.TrimSpace(string(env.Data)), nil, nil
	}
	return strings.TrimSpace(string(b)), nil, nil
}

// parsePPTX extracts text and style/layout hints from a PPTX file.
// Style hints include: master theme colors, font families, slide layouts used,
// and structural patterns (e.g., title+content, two-column).
func (p *FileParser) parsePPTX(ctx context.Context, fileURL, instruction string) (string, string, map[string]string, error) {
	data, err := p.resolveFileURL(ctx, fileURL)
	if err != nil {
		return "", "", nil, err
	}

	if p.transBaseURL == "" {
		// Fallback: call Python-based PPTX parser directly
		return p.parsePPTXWithPython(data, instruction)
	}

	payload := map[string]any{
		"data":        base64.StdEncoding.EncodeToString(data),
		"instruction": instruction,
		"file_type":   "pptx",
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.transBaseURL+"/api/v1/parse/pptx", bytes.NewReader(body))
	if err != nil {
		return "", "", nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return p.parsePPTXWithPython(data, instruction)
	}
	defer resp.Body.Close()

	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return p.parsePPTXWithPython(data, instruction)
	}

	var env struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(b, &env) == nil && env.Data != nil {
		var out struct {
			Text      string            `json:"text"`
			Style     string            `json:"style"`
			Metadata  map[string]string `json:"metadata"`
		}
		if json.Unmarshal(env.Data, &out) == nil {
			return out.Text, out.Style, out.Metadata, nil
		}
	}
	return strings.TrimSpace(string(b)), "", nil, nil
}

// parsePPTXWithPython extracts text and style from PPTX bytes using a Python subprocess.
// This is the fallback path when no transcription service is configured.
func (p *FileParser) parsePPTXWithPython(data []byte, instruction string) (string, string, map[string]string, error) {
	tmpDir := os.TempDir()
	tmpPath := filepath.Join(tmpDir, fmt.Sprintf("ref_%d.pptx", time.Now().UnixNano()))
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return "", "", nil, err
	}
	defer os.Remove(tmpPath)

	parserScript := `
import sys, json, zipfile, xml.etree.ElementTree as ET, re

pptx_path = sys.argv[1]
instruction = sys.argv[2]

try:
    result = {"text": "", "style": "", "metadata": {}}
    with zipfile.ZipFile(pptx_path, 'r') as z:
        # Extract theme colors from theme1.xml
        try:
            theme_xml = z.read("ppt/theme/theme1.xml")
            root = ET.fromstring(theme_xml)
            ns = {"a": "http://schemas.openxmlformats.org/drawingml/2006/main"}
            colors = []
            for clr in root.findall(".//a:dk1//a:srgbClr", ns):
                colors.append(clr.get("val", ""))
            for clr in root.findall(".//a:lt1//a:srgbClr", ns):
                colors.append(clr.get("val", ""))
            if colors:
                result["style"] += f"主色调: {', '.join(c for c in colors if c)}\n"
        except:
            pass

        # Extract text from all slides
        slide_files = sorted([n for n in z.namelist() if re.match(r"ppt/slides/slide\d+\.xml", n)])
        for i, slide_file in enumerate(slide_files):
            slide_xml = z.read(slide_file)
            root = ET.fromstring(slide_xml)
            ns = {
                "a": "http://schemas.openxmlformats.org/drawingml/2006/main",
                "p": "http://schemas.openxmlformats.org/presentationml/2006/main",
                "r": "http://schemas.openxmlformats.org/officeDocument/2006/relationships",
            }
            texts = []
            for t in root.iter("{http://schemas.openxmlformats.org/drawingml/2006/main}t"):
                if t.text and t.text.strip():
                    texts.append(t.text.strip())
            if texts:
                result["text"] += f"[第{i+1}页] " + " | ".join(texts) + "\n"

        # Extract layout names
        layouts = {}
        for rels_file in z.namelist():
            if "slideLayout" in rels_file and rels_file.endswith(".xml.rels"):
                try:
                    rels_xml = z.read(rels_file)
                    rels_root = ET.fromstring(rels_xml)
                    for rel in rels_root.findall(".//{http://schemas.openxmlformats.org/package/2006/relationships}Relationship"):
                        layouts[rels_file] = rel.get("Target", "")
                except:
                    pass
        if layouts:
            unique_layouts = list(set(layouts.values()))
            result["style"] += f"使用的版式: {', '.join(unique_layouts)}\n"

        # Extract font names
        fonts = set()
        for fnt in root.findall(".//{http://schemas.openxmlformats.org/drawingml/2006/main}latin typeface", ns):
            if fnt.get("typeface"):
                fonts.add(fnt.get("typeface"))
        if fonts:
            result["style"] += f"字体: {', '.join(fonts)}\n"

    print(json.dumps(result, ensure_ascii=False))
except Exception as e:
    print(json.dumps({"error": str(e)}))
`

	scriptPath := filepath.Join(tmpDir, fmt.Sprintf("parse_pptx_%d.py", time.Now().UnixNano()))
	if err := os.WriteFile(scriptPath, []byte(parserScript), 0644); err != nil {
		return "", "", nil, err
	}
	defer os.Remove(scriptPath)

	var stdout, stderr bytes.Buffer
	runner := &pythonRunner{}
	outBytes, errBytes, err := runner.Run(scriptPath, tmpPath, instruction)
	if err != nil {
		return "", "", nil, fmt.Errorf("python pptx parser failed: %w", err)
	}
	stdout.Write(outBytes)
	stderr.Write(errBytes)

	var out struct {
		Text     string            `json:"text"`
		Style    string            `json:"style"`
		Metadata map[string]string `json:"metadata"`
	}
	if json.Unmarshal(stdout.Bytes(), &out) != nil {
		return string(stdout.Bytes()), "", nil, nil
	}
	return out.Text, out.Style, out.Metadata, nil
}

// parseImage performs OCR on an image file.
func (p *FileParser) parseImage(ctx context.Context, fileURL, instruction string) (string, map[string]string, error) {
	data, err := p.resolveFileURL(ctx, fileURL)
	if err != nil {
		return "", nil, err
	}

	if p.ocrBaseURL == "" && p.transBaseURL == "" {
		return "", nil, fmt.Errorf("image OCR requires OCR_BASE_URL or TRANS_BASE_URL to be configured")
	}

	baseURL := p.ocrBaseURL
	if baseURL == "" {
		baseURL = p.transBaseURL
	}

	payload := map[string]any{
		"data":        base64.StdEncoding.EncodeToString(data),
		"instruction": instruction,
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/v1/ocr", bytes.NewReader(body))
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()

	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return "", nil, fmt.Errorf("ocr failed: %d %s", resp.StatusCode, string(b))
	}

	var env struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(b, &env) == nil && env.Data != nil {
		var out struct {
			Text     string            `json:"text"`
			Metadata map[string]string `json:"metadata"`
		}
		if json.Unmarshal(env.Data, &out) == nil {
			return out.Text, out.Metadata, nil
		}
		return strings.TrimSpace(string(env.Data)), nil, nil
	}
	return strings.TrimSpace(string(b)), nil, nil
}

// parseVideo extracts keyframes and audio transcription from a video file.
func (p *FileParser) parseVideo(ctx context.Context, fileURL, instruction string) (string, map[string]string, error) {
	data, err := p.resolveFileURL(ctx, fileURL)
	if err != nil {
		return "", nil, err
	}

	if p.transBaseURL == "" {
		return "", nil, fmt.Errorf("video parsing requires TRANS_BASE_URL to be configured")
	}

	payload := map[string]any{
		"data":        base64.StdEncoding.EncodeToString(data),
		"instruction": instruction,
		"file_type":   "video",
		"extract":     []string{"transcript", "keyframes"},
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.transBaseURL+"/api/v1/parse/video", bytes.NewReader(body))
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()

	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return "", nil, fmt.Errorf("video parse failed: %d %s", resp.StatusCode, string(b))
	}

	var env struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(b, &env) == nil && env.Data != nil {
		var out struct {
			Transcript string            `json:"transcript"`
			KeyFrames  []string           `json:"keyframes"`
			Metadata   map[string]string `json:"metadata"`
		}
		if json.Unmarshal(env.Data, &out) == nil {
			meta := out.Metadata
			if meta == nil {
				meta = make(map[string]string)
			}
			return out.Transcript, meta, nil
		}
		return strings.TrimSpace(string(env.Data)), nil, nil
	}
	return strings.TrimSpace(string(b)), nil, nil
}

// extractTextFromPDFBytes is a fallback basic PDF text extractor.
func (p *FileParser) extractTextFromPDFBytes(data []byte, instruction string) string {
	text := string(data)
	text = strings.ReplaceAll(text, "\x00", " ")
	re := regexp.MustCompile(`\((.+?)\)`)
	matches := re.FindAllStringSubmatch(text, -1)
	var out []string
	for _, m := range matches {
		if len(m) > 1 && strings.TrimSpace(m[1]) != "" {
			out = append(out, strings.TrimSpace(m[1]))
		}
	}
	if len(out) > 0 {
		return strings.Join(out, " ")
	}
	return strings.TrimSpace(text)
}

// ParseBatch parses multiple reference files concurrently.
func (p *FileParser) ParseBatch(ctx context.Context, files []model.ReferenceFile) []ParseResult {
	if len(files) == 0 {
		return nil
	}

	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	results := make([]ParseResult, len(files))

	for i, f := range files {
		i := i
		f := f
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i] = p.Parse(ctx, f)
		}()
	}
	wg.Wait()
	return results
}

// pythonRunner wraps subprocess execution of Python scripts.
type pythonRunner struct{}

func (r *pythonRunner) Run(scriptPath string, args ...string) ([]byte, []byte, error) {
	pythonPath := os.Getenv("PYTHON_PATH")
	if pythonPath == "" {
		pythonPath = "python"
	}
	cmd := exec.Command(pythonPath, append([]string{scriptPath}, args...)...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}
	out, _ := io.ReadAll(stdout)
	errOut, _ := io.ReadAll(stderr)
	cmd.Wait()
	if cmd.ProcessState != nil && !cmd.ProcessState.Success() {
		return out, errOut, fmt.Errorf("python exited with code %d: %s", cmd.ProcessState.ExitCode(), string(errOut))
	}
	return out, errOut, nil
}
