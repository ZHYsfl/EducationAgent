package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"educationagent/pptagentgo/internal/fullgen/domain"
)

type DocumentIndex struct {
	Section     string   `json:"section"`
	Subsections []string `json:"subsections"`
}

type OutlineItem struct {
	Purpose string          `json:"purpose"`
	Topic   string          `json:"topic"`
	Indexes []DocumentIndex `json:"indexes"`
	Images  []string        `json:"images,omitempty"`
}

type GenerateOutlineInput struct {
	NumSlides        int
	DocumentOverview string
}

type LayoutSelection struct {
	SlideIndex int    `json:"slide_index"`
	Layout     string `json:"layout"`
	Reasoning  string `json:"reasoning,omitempty"`
}

type SlideElement struct {
	Name string   `json:"name"`
	Data []string `json:"data"`
}

type SlideContentPlan struct {
	SlideIndex int            `json:"slide_index"`
	Layout     string         `json:"layout"`
	Elements   []SlideElement `json:"elements"`
	Error      string         `json:"error,omitempty"`
}

type SlideCoderPlan struct {
	SlideIndex      int      `json:"slide_index"`
	Layout          string   `json:"layout"`
	Commands        string   `json:"commands,omitempty"`
	PyCode          string   `json:"py_code,omitempty"`
	HTML            string   `json:"html,omitempty"`
	ExecutedActions []string `json:"executed_actions,omitempty"`
	ExecutionHistory []string `json:"execution_history,omitempty"`
	FailedIndex     int      `json:"failed_index,omitempty"`
	FailedCommand   string   `json:"failed_command,omitempty"`
	PartialActions  []string `json:"partial_actions,omitempty"`
	Error           string   `json:"error,omitempty"`
}

var reJSONObj = regexp.MustCompile(`\{[\s\S]*\}`)
var reAPICall = regexp.MustCompile(`(?m)\b(replace_span|replace_image|clone_paragraph|del_paragraph|del_image)\s*\(`)
var reAnyAPICall = regexp.MustCompile(`(?s)(replace_span|replace_image|clone_paragraph|del_paragraph|del_image)\s*\((.*?)\)`)
var reFailedIndex = regexp.MustCompile(`failed_index=(\d+)`)
var reFailedCommand = regexp.MustCompile("failed_command=`([^`]*)`")

type editableNode struct {
	ID      int
	Name    string
	IsImage bool
	Value   string
}

type execFailure struct {
	Index   int
	Command string
}

func extractFailedIndex(err error) int {
	if err == nil {
		return 0
	}
	m := reFailedIndex.FindStringSubmatch(err.Error())
	if len(m) < 2 {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(m[1]))
	return n
}

func extractFailedCommand(err error) string {
	if err == nil {
		return ""
	}
	m := reFailedCommand.FindStringSubmatch(err.Error())
	if len(m) < 2 {
		return ""
	}
	return strings.TrimSpace(m[1])
}

func parseOutlineJSON(raw string) ([]OutlineItem, error) {
	s := strings.TrimSpace(raw)
	if strings.HasPrefix(s, "```") {
		lines := strings.Split(s, "\n")
		if len(lines) > 0 && strings.HasPrefix(lines[0], "```") {
			lines = lines[1:]
		}
		for len(lines) > 0 && strings.HasPrefix(strings.TrimSpace(lines[len(lines)-1]), "```") {
			lines = lines[:len(lines)-1]
		}
		s = strings.TrimSpace(strings.Join(lines, "\n"))
	}
	var top struct {
		Outline []OutlineItem `json:"outline"`
	}
	if err := json.Unmarshal([]byte(s), &top); err == nil && len(top.Outline) > 0 {
		return top.Outline, nil
	}
	if sub := reJSONObj.FindString(s); sub != "" {
		if err := json.Unmarshal([]byte(sub), &top); err == nil && len(top.Outline) > 0 {
			return top.Outline, nil
		}
	}
	return nil, fmt.Errorf("planner output does not contain valid outline JSON")
}

func hasFunctional(bundle *domain.TemplateBundle, key string) bool {
	for _, k := range bundle.FunctionalKeys {
		if strings.EqualFold(strings.TrimSpace(k), key) {
			return true
		}
	}
	return false
}

// AddFunctionalLayouts 对齐 Python _add_functional_layouts 的最小版本：
// 在模板声明 functional_keys 时插入 opening/ending，并裁剪到 targetSlides。
func (o *Orchestrator) AddFunctionalLayouts(bundle *domain.TemplateBundle, outline []OutlineItem, targetSlides int) []OutlineItem {
	out := make([]OutlineItem, 0, len(outline)+2)
	if bundle != nil && hasFunctional(bundle, "opening") {
		out = append(out, OutlineItem{
			Purpose: "opening",
			Topic:   "Functional",
		})
	}
	out = append(out, outline...)
	if bundle != nil && hasFunctional(bundle, "ending") {
		out = append(out, OutlineItem{
			Purpose: "ending",
			Topic:   "Functional",
		})
	}
	if targetSlides > 0 && len(out) > targetSlides {
		return out[:targetSlides]
	}
	return out
}

// GenerateOutline 调用 planner.yaml（纯 Go）并返回结构化 outline。
func (o *Orchestrator) GenerateOutline(ctx context.Context, in GenerateOutlineInput) ([]OutlineItem, error) {
	if in.NumSlides <= 0 {
		in.NumSlides = 8
	}
	raw, err := o.RunRole(ctx, "planner", map[string]any{
		"num_slides":        in.NumSlides,
		"document_overview": in.DocumentOverview,
	})
	if err != nil {
		return nil, err
	}
	return parseOutlineJSON(raw)
}

func summarizeOutline(outline []OutlineItem) string {
	var b strings.Builder
	for i, it := range outline {
		b.WriteString(fmt.Sprintf("Slide %d: [%s] %s\n", i+1, it.Topic, it.Purpose))
	}
	return strings.TrimSpace(b.String())
}

func parseLayoutSelection(raw string) (layout string, reasoning string) {
	s := strings.TrimSpace(raw)
	if strings.HasPrefix(s, "```") {
		lines := strings.Split(s, "\n")
		if len(lines) > 0 && strings.HasPrefix(lines[0], "```") {
			lines = lines[1:]
		}
		for len(lines) > 0 && strings.HasPrefix(strings.TrimSpace(lines[len(lines)-1]), "```") {
			lines = lines[:len(lines)-1]
		}
		s = strings.TrimSpace(strings.Join(lines, "\n"))
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		if sub := reJSONObj.FindString(s); sub != "" {
			_ = json.Unmarshal([]byte(sub), &m)
		}
	}
	if m != nil {
		layout = strings.TrimSpace(fmt.Sprint(m["layout"]))
		reasoning = strings.TrimSpace(fmt.Sprint(m["reasoning"]))
	}
	if layout == "" {
		layout = "unknown"
	}
	return layout, reasoning
}

func parseEditorElements(raw string) []SlideElement {
	s := strings.TrimSpace(raw)
	if strings.HasPrefix(s, "```") {
		lines := strings.Split(s, "\n")
		if len(lines) > 0 && strings.HasPrefix(lines[0], "```") {
			lines = lines[1:]
		}
		for len(lines) > 0 && strings.HasPrefix(strings.TrimSpace(lines[len(lines)-1]), "```") {
			lines = lines[:len(lines)-1]
		}
		s = strings.TrimSpace(strings.Join(lines, "\n"))
	}
	var top struct {
		Elements []SlideElement `json:"elements"`
	}
	if err := json.Unmarshal([]byte(s), &top); err == nil && len(top.Elements) > 0 {
		return top.Elements
	}
	if sub := reJSONObj.FindString(s); sub != "" {
		_ = json.Unmarshal([]byte(sub), &top)
	}
	return top.Elements
}

func wrapPyCode(html string) string {
	escaped, _ := json.Marshal(html)
	return "# Auto-generated by PPTAgent_go fullgen coder\n\n" +
		"def get_slide_markup() -> str:\n" +
		"    \"\"\"Return slide HTML markup for canvas / renderer.\"\"\"\n" +
		"    return " + string(escaped) + "\n"
}

func renderHTMLFromElements(layout string, elems []SlideElement) string {
	var b strings.Builder
	b.WriteString(`<div style="padding:36px;font-family:'Microsoft YaHei','Segoe UI',sans-serif;width:1280px;height:720px;box-sizing:border-box;">`)
	b.WriteString(`<h2 style="margin:0 0 18px 0;">`)
	if layout != "" {
		b.WriteString(layout)
	} else {
		b.WriteString("Slide")
	}
	b.WriteString(`</h2>`)
	for _, e := range elems {
		if len(e.Data) == 0 {
			continue
		}
		b.WriteString(`<div style="margin-bottom:12px;">`)
		if e.Name != "" {
			b.WriteString(`<div style="font-weight:600;margin-bottom:4px;">`)
			b.WriteString(e.Name)
			b.WriteString(`</div>`)
		}
		for _, d := range e.Data {
			b.WriteString(`<p style="margin:0 0 6px 0;line-height:1.5;">`)
			b.WriteString(d)
			b.WriteString(`</p>`)
		}
		b.WriteString(`</div>`)
	}
	b.WriteString(`</div>`)
	return b.String()
}

func buildEditableTarget(layout string, elems []SlideElement) (string, []editableNode) {
	nodes := make([]editableNode, 0, len(elems)*2)
	nextID := 1
	var b strings.Builder
	b.WriteString(`<div style="padding:36px;font-family:'Microsoft YaHei','Segoe UI',sans-serif;width:1280px;height:720px;box-sizing:border-box;">`)
	b.WriteString(`<h2 data-node-id="`)
	b.WriteString(strconv.Itoa(nextID))
	b.WriteString(`" style="margin:0 0 18px 0;">`)
	if layout != "" {
		b.WriteString(layout)
	} else {
		b.WriteString("Slide")
	}
	b.WriteString(`</h2>`)
	nodes = append(nodes, editableNode{ID: nextID, Name: "layout_title", Value: layout})
	nextID++
	for _, e := range elems {
		b.WriteString(`<div style="margin-bottom:12px;">`)
		if e.Name != "" {
			b.WriteString(`<div style="font-weight:600;margin-bottom:4px;">`)
			b.WriteString(e.Name)
			b.WriteString(`</div>`)
		}
		for _, d := range e.Data {
			val := strings.TrimSpace(d)
			if val == "" {
				continue
			}
			lname := strings.ToLower(strings.TrimSpace(e.Name))
			isImg := strings.Contains(lname, "image") || strings.Contains(lname, "img") || strings.Contains(lname, "logo") || strings.HasPrefix(strings.ToLower(val), "http://") || strings.HasPrefix(strings.ToLower(val), "https://")
			if isImg {
				b.WriteString(`<img data-node-id="`)
				b.WriteString(strconv.Itoa(nextID))
				b.WriteString(`" alt="`)
				b.WriteString(e.Name)
				b.WriteString(`" src="`)
				b.WriteString(val)
				b.WriteString(`" style="max-width:100%;max-height:280px;display:block;margin:6px 0;" />`)
				nodes = append(nodes, editableNode{ID: nextID, Name: e.Name, IsImage: true, Value: val})
			} else {
				b.WriteString(`<p data-node-id="`)
				b.WriteString(strconv.Itoa(nextID))
				b.WriteString(`" style="margin:0 0 6px 0;line-height:1.5;">`)
				b.WriteString(val)
				b.WriteString(`</p>`)
				nodes = append(nodes, editableNode{ID: nextID, Name: e.Name, IsImage: false, Value: val})
			}
			nextID++
		}
		b.WriteString(`</div>`)
	}
	b.WriteString(`</div>`)
	return b.String(), nodes
}

func buildCoderCommandList(cp SlideContentPlan) string {
	// 最小命令集：把每个 element 的首条文本映射成 replace_span 命令。
	type cmd struct {
		Element string `json:"element"`
		Action  string `json:"action"`
		Value   string `json:"value"`
	}
	cmds := make([]cmd, 0, len(cp.Elements))
	for _, e := range cp.Elements {
		v := ""
		if len(e.Data) > 0 {
			v = e.Data[0]
		}
		cmds = append(cmds, cmd{
			Element: e.Name,
			Action:  "replace_span",
			Value:   v,
		})
	}
	raw, _ := json.Marshal(cmds)
	return string(raw)
}

func applyCoderActionsToNodes(nodes []editableNode, actions string) ([]editableNode, []string, []string, *execFailure, error) {
	out := make([]editableNode, len(nodes))
	copy(out, nodes)
	executed := make([]string, 0, 8)
	history := make([]string, 0, 16)
	shortCmd := func(s string) string {
		s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
		const maxLen = 96
		if len(s) > maxLen {
			return s[:maxLen] + "..."
		}
		return s
	}
	cmdErr := func(idx int, rawCmd string, err error) (*execFailure, error) {
		f := &execFailure{Index: idx, Command: shortCmd(rawCmd)}
		return f, fmt.Errorf("cmd[%d] `%s` failed: %w", idx, f.Command, err)
	}
	rebuildIndex := func(xs []editableNode) map[int]int {
		m := make(map[int]int, len(xs))
		for i, n := range xs {
			m[n.ID] = i
		}
		return m
	}
	maxID := func(xs []editableNode) int {
		m := 0
		for _, n := range xs {
			if n.ID > m {
				m = n.ID
			}
		}
		return m
	}
	splitArgs := func(s string) []string {
		s = strings.TrimSpace(s)
		if s == "" {
			return nil
		}
		outArgs := make([]string, 0, 2)
		var b strings.Builder
		var quote rune
		escaped := false
		for _, r := range s {
			if escaped {
				b.WriteRune(r)
				escaped = false
				continue
			}
			if r == '\\' {
				escaped = true
				b.WriteRune(r)
				continue
			}
			if quote != 0 {
				if r == quote {
					quote = 0
				}
				b.WriteRune(r)
				continue
			}
			if r == '"' || r == '\'' {
				quote = r
				b.WriteRune(r)
				continue
			}
			if r == ',' {
				outArgs = append(outArgs, strings.TrimSpace(b.String()))
				b.Reset()
				continue
			}
			b.WriteRune(r)
		}
		if strings.TrimSpace(b.String()) != "" {
			outArgs = append(outArgs, strings.TrimSpace(b.String()))
		}
		return outArgs
	}
	unquote := func(s string) (string, error) {
		s = strings.TrimSpace(s)
		if len(s) >= 2 && strings.HasPrefix(s, `"`) && strings.HasSuffix(s, `"`) {
			v, err := strconv.Unquote(s)
			if err == nil {
				return v, nil
			}
			return s[1 : len(s)-1], nil
		}
		if len(s) >= 2 && strings.HasPrefix(s, `'`) && strings.HasSuffix(s, `'`) {
			raw := `"` + strings.ReplaceAll(s[1:len(s)-1], `"`, `\"`) + `"`
			v, err := strconv.Unquote(raw)
			if err == nil {
				return v, nil
			}
			return s[1 : len(s)-1], nil
		}
		return s, nil
	}
	byID := rebuildIndex(out)
	a := stripCodeFence(actions)
	lines := strings.Split(a, "\n")
	commandMode := "" // clone | del
	cmdNo := 0
	for _, ln := range lines {
		line := strings.TrimSpace(ln)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			history = append(history, "comment_correct:"+line)
			continue
		}
		if strings.HasPrefix(line, "def ") {
			f, e := cmdErr(cmdNo+1, line, fmt.Errorf("function definition not allowed"))
			history = append(history, "api_call_error:"+line)
			return out, executed, history, f, e
		}
		m := reAnyAPICall.FindStringSubmatch(line)
		if len(m) < 3 {
			history = append(history, "comment_error:"+line)
			continue
		}
		cmdNo++
		rawCmd := ""
		if len(m) > 0 {
			rawCmd = m[0]
		}
		fn := strings.TrimSpace(m[1])
		args := strings.TrimSpace(m[2])
		parts := splitArgs(args)
		if len(parts) == 0 {
			f, e := cmdErr(cmdNo, rawCmd, fmt.Errorf("%s missing args", fn))
			history = append(history, "api_call_error:"+rawCmd)
			return out, executed, history, f, e
		}
		id, err := strconv.Atoi(strings.TrimSpace(parts[0]))
		if err != nil {
			f, e := cmdErr(cmdNo, rawCmd, fmt.Errorf("%s invalid node_id: %v", fn, err))
			history = append(history, "api_call_error:"+rawCmd)
			return out, executed, history, f, e
		}
		idx, ok := byID[id]
		if !ok {
			f, e := cmdErr(cmdNo, rawCmd, fmt.Errorf("%s node_id not found: %d", fn, id))
			history = append(history, "api_call_error:"+rawCmd)
			return out, executed, history, f, e
		}
		switch fn {
		case "replace_span":
			if len(parts) < 2 {
				f, e := cmdErr(cmdNo, rawCmd, fmt.Errorf("replace_span missing text"))
				history = append(history, "api_call_error:"+rawCmd)
				return out, executed, history, f, e
			}
			if out[idx].IsImage {
				f, e := cmdErr(cmdNo, rawCmd, fmt.Errorf("replace_span targets image node_id: %d", id))
				history = append(history, "api_call_error:"+rawCmd)
				return out, executed, history, f, e
			}
			v, err := unquote(parts[1])
			if err != nil {
				f, e := cmdErr(cmdNo, rawCmd, fmt.Errorf("replace_span invalid text: %v", err))
				history = append(history, "api_call_error:"+rawCmd)
				return out, executed, history, f, e
			}
			if strings.TrimSpace(v) == "" {
				f, e := cmdErr(cmdNo, rawCmd, fmt.Errorf("replace_span empty text"))
				history = append(history, "api_call_error:"+rawCmd)
				return out, executed, history, f, e
			}
			out[idx].Value = v
			executed = append(executed, fmt.Sprintf("replace_span(%d)", id))
			history = append(history, "api_call_correct:"+rawCmd)
		case "replace_image":
			if len(parts) < 2 {
				f, e := cmdErr(cmdNo, rawCmd, fmt.Errorf("replace_image missing path"))
				history = append(history, "api_call_error:"+rawCmd)
				return out, executed, history, f, e
			}
			if !out[idx].IsImage {
				f, e := cmdErr(cmdNo, rawCmd, fmt.Errorf("replace_image targets non-image node_id: %d", id))
				history = append(history, "api_call_error:"+rawCmd)
				return out, executed, history, f, e
			}
			v, err := unquote(parts[1])
			if err != nil {
				f, e := cmdErr(cmdNo, rawCmd, fmt.Errorf("replace_image invalid path: %v", err))
				history = append(history, "api_call_error:"+rawCmd)
				return out, executed, history, f, e
			}
			out[idx].Value = v
			executed = append(executed, fmt.Sprintf("replace_image(%d)", id))
			history = append(history, "api_call_correct:"+rawCmd)
		case "clone_paragraph":
			if commandMode == "del" {
				f, e := cmdErr(cmdNo, rawCmd, fmt.Errorf("cannot mix clone_* and del_* in one command batch"))
				history = append(history, "api_call_error:"+rawCmd)
				return out, executed, history, f, e
			}
			commandMode = "clone"
			if out[idx].IsImage {
				f, e := cmdErr(cmdNo, rawCmd, fmt.Errorf("clone_paragraph targets image node_id: %d", id))
				history = append(history, "api_call_error:"+rawCmd)
				return out, executed, history, f, e
			}
			dup := out[idx]
			dup.ID = maxID(out) + 1
			insertAt := idx + 1
			if insertAt >= len(out) {
				out = append(out, dup)
			} else {
				out = append(out[:insertAt], append([]editableNode{dup}, out[insertAt:]...)...)
			}
			byID = rebuildIndex(out)
			executed = append(executed, fmt.Sprintf("clone_paragraph(%d)->%d", id, dup.ID))
			history = append(history, "api_call_correct:"+rawCmd)
		case "del_paragraph":
			if commandMode == "clone" {
				f, e := cmdErr(cmdNo, rawCmd, fmt.Errorf("cannot mix clone_* and del_* in one command batch"))
				history = append(history, "api_call_error:"+rawCmd)
				return out, executed, history, f, e
			}
			commandMode = "del"
			if out[idx].IsImage {
				f, e := cmdErr(cmdNo, rawCmd, fmt.Errorf("del_paragraph targets image node_id: %d", id))
				history = append(history, "api_call_error:"+rawCmd)
				return out, executed, history, f, e
			}
			out[idx].Value = ""
			executed = append(executed, fmt.Sprintf("del_paragraph(%d)", id))
			history = append(history, "api_call_correct:"+rawCmd)
		case "del_image":
			if commandMode == "clone" {
				f, e := cmdErr(cmdNo, rawCmd, fmt.Errorf("cannot mix clone_* and del_* in one command batch"))
				history = append(history, "api_call_error:"+rawCmd)
				return out, executed, history, f, e
			}
			commandMode = "del"
			if !out[idx].IsImage {
				f, e := cmdErr(cmdNo, rawCmd, fmt.Errorf("del_image targets non-image node_id: %d", id))
				history = append(history, "api_call_error:"+rawCmd)
				return out, executed, history, f, e
			}
			out[idx].Value = ""
			executed = append(executed, fmt.Sprintf("del_image(%d)", id))
			history = append(history, "api_call_correct:"+rawCmd)
		default:
			f, e := cmdErr(cmdNo, rawCmd, fmt.Errorf("unsupported api call: %s", fn))
			history = append(history, "api_call_error:"+rawCmd)
			return out, executed, history, f, e
		}
	}
	if len(executed) == 0 {
		return out, executed, history, nil, fmt.Errorf("no executable actions parsed")
	}
	return out, executed, history, nil, nil
}

func renderNodesHTML(layout string, nodes []editableNode) string {
	var b strings.Builder
	b.WriteString(`<div style="padding:36px;font-family:'Microsoft YaHei','Segoe UI',sans-serif;width:1280px;height:720px;box-sizing:border-box;">`)
	for _, n := range nodes {
		if n.ID == 1 {
			b.WriteString(`<h2 data-node-id="1" style="margin:0 0 18px 0;">`)
			if strings.TrimSpace(n.Value) != "" {
				b.WriteString(n.Value)
			} else if layout != "" {
				b.WriteString(layout)
			} else {
				b.WriteString("Slide")
			}
			b.WriteString(`</h2>`)
			continue
		}
		if n.IsImage {
			if strings.TrimSpace(n.Value) == "" {
				continue
			}
			b.WriteString(`<img data-node-id="`)
			b.WriteString(strconv.Itoa(n.ID))
			b.WriteString(`" alt="`)
			b.WriteString(n.Name)
			b.WriteString(`" src="`)
			b.WriteString(n.Value)
			b.WriteString(`" style="max-width:100%;max-height:280px;display:block;margin:6px 0;" />`)
		} else {
			if strings.TrimSpace(n.Value) == "" {
				continue
			}
			b.WriteString(`<p data-node-id="`)
			b.WriteString(strconv.Itoa(n.ID))
			b.WriteString(`" style="margin:0 0 6px 0;line-height:1.5;">`)
			b.WriteString(n.Value)
			b.WriteString(`</p>`)
		}
	}
	b.WriteString(`</div>`)
	return b.String()
}

func stripCodeFence(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	lines := strings.Split(s, "\n")
	if len(lines) > 0 && strings.HasPrefix(strings.TrimSpace(lines[0]), "```") {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.HasPrefix(strings.TrimSpace(lines[len(lines)-1]), "```") {
		lines = lines[:len(lines)-1]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func validateCoderActions(actions string) error {
	a := stripCodeFence(actions)
	if strings.TrimSpace(a) == "" {
		return fmt.Errorf("coder returned empty actions")
	}
	matches := reAPICall.FindAllStringSubmatch(a, -1)
	if len(matches) == 0 {
		return fmt.Errorf("coder output does not contain supported api calls")
	}
	allowed := map[string]struct{}{
		"replace_span": {}, "replace_image": {}, "clone_paragraph": {}, "del_paragraph": {}, "del_image": {},
	}
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		if _, ok := allowed[strings.TrimSpace(m[1])]; !ok {
			return fmt.Errorf("unsupported api call: %s", m[1])
		}
	}
	return nil
}

type coderExecuteResult struct {
	HTML            string
	ExecutedActions []string
	ExecutionHistory []string
	Failure         *execFailure
	Err             error
}

func (o *Orchestrator) runCoderWithRetry(
	ctx context.Context,
	editTarget,
	cmdList,
	apiDocs string,
	maxRetry int,
	execute func(raw string, currentHTML string) coderExecuteResult,
) (string, string, []string, []string, error) {
	if maxRetry < 0 {
		maxRetry = 0
	}
	var (
		lastRaw     string
		lastErr     error
		currentHTML = editTarget
		lastExecLog []string
		lastHistory []string
		lastFailure *execFailure
	)
	for attempt := 0; attempt <= maxRetry; attempt++ {
		raw, err := o.RunRole(ctx, "coder", map[string]any{
			"api_docs":     apiDocs,
			"edit_target":  currentHTML,
			"command_list": cmdList,
		})
		if err != nil {
			lastErr = err
			continue
		}
		lastRaw = raw
		if err := validateCoderActions(raw); err != nil {
			lastErr = err
			feedback := "上一轮输出不可执行，错误：" + err.Error()
			// 通过拼接反馈到 command_list，驱动 coder 自校正。
			cmdList = cmdList + "\n\n# retry_feedback_" + strconv.Itoa(attempt+1) + "\n" + feedback
			continue
		}
		if execute != nil {
			res := execute(raw, currentHTML)
			if strings.TrimSpace(res.HTML) != "" {
				currentHTML = res.HTML
			}
			if len(res.ExecutedActions) > 0 {
				lastExecLog = append([]string{}, res.ExecutedActions...)
			}
			if len(res.ExecutionHistory) > 0 {
				lastHistory = append([]string{}, res.ExecutionHistory...)
			}
			if res.Failure != nil {
				lastFailure = &execFailure{Index: res.Failure.Index, Command: res.Failure.Command}
			}
			if res.Err != nil {
				lastErr = res.Err
				feedback := "上一轮命令执行失败，错误：" + res.Err.Error()
				if len(res.ExecutedActions) > 0 {
					feedback += "\n已执行动作: " + strings.Join(res.ExecutedActions, ", ")
				}
				if lastFailure != nil {
					feedback += fmt.Sprintf("\n失败命令: #%d `%s`", lastFailure.Index, lastFailure.Command)
				}
				if strings.TrimSpace(res.HTML) != "" {
					feedback += "\n执行后编辑目标:\n" + res.HTML
				}
				cmdList = cmdList + "\n\n# retry_feedback_" + strconv.Itoa(attempt+1) + "\n" + feedback
				continue
			}
		}
		return strings.TrimSpace(stripCodeFence(raw)), currentHTML, lastExecLog, lastHistory, nil
	}
	if strings.TrimSpace(lastRaw) != "" && lastErr != nil {
		if lastFailure != nil {
			lastErr = fmt.Errorf("%w | failed_index=%d failed_command=`%s`", lastErr, lastFailure.Index, lastFailure.Command)
		}
		return strings.TrimSpace(stripCodeFence(lastRaw)), currentHTML, lastExecLog, lastHistory, lastErr
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("coder retry exhausted")
	}
	return "", currentHTML, lastExecLog, lastHistory, lastErr
}

// SelectLayoutsForOutline 对每页调用 layout_selector.yaml，选择最匹配布局（纯 Go）。
func (o *Orchestrator) SelectLayoutsForOutline(ctx context.Context, bundle *domain.TemplateBundle, outline []OutlineItem, documentOverview string) ([]LayoutSelection, error) {
	if len(outline) == 0 {
		return nil, nil
	}
	available := "{}"
	if bundle != nil {
		available = bundle.AvailableLayoutsJSON()
	}
	outlineText := summarizeOutline(outline)
	out := make([]LayoutSelection, 0, len(outline))
	for i, it := range outline {
		slideDesc := fmt.Sprintf("Slide %d: %s", i+1, it.Purpose)
		slideContent := documentOverview
		if len(it.Indexes) > 0 {
			if b, err := json.Marshal(it.Indexes); err == nil {
				slideContent = fmt.Sprintf("%s\n\nindexes=%s", documentOverview, string(b))
			}
		}
		raw, err := o.RunRole(ctx, "layout_selector", map[string]any{
			"outline":           outlineText,
			"slide_description": slideDesc,
			"slide_content":     slideContent,
			"available_layouts": available,
		})
		if err != nil {
			out = append(out, LayoutSelection{
				SlideIndex: i + 1,
				Layout:     "unknown",
				Reasoning:  "layout_selector error: " + err.Error(),
			})
			continue
		}
		layout, reasoning := parseLayoutSelection(raw)
		out = append(out, LayoutSelection{
			SlideIndex: i + 1,
			Layout:     layout,
			Reasoning:  reasoning,
		})
	}
	return out, nil
}

// PlanSlideContents 调用 editor.yaml，按每页选中的 layout schema 生成结构化 elements 计划。
func (o *Orchestrator) PlanSlideContents(
	ctx context.Context,
	bundle *domain.TemplateBundle,
	outline []OutlineItem,
	layouts []LayoutSelection,
	metadata string,
	documentOverview string,
) ([]SlideContentPlan, error) {
	if len(outline) == 0 || len(layouts) == 0 {
		return nil, nil
	}
	outlineText := summarizeOutline(outline)
	lang := "en"
	if bundle != nil && strings.TrimSpace(bundle.Language.LID) != "" {
		lang = bundle.Language.LID
	}
	plan := make([]SlideContentPlan, 0, len(layouts))
	for i, ls := range layouts {
		slideDesc := fmt.Sprintf("Slide %d: %s", i+1, outline[i].Purpose)
		slideContent := documentOverview
		if len(outline[i].Indexes) > 0 {
			if b, err := json.Marshal(outline[i].Indexes); err == nil {
				slideContent = fmt.Sprintf("%s\n\nindexes=%s", documentOverview, string(b))
			}
		}
		schema := "{}"
		if bundle != nil {
			schema = bundle.LayoutSchemaJSON(ls.Layout)
		}
		raw, err := o.RunRole(ctx, "editor", map[string]any{
			"outline":           outlineText,
			"metadata":          metadata,
			"slide_description": slideDesc,
			"slide_content":     slideContent,
			"schema":            schema,
			"language":          lang,
		})
		if err != nil {
			plan = append(plan, SlideContentPlan{
				SlideIndex: i + 1,
				Layout:     ls.Layout,
				Error:      err.Error(),
			})
			continue
		}
		plan = append(plan, SlideContentPlan{
			SlideIndex: i + 1,
			Layout:     ls.Layout,
			Elements:   parseEditorElements(raw),
		})
	}
	return plan, nil
}

// BuildCoderPlans 调用 coder.yaml 生成命令草案，并构造 py_code/html（最小可运行版本）。
func (o *Orchestrator) BuildCoderPlans(ctx context.Context, plans []SlideContentPlan) ([]SlideCoderPlan, error) {
	if len(plans) == 0 {
		return nil, nil
	}
	const apiDocs = `replace_span(node_id, text)\nreplace_image(node_id, path)\nclone_paragraph(node_id)\ndel_paragraph(node_id)\ndel_image(node_id)`
	maxCoderRetry := 3
	if v := strings.TrimSpace(os.Getenv("PPTAGENT_CODER_MAX_RETRY")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 && n <= 10 {
			maxCoderRetry = n
		}
	}
	out := make([]SlideCoderPlan, 0, len(plans))
	for _, p := range plans {
		if p.Error != "" {
			out = append(out, SlideCoderPlan{
				SlideIndex: p.SlideIndex,
				Layout:     p.Layout,
				Error:      p.Error,
			})
			continue
		}
		editTarget, nodes := buildEditableTarget(p.Layout, p.Elements)
		currentNodes := make([]editableNode, len(nodes))
		copy(currentNodes, nodes)
		cmdList := buildCoderCommandList(p)
		raw, finalHTML, finalActions, finalHistory, err := o.runCoderWithRetry(ctx, editTarget, cmdList, apiDocs, maxCoderRetry, func(candidate string, currentHTML string) coderExecuteResult {
			updated, logs, hist, fail, execErr := applyCoderActionsToNodes(currentNodes, candidate)
			snapshot := currentHTML
			if len(updated) > 0 {
				// 即使本轮执行报错，也保留已执行部分产生的中间态，供下一轮自校正参考。
				currentNodes = updated
				snapshot = renderNodesHTML(p.Layout, updated)
			}
			return coderExecuteResult{
				HTML:            snapshot,
				ExecutedActions: logs,
				ExecutionHistory: hist,
				Failure:         fail,
				Err:             execErr,
			}
		})
		html := finalHTML
		if strings.TrimSpace(html) == "" {
			html = editTarget
		}
		py := wrapPyCode(html)
		if err != nil {
			out = append(out, SlideCoderPlan{
				SlideIndex:      p.SlideIndex,
				Layout:          p.Layout,
				Commands:        strings.TrimSpace(raw),
				PyCode:          py,
				HTML:            html,
				ExecutedActions: finalActions,
				ExecutionHistory: finalHistory,
				PartialActions:  finalActions,
				FailedIndex:     extractFailedIndex(err),
				FailedCommand:   extractFailedCommand(err),
				Error:           "coder validation/retry failed: " + err.Error(),
			})
			continue
		}
		out = append(out, SlideCoderPlan{
			SlideIndex:      p.SlideIndex,
			Layout:          p.Layout,
			Commands:        strings.TrimSpace(raw),
			PyCode:          py,
			HTML:            html,
			ExecutedActions: finalActions,
			ExecutionHistory: finalHistory,
		})
	}
	return out, nil
}
