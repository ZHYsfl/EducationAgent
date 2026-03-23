package merge

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/pmezard/go-difflib/difflib"
)

// GlobalKey 与 Python feedback_pipeline.GLOBAL_KEY 一致。
const GlobalKey = "__global__"

// MergeDecision 对齐 Python MergeDecision。
type MergeDecision struct {
	MergeStatus     string // auto_resolved | ask_human
	MergedPycode    string
	QuestionForUser string
	RuleMergePath   bool
}

func mergeBucket(actionType, targetPageID string) string {
	if actionType == "global_modify" {
		return GlobalKey
	}
	s := strings.TrimSpace(targetPageID)
	if s == "" {
		return GlobalKey
	}
	return s
}

// MergeBucket 导出供流水线按意图分桶。
func MergeBucket(it Intent) string {
	return mergeBucket(it.ActionType, it.TargetPageID)
}

// Intent 单条反馈意图。
type Intent struct {
	ActionType     string
	TargetPageID   string
	Instruction    string
	BaseTimestamp  int64
	RawText        string
}

func IntentFromMap(m map[string]any) Intent {
	return Intent{
		ActionType:   strings.TrimSpace(fmt.Sprint(m["action_type"])),
		TargetPageID: strings.TrimSpace(fmt.Sprint(m["target_page_id"])),
		Instruction:  fmt.Sprint(m["instruction"]),
	}
}

func BuildSystemPatch(snapshotPages map[string]any, pageID, currentPyCode string) string {
	if len(snapshotPages) == 0 || pageID == "" {
		return ""
	}
	ent, _ := snapshotPages[pageID].(map[string]any)
	if ent == nil {
		ent = map[string]any{}
	}
	baseCode := strings.TrimSpace(fmt.Sprint(ent["py_code"]))
	if baseCode == "" {
		return "(VAD 快照中无该页 py_code，无法计算 diff)"
	}
	text := unifiedDiffTxt(baseCode, currentPyCode, "V_base", "V_current")
	if strings.TrimSpace(text) == "" {
		return "(无文本行级差异)"
	}
	if len(text) > 12000 {
		return text[:12000]
	}
	return text
}

func unifiedDiffTxt(a, b, from, to string) string {
	d := difflib.UnifiedDiff{
		A:        difflib.SplitLines(a),
		B:        difflib.SplitLines(b),
		FromFile: from,
		ToFile:   to,
		Context:  3,
	}
	s, _ := difflib.GetUnifiedDiffString(d)
	return s
}

// BuildSystemPatchGlobal 按 page_order 拼接再 diff。
func BuildSystemPatchGlobal(snapshotPages map[string]any, pageOrder []string, currentConcat string) string {
	if len(pageOrder) == 0 {
		return "(无 VAD 快照，system_patch 为空)"
	}
	var parts []string
	for _, pid := range pageOrder {
		if pid == "" {
			continue
		}
		ent, _ := snapshotPages[pid].(map[string]any)
		if ent == nil {
			parts = append(parts, "")
			continue
		}
		parts = append(parts, fmt.Sprint(ent["py_code"]))
	}
	baseConcat := strings.Join(parts, "\n")
	if strings.TrimSpace(baseConcat) == "" {
		return "(无 VAD 快照，system_patch 为空)"
	}
	text := unifiedDiffTxt(baseConcat, currentConcat, "V_base_global", "V_current_global")
	if strings.TrimSpace(text) == "" {
		return "(无文本行级差异)"
	}
	if len(text) > 12000 {
		return text[:12000]
	}
	return text
}

func patchIsNoCodeConflict(systemPatch string) bool {
	p := strings.TrimSpace(systemPatch)
	if p == "" {
		return true
	}
	if p == "(无文本行级差异)" {
		return true
	}
	if p == "(无 VAD 快照，system_patch 为空)" {
		return true
	}
	if strings.Contains(p, "VAD 快照中无该页") || strings.Contains(p, "无法计算 diff") {
		return false
	}
	if strings.HasPrefix(strings.TrimLeft(p, " "), "---") || strings.Contains(p, "\n@@") ||
		strings.Contains(p, "\n+") || strings.Contains(p, "\n-") {
		return false
	}
	if strings.HasPrefix(p, "(") && strings.HasSuffix(p, ")") {
		return true
	}
	return false
}

// MergeLLMCaller 三路合并 LLM（应配置 JSON 输出）。
type MergeLLMCaller func(ctx context.Context, user, system string) (string, error)

// DecideThreeWayMerge 对齐 Python decide_three_way_merge。
func DecideThreeWayMerge(ctx context.Context, taskID, pageID, currentCode, systemPatch, instruction, actionType string, llm MergeLLMCaller) MergeDecision {
	inst := strings.TrimSpace(instruction)
	patch := strings.TrimSpace(systemPatch)

	codeOK := patchIsNoCodeConflict(patch)
	// 仅保留非语义结构判断：无代码冲突时可直接走规则路径。
	// 高动态/模糊自然语言不再使用本地规则（含 regex）判断。
	if codeOK {
		return MergeDecision{
			MergeStatus:   "auto_resolved",
			RuleMergePath: true,
		}
	}
	if llm == nil {
		return MergeDecision{
			MergeStatus:     "ask_human",
			QuestionForUser: "冲突裁决依赖 LLM，当前不可用，请明确说明希望保留哪种修改方案。",
		}
	}

	prompt := fmt.Sprintf(`你是课件编辑「三路合并」裁决器（教学场景）。
任务 task_id=%s，页面 page_id=%s，用户操作类型 action_type=%s。

【V_current 页面 Python 源码（节选）】
%s

【V_base→V_current 的 system_patch（diff 摘要）】
%s

【用户自然语言指令】
%s

请输出**仅一个** JSON 对象，键为：
- merge_status: 字符串 "auto_resolved" 或 "ask_human"
- merged_pycode: 当 auto_resolved 时，给出合并用户指令后的**完整**页面 Python 源码（必须保留 def get_slide_markup）；若无法在不猜用户偏好下完成则必须用 ask_human
- question_for_user: 当 ask_human 时，一两句口语化中文问句，让用户通过语音二选一或澄清；auto_resolved 时为空字符串
`, taskID, pageID, truncate(currentCode, 6000), truncate(patch, 4000), inst)

	raw, err := llm(ctx, prompt, "只输出 JSON，不要 Markdown。")
	if err != nil {
		return MergeDecision{
			MergeStatus:     "ask_human",
			QuestionForUser: "我暂时无法完成冲突裁决，请用一句话明确这页要保留的修改方向。",
		}
	}
	data := parseJSONObject(raw)
	if data == nil {
		return MergeDecision{
			MergeStatus:     "ask_human",
			QuestionForUser: "冲突裁决结果解析失败，请直接说明你希望采用的具体方案。",
		}
	}
	status := strings.TrimSpace(fmt.Sprint(data["merge_status"]))
	if status != "auto_resolved" && status != "ask_human" {
		status = "ask_human"
	}
	return MergeDecision{
		MergeStatus:     status,
		MergedPycode:    fmt.Sprint(data["merged_pycode"]),
		QuestionForUser: fmt.Sprint(data["question_for_user"]),
	}
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

var reJSONObj = regexp.MustCompile(`\{[\s\S]*\}`)

func parseJSONObject(s string) map[string]any {
	s = strings.TrimSpace(s)
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err == nil && m != nil {
		return m
	}
	sub := reJSONObj.FindString(s)
	if sub == "" {
		return nil
	}
	if err := json.Unmarshal([]byte(sub), &m); err != nil {
		return nil
	}
	return m
}
