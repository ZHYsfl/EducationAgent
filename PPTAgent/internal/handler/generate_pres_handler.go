package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"educationagent/pptagentgo/internal/fullgen/orchestrator"
)

func (a *App) handleGeneratePres(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method", http.StatusMethodNotAllowed)
		return
	}
	var body GeneratePresRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 40001, "invalid json")
		return
	}
	if strings.TrimSpace(body.Topic) == "" || strings.TrimSpace(body.Description) == "" {
		writeErr(w, 40001, "topic/description 不能为空")
		return
	}
	absTaskDir := strings.TrimSpace(body.TaskDir)
	if absTaskDir == "" {
		root := strings.TrimSpace(body.RepoRoot)
		if root == "" {
			root = strings.TrimSpace(os.Getenv("PPTAGENT_REPO_ROOT"))
		}
		if root == "" {
			root, _ = os.Getwd()
		}
		absTaskDir = filepath.Join(root, "runs", "pres_"+strconv.FormatInt(time.Now().UnixMilli(), 10))
	}
	_ = os.MkdirAll(absTaskDir, 0o755)

	cfg := orchestrator.ConfigFromEnv()
	repoRoot := strings.TrimSpace(body.RepoRoot)
	if repoRoot == "" {
		repoRoot = strings.TrimSpace(os.Getenv("PPTAGENT_REPO_ROOT"))
	}
	if cfg.TemplatesRoot == "" && repoRoot != "" {
		cfg.TemplatesRoot = filepath.Join(repoRoot, "PPTAgent", "pptagent", "templates")
	}
	if cfg.RolesRoot == "" && cfg.TemplatesRoot != "" {
		cfg.RolesRoot = filepath.Join(filepath.Dir(cfg.TemplatesRoot), "roles")
	}
	if strings.TrimSpace(cfg.TemplatesRoot) == "" {
		writeErr(w, 40001, "未配置 PPTAGENT_TEMPLATES_ROOT，且无法从 repo_root 推导")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Minute)
	defer cancel()
	orch := orchestrator.New(cfg, a.Infer)
	templateName := strings.TrimSpace(body.TemplateName)
	if templateName == "" {
		templateName = "default"
	}
	allowMissingInduction := strings.ToLower(strings.TrimSpace(body.Stage1Mode)) == "live"
	bundle, err := orchestrator.LoadTemplateWithOption(orch, templateName, allowMissingInduction)
	if err != nil {
		writeErr(w, 50000, "加载模板失败: "+err.Error())
		return
	}
	var stage1Result any
	if strings.ToLower(strings.TrimSpace(body.Stage1Mode)) == "live" {
		if res, e := orch.RunStage1Live(ctx, bundle); e == nil && res != nil {
			_ = bundle.ReplaceInductionRaw(res.Induction)
			stage1Result = res
		} else {
			writeErr(w, 50000, "stage1 live 失败: "+e.Error())
			return
		}
	}
	docOverview := strings.TrimSpace(body.Description)
	outline, _ := orch.GenerateOutline(ctx, orchestrator.GenerateOutlineInput{NumSlides: body.TotalPages, DocumentOverview: docOverview})
	outline = orch.AddFunctionalLayouts(bundle, outline, body.TotalPages)
	layouts, _ := orch.SelectLayoutsForOutline(ctx, bundle, outline, docOverview)
	metadata := fmt.Sprintf(`{"topic":%q,"audience":%q,"global_style":%q}`, body.Topic, body.Audience, body.GlobalStyle)
	contentPlan, _ := orch.PlanSlideContents(ctx, bundle, outline, layouts, metadata, docOverview)
	coderPlan, _ := orch.BuildCoderPlans(ctx, contentPlan)
	pptxPath := filepath.Join(absTaskDir, "output.pptx")
	if err := orch.RoundTripSourcePPTX(bundle, pptxPath); err != nil {
		writeErr(w, 50000, "生成 PPTX 失败: "+err.Error())
		return
	}
	slideHTML := buildSlideHTMLFromPlans(contentPlan, coderPlan)
	parityReport := buildParityReport(strings.ToLower(strings.TrimSpace(body.Stage1Mode)), stage1Result, outline, layouts, contentPlan, coderPlan, slideHTML, "")
	artDir, _ := saveFullgenArtifacts(absTaskDir, outline, layouts, contentPlan, coderPlan, nil, nil, body.RetrievalTrace, body.ContextInjections, stage1Result, parityReport)
	writeOK(w, GeneratePresResponse{
		OK:           true,
		SlideHTML:    slideHTML,
		SlideCount:   len(slideHTML),
		PPTXPath:     pptxPath,
		Outline:      outline,
		Layouts:      layouts,
		ContentPlan:  contentPlan,
		CoderPlan:    coderPlan,
		ArtifactsDir: artDir,
		Stage1Result: stage1Result,
		ParityReport: parityReport,
	})
}

