package agent

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/nikolalohinski/gonja/v2"
	"github.com/nikolalohinski/gonja/v2/exec"

	"educationagent/pptagentgo/pkg/infer"
)

// LLM 与 deckgen.Completer 类似，便于测试注入 mock。
type LLM interface {
	Complete(ctx context.Context, user, system string, opts *infer.Options) (string, error)
}

// RoleRunner 执行单个 Agent 轮次（渲染 Jinja → 调 LLM）。
type RoleRunner struct {
	LLM LLM
}

// Run 将 vars 的 key 提供给 gonja（与 yaml 中 jinja_args 名称一致，如 outline、schema）。
func (r *RoleRunner) Run(ctx context.Context, cfg *RoleConfig, vars map[string]any) (string, error) {
	if r.LLM == nil || cfg == nil {
		return "", fmt.Errorf("RoleRunner.LLM or cfg nil")
	}
	tpl, err := gonja.FromString(cfg.Template)
	if err != nil {
		return "", fmt.Errorf("gonja parse template: %w", err)
	}
	ctxMap := make(map[string]interface{}, len(vars))
	for k, v := range vars {
		ctxMap[k] = v
	}
	gctx := exec.NewContext(ctxMap)
	var buf bytes.Buffer
	if err := tpl.Execute(&buf, gctx); err != nil {
		return "", fmt.Errorf("gonja execute: %w", err)
	}
	prompt := buf.String()
	opts := &infer.Options{
		JSONMode: cfg.ReturnJSON,
	}
	if cfg.RunArgs != nil {
		if v, ok := cfg.RunArgs["temperature"].(float64); ok {
			opts.Temperature = &v
		}
		switch v := cfg.RunArgs["max_tokens"].(type) {
		case int:
			x := v
			opts.MaxTokens = &x
		case int64:
			x := int(v)
			opts.MaxTokens = &x
		case float64:
			x := int(v)
			opts.MaxTokens = &x
		}
	}
	// use_model: vision — 与 Python 一致：有图时才走视觉模型
	if strings.EqualFold(strings.TrimSpace(cfg.UseModel), "vision") {
		if urls, ok := vars["image_urls"].([]string); ok && len(urls) > 0 {
			opts.UseVisionModel = true
			opts.ImageURLs = urls
		}
	}
	return r.LLM.Complete(ctx, prompt, cfg.SystemPrompt, opts)
}
