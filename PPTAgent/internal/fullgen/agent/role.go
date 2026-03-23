package agent

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// RoleConfig 与 Python pptagent/agent.py 读取的 roles/{name}.yaml 字段对齐（子集）。
type RoleConfig struct {
	SystemPrompt string         `yaml:"system_prompt"`
	Template     string         `yaml:"template"`
	JinjaArgs    []string       `yaml:"jinja_args"`
	UseModel     string         `yaml:"use_model"` // language | vision — 由 Runner 映射到 infer 选项
	ReturnJSON   bool           `yaml:"return_json"`
	RunArgs      map[string]any `yaml:"run_args,omitempty"`
}

// LoadRoleYAML 从目录加载指定角色名（不含 .yaml）。
func LoadRoleYAML(rolesDir, roleName string) (*RoleConfig, error) {
	p := filepath.Join(rolesDir, roleName+".yaml")
	raw, err := os.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("read role %s: %w", p, err)
	}
	var c RoleConfig
	if err := yaml.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("yaml role %s: %w", roleName, err)
	}
	if c.SystemPrompt == "" || c.Template == "" {
		return nil, fmt.Errorf("role %s: missing system_prompt or template", roleName)
	}
	return &c, nil
}
