package orchestrator

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"educationagent/pptagentgo/internal/fullgen/agent"
	"educationagent/pptagentgo/internal/fullgen/domain"
	"educationagent/pptagentgo/internal/fullgen/pptx"
	"educationagent/pptagentgo/pkg/infer"
)

// Config 纯 Go 全量编排所需根路径（通常指向本仓库内的 PPTAgent/pptagent 子树）。
type Config struct {
	TemplatesRoot string // e.g. .../PPTAgent/pptagent/templates
	RolesRoot     string // e.g. .../PPTAgent/pptagent/roles
}

// ConfigFromEnv 使用 PPTAGENT_TEMPLATES_ROOT、PPTAGENT_ROLES_ROOT；未设置 Roles 时尝试 Templates 的兄弟目录 roles。
func ConfigFromEnv() Config {
	t := strings.TrimSpace(os.Getenv("PPTAGENT_TEMPLATES_ROOT"))
	r := strings.TrimSpace(os.Getenv("PPTAGENT_ROLES_ROOT"))
	if r == "" && t != "" {
		r = filepath.Join(filepath.Dir(t), "roles")
	}
	return Config{TemplatesRoot: t, RolesRoot: r}
}

// Orchestrator 组装模板元数据、PPTX 引擎与 Role 推理（后续在此扩展 generate_pres 各阶段）。
type Orchestrator struct {
	Config Config
	Infer  *infer.Client
	Runner agent.RoleRunner
}

// New 创建编排器；Infer 可为 nil（仅做模板/PPTX 不调用模型）。
func New(cfg Config, ic *infer.Client) *Orchestrator {
	return &Orchestrator{
		Config: cfg,
		Infer:  ic,
		Runner: agent.RoleRunner{LLM: ic},
	}
}

func (o *Orchestrator) LoadTemplate(name string) (*domain.TemplateBundle, error) {
	return domain.LoadTemplateBundle(o.Config.TemplatesRoot, name)
}

func (o *Orchestrator) LoadTemplateWithOption(name string, allowMissingInduction bool) (*domain.TemplateBundle, error) {
	return domain.LoadTemplateBundleWithOption(o.Config.TemplatesRoot, name, allowMissingInduction)
}

// LoadTemplateWithOption 供 cmd 层以函数形式调用（避免改动过多调用点）。
func LoadTemplateWithOption(o *Orchestrator, name string, allowMissingInduction bool) (*domain.TemplateBundle, error) {
	if o == nil {
		return nil, errors.New("orchestrator nil")
	}
	return o.LoadTemplateWithOption(name, allowMissingInduction)
}

// RoundTripSourcePPTX 将模板内 source.pptx 经 gopptx 另存为 dst（基线校验）。
func (o *Orchestrator) RoundTripSourcePPTX(bundle *domain.TemplateBundle, dst string) error {
	return pptx.RoundTrip(bundle.SourcePPTX, dst)
}

// RunRole 使用与 Python 相同的 roles/{name}.yaml（Jinja 由 gonja 执行）。
func (o *Orchestrator) RunRole(ctx context.Context, roleName string, vars map[string]any) (string, error) {
	if o.Infer == nil {
		return "", errors.New("infer client nil: 请先 NewClientFromEnv 或注入 *infer.Client")
	}
	rc, err := agent.LoadRoleYAML(o.Config.RolesRoot, roleName)
	if err != nil {
		return "", err
	}
	return o.Runner.Run(ctx, rc, vars)
}
