package pptserver

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"educationagent/ppt_agent_service_go/internal/ecode"
	"educationagent/ppt_agent_service_go/internal/task"
)

// verifyUserAllowed 对齐 Python verify_user_id_allowed；非空返回值表示错误文案（404 用 NotFound）。
func verifyUserAllowed(userID string) string {
	mode := strings.ToLower(strings.TrimSpace(os.Getenv("PPT_USER_VERIFY")))
	if mode != "allowlist" {
		return ""
	}
	raw := os.Getenv("PPT_USER_ALLOWLIST")
	allow := make(map[string]struct{})
	for _, x := range strings.Split(raw, ",") {
		s := strings.TrimSpace(x)
		if s != "" {
			allow[s] = struct{}{}
		}
	}
	if len(allow) == 0 {
		return ""
	}
	if _, ok := allow[strings.TrimSpace(userID)]; !ok {
		return "user_id 不存在"
	}
	return ""
}

func joinTEList(v any) string {
	arr, ok := v.([]any)
	if !ok || len(arr) == 0 {
		return ""
	}
	parts := make([]string, 0, len(arr))
	for _, x := range arr {
		s := strings.TrimSpace(fmt.Sprint(x))
		if s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, ", ")
}

// buildEffectiveDescriptionInit 对齐 Python build_effective_description_for_init。
func buildEffectiveDescriptionInit(desc string, refFiles []map[string]any, teachingElements json.RawMessage) string {
	effective := strings.TrimSpace(desc)
	for i, rf := range refFiles {
		ft := strings.TrimSpace(fmt.Sprint(rf["file_type"]))
		inst := strings.TrimSpace(fmt.Sprint(rf["instruction"]))
		furl := strings.TrimSpace(fmt.Sprint(rf["file_url"]))
		fid := strings.TrimSpace(fmt.Sprint(rf["file_id"]))
		effective += fmt.Sprintf("\n\n【参考资料%d：%s】\n使用说明：%s\nfile_url：%s\nfile_id：%s\n",
			i+1, ft, inst, furl, fid)
	}
	if len(teachingElements) == 0 || string(teachingElements) == "null" {
		return effective
	}
	var m map[string]any
	if err := json.Unmarshal(teachingElements, &m); err != nil || len(m) == 0 {
		return effective
	}
	effective += "\n\n【教学要素（结构化兜底）】\n"
	effective += fmt.Sprintf("knowledge_points：%s\n", joinTEList(m["knowledge_points"]))
	effective += fmt.Sprintf("teaching_goals：%s\n", joinTEList(m["teaching_goals"]))
	effective += fmt.Sprintf("teaching_logic：%s\n", strings.TrimSpace(fmt.Sprint(m["teaching_logic"])))
	effective += fmt.Sprintf("key_difficulties：%s\n", joinTEList(m["key_difficulties"]))
	effective += fmt.Sprintf("duration：%s\n", strings.TrimSpace(fmt.Sprint(m["duration"])))
	effective += fmt.Sprintf("interaction_design：%s\n", strings.TrimSpace(fmt.Sprint(m["interaction_design"])))
	effective += fmt.Sprintf("output_formats：%s\n", joinTEList(m["output_formats"]))
	return effective
}

// validateFeedbackIntents 对齐 Python validate_feedback_intents。
// replyToContextID 为当前请求携带的 reply（子校验时传空串）。
// 返回 (lines, errCode, errMsg)；errCode==0 表示通过。
func validateFeedbackIntents(t *task.Task, intents []map[string]any, replyToContextID, rawText string) (lines []string, errCode int, errMsg string) {
	if len(intents) == 0 {
		return nil, ecode.Param, "intents 数组为空"
	}
	hasResolve := false
	for _, it := range intents {
		if strings.TrimSpace(fmt.Sprint(it["action_type"])) == "resolve_conflict" {
			hasResolve = true
			break
		}
	}
	if hasResolve && strings.TrimSpace(replyToContextID) == "" {
		return nil, ecode.Param, "resolve_conflict 必须携带非空的 reply_to_context_id"
	}

	pagesKnown := len(t.Pages) > 0 && t.Status == "completed"
	replyTrim := strings.TrimSpace(replyToContextID)

	for _, it := range intents {
		at := strings.TrimSpace(fmt.Sprint(it["action_type"]))
		if at == "" {
			return nil, ecode.Param, "不支持的 action_type："
		}
		if _, ok := allowedIntentActions[at]; !ok {
			return nil, ecode.Param, "不支持的 action_type：" + at
		}

		if at == "global_modify" {
			tid := strings.TrimSpace(fmt.Sprint(it["target_page_id"]))
			if strings.ToUpper(tid) != "ALL" {
				return nil, ecode.Param, "global_modify 要求 target_page_id 为 ALL"
			}
		}

		if at == "resolve_conflict" {
			line := fmt.Sprintf("[action=resolve_conflict, target=%s, context_id=%s] %s",
				strings.TrimSpace(fmt.Sprint(it["target_page_id"])), replyTrim, strings.TrimSpace(fmt.Sprint(it["instruction"])))
			lines = append(lines, line)
			continue
		}

		tid := strings.TrimSpace(fmt.Sprint(it["target_page_id"]))
		if pagesKnown && tid != "" && strings.ToUpper(tid) != "ALL" && t.Pages[tid] == nil {
			return nil, ecode.NotFound, "page_id 不存在"
		}

		instr := strings.TrimSpace(fmt.Sprint(it["instruction"]))
		if at == "delete" && instr == "" {
			instr = "（删除本页）"
		}
		if instr == "" && at != "delete" {
			return nil, ecode.Param, "intent 缺少有效 instruction：" + at
		}

		extra := ""
		if replyTrim != "" {
			extra += ", reply_to_context_id=" + replyTrim
		}
		rt := strings.TrimSpace(rawText)
		if rt != "" {
			runes := []rune(rt)
			if len(runes) > 500 {
				rt = string(runes[:500])
			}
			extra += ", raw_text=" + rt
		}
		lines = append(lines, fmt.Sprintf("[action=%s, target=%s%s] %s", at, fmt.Sprint(it["target_page_id"]), extra, instr))
	}

	if len(lines) == 0 {
		return nil, ecode.Param, "无法从 intents 中提取有效修改指令"
	}
	return lines, 0, ""
}
