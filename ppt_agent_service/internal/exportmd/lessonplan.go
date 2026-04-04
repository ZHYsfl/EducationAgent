package exportmd

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"educationagent/ppt_agent_service_go/internal/slideutil"
	"educationagent/ppt_agent_service_go/internal/task"
)

var (
	reScript = regexp.MustCompile(`(?is)<script.*?>.*?</script>`)
	reStyle  = regexp.MustCompile(`(?is)<style.*?>.*?</style>`)
	reBr     = regexp.MustCompile(`(?i)<br\s*/?>`)
	reBlock  = regexp.MustCompile(`(?i)</(p|div|h[1-6]|li|tr)>`)
	reTags   = regexp.MustCompile(`<[^>]+>`)
	reSpace  = regexp.MustCompile(`[ \t\r\f\v]+`)
	reNL3    = regexp.MustCompile(`\n{3,}`)
)

// HTMLToPlainText 与 Python lesson_plan_docx._html_to_plain_text 对齐。
func HTMLToPlainText(html string, maxLen int) string {
	if html == "" {
		return ""
	}
	t := reScript.ReplaceAllString(html, " ")
	t = reStyle.ReplaceAllString(t, " ")
	t = reBr.ReplaceAllString(t, "\n")
	t = reBlock.ReplaceAllString(t, "\n")
	t = reTags.ReplaceAllString(t, " ")
	t = reSpace.ReplaceAllString(t, " ")
	lines := strings.Split(t, "\n")
	for i := range lines {
		lines[i] = strings.TrimSpace(lines[i])
	}
	t = strings.Join(lines, "\n")
	t = reNL3.ReplaceAllString(t, "\n\n")
	t = strings.TrimSpace(t)
	r := []rune(t)
	if maxLen > 0 && len(r) > maxLen {
		t = string(r[:maxLen-1]) + "…"
	}
	return t
}

func teachingElementLines(label string, value any) []string {
	if value == nil {
		return nil
	}
	switch x := value.(type) {
	case []any:
		if len(x) == 0 {
			return nil
		}
		parts := make([]string, 0, len(x))
		for _, v := range x {
			s := strings.TrimSpace(fmt.Sprint(v))
			if s != "" {
				parts = append(parts, s)
			}
		}
		if len(parts) == 0 {
			return nil
		}
		return []string{label + "：" + strings.Join(parts, "；")}
	default:
		s := strings.TrimSpace(fmt.Sprint(x))
		if s == "" {
			return nil
		}
		return []string{label + "：" + s}
	}
}

// BuildLessonPlanMarkdown 生成教案 Markdown（对齐原 docx 教案结构，改为 md）。
func BuildLessonPlanMarkdown(t *task.Task) string {
	var b strings.Builder
	topic := strings.TrimSpace(t.Topic)
	if topic == "" {
		topic = "（未命名课程）"
	}
	b.WriteString("# 教案\n\n")
	b.WriteString("## ")
	b.WriteString(topic)
	b.WriteString("\n\n")

	b.WriteString("### 基本信息\n\n")
	meta := []struct{ k, v string }{
		{"任务 ID", t.TaskID},
		{"会话 ID", t.SessionID},
		{"授课对象", t.Audience},
		{"全局风格", t.GlobalStyle},
		{"课件页数", fmt.Sprintf("%d", len(t.PageOrder))},
		{"期望页数（初始化）", fmt.Sprintf("%d", t.TotalPages)},
	}
	for _, row := range meta {
		v := strings.TrimSpace(row.v)
		if v == "" {
			v = "—"
		}
		b.WriteString("- **")
		b.WriteString(row.k)
		b.WriteString("**：")
		b.WriteString(v)
		b.WriteByte('\n')
	}
	b.WriteByte('\n')

	var te map[string]any
	if len(t.TeachingElements) > 0 && string(t.TeachingElements) != "null" {
		_ = json.Unmarshal(t.TeachingElements, &te)
	}
	if len(te) > 0 {
		b.WriteString("### 一、教学要素（结构化）\n\n")
		sections := []struct {
			label string
			key   string
		}{
			{"核心知识点", "knowledge_points"},
			{"教学目标", "teaching_goals"},
			{"讲授逻辑 / 大纲", "teaching_logic"},
			{"重点难点", "key_difficulties"},
			{"课程时长", "duration"},
			{"互动设计", "interaction_design"},
			{"期望产出格式", "output_formats"},
		}
		for _, sec := range sections {
			for _, line := range teachingElementLines(sec.label, te[sec.key]) {
				b.WriteString(line)
				b.WriteByte('\n')
			}
		}
		b.WriteByte('\n')
	}

	b.WriteString("### 二、教学说明与设计意图（任务描述摘要）\n\n")
	desc := strings.TrimSpace(t.Description)
	if len([]rune(desc)) > 12000 {
		desc = string([]rune(desc)[:11999]) + "…"
	}
	if desc == "" {
		b.WriteString("（无）\n\n")
	} else {
		for _, chunk := range strings.Split(desc, "\n\n") {
			c := strings.TrimSpace(chunk)
			if c == "" {
				c = "（空行）"
			}
			b.WriteString(c)
			b.WriteString("\n\n")
		}
	}

	b.WriteString("### 三、教学过程与课件分页要点\n\n")
	b.WriteString("以下由各页幻灯片内容抽取文本，供备课参考；具体呈现以课件为准。\n\n")

	for i, pid := range t.PageOrder {
		p := t.Pages[pid]
		if p == nil {
			continue
		}
		inner := slideutil.ExtractHTMLFromPy(p.PyCode)
		plain := HTMLToPlainText(inner, 3500)
		b.WriteString("#### 第 ")
		b.WriteString(fmt.Sprintf("%d", i+1))
		b.WriteString(" 页（")
		b.WriteString(pid)
		b.WriteString("）\n\n")
		if plain != "" {
			b.WriteString(plain)
			b.WriteString("\n\n")
		} else {
			b.WriteString("（本页无可用文本或为空）\n\n")
		}
	}

	b.WriteString("\n*— 本文档由 EducationAgent PPT Agent 根据当前任务与课件自动生成 —*\n")
	return b.String()
}
