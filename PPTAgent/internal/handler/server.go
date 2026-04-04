package handler

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"educationagent/pptagentgo/internal/fullgen/orchestrator"
	"educationagent/pptagentgo/internal/fullgen/pptx"
	"educationagent/pptagentgo/pkg/infer"
)

type InferRequest struct {
	Prompt         string   `json:"prompt"`
	SystemMessage  string   `json:"system_message"`
	ReturnJSON     bool     `json:"return_json"`
	Temperature    *float64 `json:"temperature"`
	MaxTokens      *int     `json:"max_tokens"`
	ImageURLs      []string `json:"image_urls"`
	ImageDetail    string   `json:"image_detail"`
	Model          string   `json:"model"`
	UseVisionModel bool     `json:"use_vision_model"`
}

type InferResponse struct {
	OK      bool   `json:"ok"`
	Content string `json:"content"`
	Error   string `json:"error,omitempty"`
}

type APIResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message,omitempty"`
	Data    any    `json:"data"`
}

func writeOK(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(APIResponse{Code: 200, Message: "success", Data: data})
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(APIResponse{Code: code, Message: msg, Data: nil})
}

type GeneratePresRequest struct {
	RepoRoot          string                   `json:"repo_root,omitempty"`
	TaskDir           string                   `json:"task_dir,omitempty"`
	UserID            string                   `json:"user_id"`
	Topic             string                   `json:"topic"`
	Description       string                   `json:"description"`
	TotalPages        int                      `json:"total_pages"`
	Audience          string                   `json:"audience"`
	GlobalStyle       string                   `json:"global_style"`
	SessionID         string                   `json:"session_id"`
	TemplateName      string                   `json:"template_name,omitempty"`
	TeachingElements  map[string]interface{}   `json:"teaching_elements,omitempty"`
	ReferenceFiles    []map[string]interface{} `json:"reference_files,omitempty"`
	ExtraContext      string                   `json:"extra_context,omitempty"`
	RetrievalTrace    map[string]any           `json:"retrieval_trace,omitempty"`
	ContextInjections []map[string]any         `json:"context_injections,omitempty"`
	Stage1Mode        string                   `json:"stage1_mode,omitempty"`
}

type GeneratePresResponse struct {
	OK                bool                           `json:"ok"`
	SlideHTML         []string                       `json:"slide_html,omitempty"`
	SlideCount        int                            `json:"slide_count"`
	PPTXPath          string                         `json:"pptx_path,omitempty"`
	Outline           any                            `json:"outline,omitempty"`
	Layouts           any                            `json:"layouts,omitempty"`
	ContentPlan       any                            `json:"content_plan,omitempty"`
	CoderPlan         []orchestrator.SlideCoderPlan `json:"coder_plan,omitempty"`
	ArtifactsDir      string                         `json:"artifacts_dir,omitempty"`
	GeneratedPPTXPath string                         `json:"generated_pptx_path,omitempty"`
	AppliedPPTXPath   string                         `json:"applied_pptx_path,omitempty"`
	ImagePlan         any                            `json:"image_plan,omitempty"`
	ImageApplyReport  any                            `json:"image_apply_report,omitempty"`
	Stage1Result      any                            `json:"stage1_result,omitempty"`
	ParityReport      any                            `json:"parity_report,omitempty"`
	Error             string                         `json:"error,omitempty"`
}

func buildSlideHTMLFromPlans(contentPlan any, coderPlan []orchestrator.SlideCoderPlan) []string {
	maxIdx := 0
	for _, cp := range coderPlan {
		if cp.SlideIndex > maxIdx {
			maxIdx = cp.SlideIndex
		}
	}
	if maxIdx == 0 {
		plans := pptx.BuildElementPlansFromContentPlan(contentPlan)
		for _, p := range plans {
			if p.SlideIndex > maxIdx {
				maxIdx = p.SlideIndex
			}
		}
	}
	if maxIdx == 0 {
		return nil
	}
	out := make([]string, maxIdx)
	for _, cp := range coderPlan {
		if cp.SlideIndex <= 0 || cp.SlideIndex > maxIdx {
			continue
		}
		if strings.TrimSpace(cp.HTML) != "" {
			out[cp.SlideIndex-1] = cp.HTML
		}
	}
	plans := pptx.BuildElementPlansFromContentPlan(contentPlan)
	for _, p := range plans {
		if p.SlideIndex <= 0 || p.SlideIndex > maxIdx || strings.TrimSpace(out[p.SlideIndex-1]) != "" {
			continue
		}
		var b strings.Builder
		b.WriteString(`<div style="padding:36px;font-family:'Microsoft YaHei','Segoe UI',sans-serif;width:1280px;height:720px;box-sizing:border-box;">`)
		for _, e := range p.Elements {
			for _, d := range e.Data {
				v := strings.TrimSpace(d)
				if v == "" {
					continue
				}
				b.WriteString(`<p style="margin:0 0 8px 0;line-height:1.5;">`)
				b.WriteString(v)
				b.WriteString(`</p>`)
			}
		}
		b.WriteString(`</div>`)
		out[p.SlideIndex-1] = b.String()
	}
	return out
}

func buildParityReport(stage1Mode string, stage1Result any, outline any, layouts any, contentPlan any, coderPlan []orchestrator.SlideCoderPlan, slideHTML []string, appliedPPTX string) map[string]any {
	rep := map[string]any{"stage1_mode": stage1Mode, "checks": map[string]any{}}
	checks := rep["checks"].(map[string]any)
	checks["stage1_ready"] = stage1Mode != "live" || stage1Result != nil
	checks["outline_ready"] = outline != nil
	checks["layouts_ready"] = layouts != nil
	checks["content_plan_ready"] = contentPlan != nil
	checks["slide_html_ready"] = len(slideHTML) > 0
	checks["pptx_ready"] = strings.TrimSpace(appliedPPTX) != ""

	total := len(coderPlan)
	failed := 0
	failedSlides := make([]int, 0)
	executedCount := 0
	for _, cp := range coderPlan {
		executedCount += len(cp.ExecutedActions)
		if strings.TrimSpace(cp.Error) != "" {
			failed++
			failedSlides = append(failedSlides, cp.SlideIndex)
		}
	}
	successRate := 1.0
	if total > 0 {
		successRate = float64(total-failed) / float64(total)
	}
	rep["coder"] = map[string]any{"total_slides": total, "failed_slides": failedSlides, "failed_count": failed, "success_rate": successRate, "executed_actions_total": executedCount}

	historyTotal := 0
	historyCoveredSlides := 0
	apiCallErrorCount := 0
	commentCount := 0
	for _, cp := range coderPlan {
		if len(cp.ExecutionHistory) > 0 {
			historyCoveredSlides++
		}
		for _, h := range cp.ExecutionHistory {
			s := strings.TrimSpace(h)
			if s == "" {
				continue
			}
			historyTotal++
			if strings.HasPrefix(s, "api_call_error:") {
				apiCallErrorCount++
			}
			if strings.HasPrefix(s, "comment_correct:") || strings.HasPrefix(s, "comment_error:") {
				commentCount++
			}
		}
	}
	historyCoverage := 0.0
	if total > 0 {
		historyCoverage = float64(historyCoveredSlides) / float64(total)
	}
	apiCallErrorRatio := 0.0
	commentNoiseRatio := 0.0
	if historyTotal > 0 {
		apiCallErrorRatio = float64(apiCallErrorCount) / float64(historyTotal)
		commentNoiseRatio = float64(commentCount) / float64(historyTotal)
	}
	rep["executor_metrics"] = map[string]any{"history_coverage": historyCoverage, "api_call_error_ratio": apiCallErrorRatio, "comment_noise_ratio": commentNoiseRatio, "history_total": historyTotal}
	rep["status"] = "ok"
	if total > 0 && failed > 0 {
		rep["status"] = "partial"
	}
	if strings.TrimSpace(appliedPPTX) == "" || len(slideHTML) == 0 {
		rep["status"] = "degraded"
	}
	rep["stage1_metrics"] = map[string]any{"category_coverage_ratio": 0.0, "layout_cluster_count": 0, "schema_completeness": 0.0}
	return rep
}

func saveFullgenArtifacts(taskDir string, outline any, layouts any, contentPlan any, coderPlan []orchestrator.SlideCoderPlan, imagePlan any, imageApplyReport any, retrievalTrace any, contextInjections any, stage1Result any, parityReport any) (string, error) {
	artDir := filepath.Join(taskDir, "fullgen_artifacts")
	if err := os.MkdirAll(artDir, 0o755); err != nil {
		return "", err
	}
	writeJSON := func(name string, v any) error {
		raw, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(artDir, name), raw, 0o644)
	}
	if err := writeJSON("outline.json", outline); err != nil {
		return "", err
	}
	if err := writeJSON("layouts.json", layouts); err != nil {
		return "", err
	}
	if err := writeJSON("content_plan.json", contentPlan); err != nil {
		return "", err
	}
	if err := writeJSON("coder_plan.json", coderPlan); err != nil {
		return "", err
	}
	if imagePlan != nil {
		_ = writeJSON("image_plan.json", imagePlan)
	}
	if imageApplyReport != nil {
		_ = writeJSON("image_apply_report.json", imageApplyReport)
	}
	if retrievalTrace != nil {
		_ = writeJSON("retrieval_trace.json", retrievalTrace)
	}
	if contextInjections != nil {
		_ = writeJSON("context_injections.json", contextInjections)
	}
	if stage1Result != nil {
		_ = writeJSON("stage1_result.json", stage1Result)
	}
	if parityReport != nil {
		_ = writeJSON("parity_report.json", parityReport)
	}
	return artDir, nil
}

func RunFromEnv() error {
	host := strings.TrimSpace(os.Getenv("PPTAGENT_SERVER_HOST"))
	if host == "" {
		host = "0.0.0.0"
	}
	port := 9300
	if p := strings.TrimSpace(os.Getenv("PPTAGENT_SERVER_PORT")); p != "" {
		if v, err := strconv.Atoi(p); err == nil {
			port = v
		}
	}
	ic, err := infer.NewClientFromEnv()
	if err != nil {
		log.Printf("警告: 无法从环境初始化推理客户端（/infer 将失败）: %v", err)
	}

	app := &App{Infer: ic}
	mux := app.Router()

	addr := host + ":" + strconv.Itoa(port)
	log.Printf("PPTAgent_go 监听 %s （POST /api/v1/infer ，POST /api/v1/generate-deck ，POST /api/v1/generate-pres ，POST /v1/chat/completions）", addr)
	return http.ListenAndServe(addr, mux)
}

