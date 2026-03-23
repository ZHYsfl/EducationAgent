package domain

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// TemplateBundle 对应 Python 中 templates/{name}/ 目录（含 source.pptx 与 slide_induction.json）。
type TemplateBundle struct {
	Name            string
	Root            string // .../templates/{Name}
	SourcePPTX      string
	SlideInduction  string // JSON 文件路径
	inductionRaw    json.RawMessage
	layoutRaw       map[string]json.RawMessage
	Language        LanguageInfo
	FunctionalKeys  []string
	LayoutKeys      []string // slide_induction 中除 language/functional_keys 外的顶层 key
}

type LanguageInfo struct {
	LID string `json:"lid"`
}

// LoadTemplateBundle 从 templates 根目录加载某一模板名（如 default）。
func LoadTemplateBundle(templatesRoot, name string) (*TemplateBundle, error) {
	return LoadTemplateBundleWithOption(templatesRoot, name, false)
}

// LoadTemplateBundleWithOption 支持在 stage1 live 模式下允许 slide_induction.json 缺失。
func LoadTemplateBundleWithOption(templatesRoot, name string, allowMissingInduction bool) (*TemplateBundle, error) {
	root := filepath.Join(templatesRoot, name)
	pptx := filepath.Join(root, "source.pptx")
	ind := filepath.Join(root, "slide_induction.json")
	if _, err := os.Stat(pptx); err != nil {
		return nil, fmt.Errorf("source.pptx: %w", err)
	}
	if _, err := os.Stat(ind); err != nil {
		if !allowMissingInduction {
			return nil, fmt.Errorf("slide_induction.json: %w", err)
		}
		return &TemplateBundle{
			Name:           name,
			Root:           root,
			SourcePPTX:     pptx,
			SlideInduction: ind,
			inductionRaw:   []byte("{}"),
			layoutRaw:      map[string]json.RawMessage{},
			FunctionalKeys: nil,
			LayoutKeys:     nil,
		}, nil
	}
	raw, err := os.ReadFile(ind)
	if err != nil {
		return nil, err
	}
	return NewTemplateBundleFromInduction(root, name, pptx, ind, raw)
}

func NewTemplateBundleFromInduction(root, name, pptx, ind string, raw []byte) (*TemplateBundle, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, fmt.Errorf("slide_induction json: %w", err)
	}
	var lang LanguageInfo
	var funcKeys []string
	layoutKeys := make([]string, 0, len(top))
	layoutRaw := make(map[string]json.RawMessage)
	for k, v := range top {
		switch k {
		case "language":
			_ = json.Unmarshal(v, &lang)
		case "functional_keys":
			_ = json.Unmarshal(v, &funcKeys)
		default:
			layoutKeys = append(layoutKeys, k)
			layoutRaw[k] = v
		}
	}
	sort.Strings(layoutKeys)
	return &TemplateBundle{
		Name:           name,
		Root:           root,
		SourcePPTX:     pptx,
		SlideInduction: ind,
		inductionRaw:   raw,
		layoutRaw:      layoutRaw,
		Language:       lang,
		FunctionalKeys: funcKeys,
		LayoutKeys:     layoutKeys,
	}, nil
}

// ReplaceInductionRaw 用新的 slide_induction JSON 覆盖当前模板归纳结果（用于 stage1 live）。
func (b *TemplateBundle) ReplaceInductionRaw(raw []byte) error {
	nb, err := NewTemplateBundleFromInduction(b.Root, b.Name, b.SourcePPTX, b.SlideInduction, raw)
	if err != nil {
		return err
	}
	b.inductionRaw = nb.inductionRaw
	b.layoutRaw = nb.layoutRaw
	b.Language = nb.Language
	b.FunctionalKeys = nb.FunctionalKeys
	b.LayoutKeys = nb.LayoutKeys
	return nil
}

// InductionJSON 返回原始 slide_induction 字节（供调试或与 Python 对比）。
func (b *TemplateBundle) InductionJSON() json.RawMessage { return b.inductionRaw }

// AvailableLayoutsJSON 返回 layout_selector 使用的布局候选 JSON。
func (b *TemplateBundle) AvailableLayoutsJSON() string {
	if b == nil || len(b.layoutRaw) == 0 {
		return "{}"
	}
	out := make(map[string]any, len(b.layoutRaw))
	for k, v := range b.layoutRaw {
		var x any
		if err := json.Unmarshal(v, &x); err == nil {
			out[k] = x
		}
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func normalizeLayoutKey(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	if i := strings.Index(s, ":"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	return s
}

// LayoutSchemaJSON 根据 layout_selector 选中的布局名，返回该布局在 slide_induction 中的 schema JSON。
// 允许模糊匹配：忽略 ":text/:image" 后缀与大小写。
func (b *TemplateBundle) LayoutSchemaJSON(layoutName string) string {
	if b == nil || len(b.layoutRaw) == 0 {
		return "{}"
	}
	keyNorm := normalizeLayoutKey(layoutName)
	if keyNorm == "" {
		return "{}"
	}
	// exact / prefix match
	for k, v := range b.layoutRaw {
		kn := normalizeLayoutKey(k)
		if kn == keyNorm || strings.Contains(kn, keyNorm) || strings.Contains(keyNorm, kn) {
			return string(v)
		}
	}
	return "{}"
}
