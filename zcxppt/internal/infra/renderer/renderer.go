package renderer

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
)

type Config struct {
	PythonPath      string // Path to python3/python executable
	ScriptPath      string // Path to render_page.py
	RenderDir       string // Directory to store temporary .pptx files
	RenderURLPrefix string // URL prefix for rendered files (e.g. http://host/renders/)
	TimeoutSeconds  int    // Max time to wait for a render
}

type RenderRequest struct {
	PageIndex    int               `json:"page_index"`
	PageTitle    string            `json:"page_title"`
	OutputPath   string            `json:"output_path"`
	PyCode       string            `json:"py_code"`
	RenderConfig RenderConfig      `json:"render_config"`
}

type RenderConfig struct {
	WidthInches  float64 `json:"width_inches"`
	HeightInches float64 `json:"height_inches"`
	BgColor      string  `json:"bg_color"`
	FontName     string  `json:"font_name"`
}

type RenderResult struct {
	Success   bool   `json:"success"`
	PPTXPath  string `json:"pptx_path"`
	RenderURL string `json:"render_url"`
	Error     string `json:"error"`
}

type Renderer struct {
	cfg        Config
	ossClient  *oss.Client
}

func NewRenderer(ossClient *oss.Client) *Renderer {
	return &Renderer{
		cfg: Config{
			PythonPath:      "python",
			ScriptPath:      "./internal/infra/renderer/render_page.py",
			RenderDir:       "./data/renders",
			RenderURLPrefix: "",
			TimeoutSeconds:  60,
		},
		ossClient: ossClient,
	}
}

func NewRendererWithConfig(cfg Config, ossClient *oss.Client) *Renderer {
	r := &Renderer{cfg: cfg, ossClient: ossClient}
	if r.cfg.PythonPath == "" {
		r.cfg.PythonPath = "python"
	}
	if r.cfg.RenderDir == "" {
		r.cfg.RenderDir = "./data/renders"
	}
	if r.cfg.ScriptPath == "" {
		r.cfg.ScriptPath = "./internal/infra/renderer/render_page.py"
	}
	if r.cfg.TimeoutSeconds == 0 {
		r.cfg.TimeoutSeconds = 60
	}
	return r
}

func (r *Renderer) Configure(cfg Config) {
	if cfg.PythonPath != "" {
		r.cfg.PythonPath = cfg.PythonPath
	}
	if cfg.ScriptPath != "" {
		r.cfg.ScriptPath = cfg.ScriptPath
	}
	if cfg.RenderDir != "" {
		r.cfg.RenderDir = cfg.RenderDir
	}
	if cfg.RenderURLPrefix != "" {
		r.cfg.RenderURLPrefix = cfg.RenderURLPrefix
	}
	if cfg.TimeoutSeconds > 0 {
		r.cfg.TimeoutSeconds = cfg.TimeoutSeconds
	}
}

func (r *Renderer) EnsureRenderDir() error {
	return os.MkdirAll(r.cfg.RenderDir, 0755)
}

// Render executes the Python script to generate a .pptx for a single page.
// It writes the file to r.cfg.RenderDir, then uploads to OSS and returns the signed URL.
func (r *Renderer) Render(ctx context.Context, req RenderRequest) (RenderResult, error) {
	if err := r.EnsureRenderDir(); err != nil {
		return RenderResult{}, fmt.Errorf("ensure render dir: %w", err)
	}

	// Determine output path
	outputPath := req.OutputPath
	if outputPath == "" {
		filename := fmt.Sprintf("page_%d_%d.pptx", req.PageIndex, time.Now().UnixMilli())
		outputPath = filepath.Join(r.cfg.RenderDir, filename)
	}
	outputPath, _ = filepath.Abs(outputPath)

	// Build render config defaults
	cfg := req.RenderConfig
	if cfg.WidthInches == 0 {
		cfg.WidthInches = 10
	}
	if cfg.HeightInches == 0 {
		cfg.HeightInches = 7.5
	}
	if cfg.BgColor == "" {
		cfg.BgColor = "FFFFFF"
	}
	if cfg.FontName == "" {
		cfg.FontName = "Microsoft YaHei"
	}

	payload := RenderRequest{
		PageIndex:    req.PageIndex,
		PageTitle:    req.PageTitle,
		OutputPath:   outputPath,
		PyCode:       req.PyCode,
		RenderConfig: cfg,
	}
	payload.RenderConfig = cfg

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return RenderResult{}, fmt.Errorf("marshal payload: %w", err)
	}

	timeout := time.Duration(r.cfg.TimeoutSeconds) * time.Second
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, r.cfg.PythonPath, r.cfg.ScriptPath)
	cmd.Stdin = bytes.NewReader(payloadJSON)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err = cmd.Run()
	if err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg == "" {
			errMsg = err.Error()
		}
		return RenderResult{
			Success:  false,
			PPTXPath: "",
			Error:    fmt.Sprintf("python script failed: %s", errMsg),
		}, nil
	}

	var result RenderResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		return RenderResult{
			Success: false,
			Error:   fmt.Sprintf("parse python output: %v, raw: %s", err, stdout.String()),
		}, nil
	}

	// If render succeeded, upload the file to OSS
	if result.Success && result.PPTXPath != "" && r.ossClient != nil {
		data, err := os.ReadFile(result.PPTXPath)
		if err == nil {
			relKey := strings.TrimPrefix(result.PPTXPath, r.cfg.RenderDir)
			if relKey == result.PPTXPath {
				relKey = filepath.Base(result.PPTXPath)
			}
			relKey = strings.TrimPrefix(relKey, string(os.PathSeparator))
			objectKey := fmt.Sprintf("renders/%d_%s", time.Now().UnixMilli(), filepath.Base(result.PPTXPath))

			url, _, uploadErr := r.ossClient.PutObject(ctx, objectKey, data)
			if uploadErr == nil {
				result.RenderURL = url
			}
		}
	}

	return result, nil
}

// RenderAll renders all pages in a task and returns their URLs.
func (r *Renderer) RenderAll(ctx context.Context, taskID string, pages []PageData, globalStyle string) (map[string]string, error) {
	results := make(map[string]string)
	for i, page := range pages {
		cfg := RenderConfig{
			WidthInches:  10,
			HeightInches: 7.5,
			BgColor:      page.BgColor,
			FontName:     page.FontName,
		}
		if cfg.BgColor == "" {
			cfg.BgColor = "FFFFFF"
		}
		if cfg.FontName == "" {
			cfg.FontName = "Microsoft YaHei"
		}

		req := RenderRequest{
			PageIndex:    i,
			PageTitle:    page.Title,
			PyCode:       page.PyCode,
			RenderConfig: cfg,
		}

		result, err := r.Render(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("render page %d: %w", i, err)
		}
		if !result.Success {
			return nil, fmt.Errorf("render page %d failed: %s", i, result.Error)
		}
		results[page.PageID] = result.RenderURL
	}
	return results, nil
}

type PageData struct {
	PageID   string
	Title    string
	PyCode   string
	BgColor  string
	FontName string
}

// RenderAndSave renders a page and immediately saves the render_url back to the ppt repository.
func (r *Renderer) RenderAndSave(ctx context.Context, taskID, pageID string, pyCode, pageTitle string, cfg RenderConfig) (string, error) {
	req := RenderRequest{
		PageIndex:    0,
		PageTitle:    pageTitle,
		PyCode:       pyCode,
		RenderConfig: cfg,
	}
	if req.RenderConfig.WidthInches == 0 {
		req.RenderConfig.WidthInches = 10
	}
	if req.RenderConfig.HeightInches == 0 {
		req.RenderConfig.HeightInches = 7.5
	}
	if req.RenderConfig.BgColor == "" {
		req.RenderConfig.BgColor = "FFFFFF"
	}
	if req.RenderConfig.FontName == "" {
		req.RenderConfig.FontName = "Microsoft YaHei"
	}

	result, err := r.Render(ctx, req)
	if err != nil {
		return "", err
	}
	if !result.Success {
		return "", fmt.Errorf("render failed: %s", result.Error)
	}
	return result.RenderURL, nil
}
