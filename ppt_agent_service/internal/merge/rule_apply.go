package merge

import (
	"regexp"
	"strings"
)

func norm(s string) string {
	return strings.TrimSpace(s)
}

// TryRuleApplyHTML 对齐 Python try_rule_apply_html。
func TryRuleApplyHTML(html, instruction string) *string {
	inst := norm(instruction)
	if inst == "" || strings.TrimSpace(html) == "" {
		return nil
	}

	if ok, _ := regexp.MatchString(`^(保持|维持|不要改|别改|不用改|就这样|可以了|好的|确认)`, inst); ok {
		return &html
	}

	reBook := regexp.MustCompile(`(?:将|把)\s*「([^」]+)」\s*(?:改(?:成|为)|换(?:成|为))\s*「([^」]+)」`)
	if m := reBook.FindStringSubmatch(inst); len(m) == 3 {
		a, b := m[1], m[2]
		if strings.Contains(html, a) {
			out := strings.Replace(html, a, b, 1)
			return &out
		}
		return nil
	}
	reQuote := regexp.MustCompile(`(?:将|把)\s*"([^"]+)"\s*(?:改(?:成|为)|换(?:成|为))\s*"([^"]+)"`)
	if m := reQuote.FindStringSubmatch(inst); len(m) == 3 {
		a, b := m[1], m[2]
		if strings.Contains(html, a) {
			out := strings.Replace(html, a, b, 1)
			return &out
		}
		return nil
	}

	reTitle := regexp.MustCompile(`标题\s*(?:改(?:成|为)|换成|设置为)\s*[「"]?([^」"]*)[」"]?\s*$`)
	if m := reTitle.FindStringSubmatch(inst); len(m) == 2 {
		if out := tryReplaceHeading(html, norm(m[1])); out != nil {
			return out
		}
	}
	reTitle2 := regexp.MustCompile(`^(?:标题|大标题)\s*[:：]\s*(.+)$`)
	if m := reTitle2.FindStringSubmatch(inst); len(m) == 2 {
		if out := tryReplaceHeading(html, norm(m[1])); out != nil {
			return out
		}
	}

	colorMap := []struct{ word, hex string }{
		{"红色", "#c0392b"}, {"红", "#c0392b"},
		{"蓝色", "#2980b9"}, {"蓝", "#2980b9"},
		{"绿色", "#27ae60"}, {"绿", "#27ae60"},
		{"黑色", "#111111"}, {"黑", "#111111"},
		{"白色", "#ffffff"}, {"白", "#ffffff"},
	}
	reColorCtx := regexp.MustCompile(`(?:标题|文字|字体|字|颜色|改成|改为|换成)`)
	reHOpen := regexp.MustCompile(`(?i)(<h[1-6])([^>]*>)`)
	for _, cm := range colorMap {
		if !strings.Contains(inst, cm.word) || !reColorCtx.MatchString(inst) {
			continue
		}
		if idx := reHOpen.FindStringIndex(html); idx != nil {
			m := reHOpen.FindStringSubmatch(html[idx[0]:idx[1]])
			if len(m) >= 3 {
				out := html[:idx[0]] + m[1] + injectColorOnOpenTagRest(m[2], cm.hex) + html[idx[1]:]
				return &out
			}
		}
		reFirst := regexp.MustCompile(`^(<[a-zA-Z][a-zA-Z0-9]*)(?=[\s>])`)
		trim := strings.TrimSpace(html)
		if loc := reFirst.FindStringIndex(trim); loc != nil {
			prefix := trim[:loc[1]]
			suffix := trim[loc[1]:]
			out := prefix + ` style="color:` + cm.hex + `;"` + suffix
			return &out
		}
	}

	reDel := regexp.MustCompile(`删除\s*「([^」]+)」`)
	if m := reDel.FindStringSubmatch(inst); len(m) == 2 {
		frag := m[1]
		if len(frag) >= 2 && strings.Contains(html, frag) {
			out := strings.Replace(html, frag, "", 1)
			return &out
		}
	}

	return nil
}

func tryReplaceHeading(html, newTitle string) *string {
	if newTitle == "" {
		return nil
	}
	reH := regexp.MustCompile(`(?i)(<h[1-6][^>]*>)(.*?)(</h[1-6]>)`)
	loc := reH.FindStringIndex(html)
	if loc != nil {
		m := reH.FindStringSubmatch(html[loc[0]:loc[1]])
		if len(m) >= 4 {
			out := html[:loc[0]] + m[1] + newTitle + m[3] + html[loc[1]:]
			return &out
		}
	}
	out := `<h2 style="margin:0 0 12px 0;">` + newTitle + `</h2>` + html
	return &out
}

func injectColorOnOpenTagRest(rest, hexv string) string {
	if strings.Contains(strings.ToLower(rest), "style=") {
		return regexp.MustCompile(`(?i)style="([^"]*)"`).ReplaceAllStringFunc(rest, func(full string) string {
			sm := regexp.MustCompile(`(?i)style="([^"]*)"`).FindStringSubmatch(full)
			if len(sm) < 2 {
				return full
			}
			without := regexp.MustCompile(`(?i)color\s*:\s*[^;"]+;?`).ReplaceAllString(sm[1], "")
			return `style="` + without + `color:` + hexv + `;"`
		})
	}
	return ` style="color:` + hexv + `;"` + rest
}
