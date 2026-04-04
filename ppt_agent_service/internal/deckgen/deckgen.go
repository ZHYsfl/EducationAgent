package deckgen

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
)

// LLMOpts 单次补全选项（对齐 toolcalling.ChatCompletionSimple）。
type LLMOpts struct {
	JSONMode    bool
	Temperature *float64
	MaxTokens   *int
}

// Completer 由 toolllm.Client 实现。
type Completer interface {
	Complete(ctx context.Context, user, system string, opts *LLMOpts) (string, error)
}

type GenerateDeckRequest struct {
	Topic              string          `json:"topic"`
	Description        string          `json:"description"`
	TotalPages         int             `json:"total_pages"`
	Audience           string          `json:"audience"`
	GlobalStyle        string          `json:"global_style"`
	TeachingElements   json.RawMessage `json:"teaching_elements"`
	ExtraContext       string          `json:"extra_context"`
}

type GenerateDeckResponse struct {
	OK         bool     `json:"ok"`
	Error      string   `json:"error,omitempty"`
	SlideHTML  []string `json:"slide_html"`
	SlideCount int      `json:"slide_count"`
}

type outlineSlide struct {
	Title     string   `json:"title"`
	KeyPoints []string `json:"key_points"`
	Bullets   []string `json:"bullets"`
}

func normalizeCount(n int) int {
	if n <= 0 {
		return 8
	}
	if n > 40 {
		return 40
	}
	return n
}

func stripJSONFences(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	lines := strings.Split(s, "\n")
	if len(lines) > 0 && strings.HasPrefix(lines[0], "```") {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.HasPrefix(strings.TrimSpace(lines[len(lines)-1]), "```") {
		lines = lines[:len(lines)-1]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func parseJSONObject(s string) (map[string]json.RawMessage, error) {
	s = stripJSONFences(s)
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(s), &m); err == nil && m != nil {
		return m, nil
	}
	re := regexp.MustCompile(`\{[\s\S]*\}`)
	if sub := re.FindString(s); sub != "" {
		if err := json.Unmarshal([]byte(sub), &m); err == nil && m != nil {
			return m, nil
		}
	}
	return nil, fmt.Errorf("无法解析 JSON 对象")
}

func keyPointsFor(sl outlineSlide) []string {
	if len(sl.KeyPoints) > 0 {
		return sl.KeyPoints
	}
	return sl.Bullets
}

func intPtr(v int) *int { return &v }

func Generate(ctx context.Context, llm Completer, req GenerateDeckRequest) (*GenerateDeckResponse, error) {
	if llm == nil {
		return &GenerateDeckResponse{OK: false, Error: "llm nil"}, fmt.Errorf("llm nil")
	}
	n := normalizeCount(req.TotalPages)
	teach := ""
	if len(req.TeachingElements) > 0 && string(req.TeachingElements) != "null" {
		r := []rune(string(req.TeachingElements))
		if len(r) > 4000 {
			teach = string(r[:4000]) + "…"
		} else {
			teach = string(req.TeachingElements)
		}
	}
	extra := strings.TrimSpace(req.ExtraContext)
	if len([]rune(extra)) > 12000 {
		r := []rune(extra)
		extra = string(r[:12000]) + "…"
	}

	tOut := 0.35
	tSlide := 0.6

	outlineUser := fmt.Sprintf(
		`课程主题：%s
教学对象/受众：%s
整体风格要求：%s

【课程需求描述】
%s

%s

请生成恰好 %d 张幻灯片的大纲。每张要有清晰标题与 2～5 条要点（字符串数组）。
只输出一个 JSON 对象，不要 markdown 代码围栏，格式严格为：
{"slides":[{"title":"第1页标题","key_points":["要点1","要点2"]}, ...]}
slides 数组长度必须等于 %d。`,
		strings.TrimSpace(req.Topic),
		strings.TrimSpace(req.Audience),
		strings.TrimSpace(req.GlobalStyle),
		strings.TrimSpace(req.Description),
		func() string {
			if teach == "" {
				return ""
			}
			return "【教学元素 JSON】\n" + teach
		}(),
		n, n,
	)
	if extra != "" {
		outlineUser += "\n\n【参考资料与检索摘要】\n" + extra
	}
	outlineSys := "你是资深教学设计助手。只输出合法 JSON 对象，键名使用 slides、title、key_points。不要解释。"
	outRaw, err := llm.Complete(ctx, outlineUser, outlineSys, &LLMOpts{JSONMode: true, Temperature: &tOut, MaxTokens: intPtr(4096)})
	if err != nil {
		return &GenerateDeckResponse{OK: false, Error: err.Error()}, err
	}
	slidesOutline, err := parseOutline(outRaw, n, req.Topic)
	if err != nil {
		return &GenerateDeckResponse{OK: false, Error: "大纲解析失败: " + err.Error()}, err
	}

	sem := make(chan struct{}, 4)
	results := make([]string, n)
	var wg sync.WaitGroup
	var firstErr error
	var errMu sync.Mutex

	for i := 0; i < n; i++ {
		i := i
		sl := slidesOutline[i]
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			kps := keyPointsFor(sl)
			kpText := strings.Join(kps, "\n- ")
			if kpText != "" {
				kpText = "- " + kpText
			}
			prompt := fmt.Sprintf(
				`你是课件幻灯片 HTML 生成器。只输出一段 HTML 片段（不要 DOCTYPE、不要 html/head/body 外壳），用于 16:9（1280×720 逻辑像素）幻灯片内容区。

课程主题：%s
受众：%s
风格：%s
当前为第 %d / %d 页。

本页标题：%s
要点：
%s

【全课需求摘要】
%s

要求：教学向排版，层次清晰，可用内联 style；禁止 script、禁止外链脚本、禁止 Markdown 语法；可适当使用 ul/li、h2、色块 div。
只输出 HTML 片段，不要解释。`,
				strings.TrimSpace(req.Topic),
				strings.TrimSpace(req.Audience),
				strings.TrimSpace(req.GlobalStyle),
				i+1, n,
				strings.TrimSpace(sl.Title),
				kpText,
				truncRunes(strings.TrimSpace(req.Description), 2500),
			)
			if extra != "" {
				prompt += "\n\n【参考上下文摘录】\n" + truncRunes(extra, 2000)
			}
			html, err := llm.Complete(ctx, prompt, "只输出 HTML 片段。", &LLMOpts{JSONMode: false, Temperature: &tSlide, MaxTokens: intPtr(8192)})
			if err != nil {
				errMu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				errMu.Unlock()
				return
			}
			html = strings.TrimSpace(stripHTMLFences(html))
			if html == "" {
				html = "<div style=\"padding:40px\"><h2>" + escapeMinimal(sl.Title) + "</h2><p>（内容生成失败）</p></div>"
			}
			results[i] = html
		}()
	}
	wg.Wait()
	if firstErr != nil {
		return &GenerateDeckResponse{OK: false, Error: firstErr.Error()}, firstErr
	}
	return &GenerateDeckResponse{OK: true, SlideHTML: results, SlideCount: len(results)}, nil
}

func parseOutline(raw string, wantN int, topic string) ([]outlineSlide, error) {
	m, err := parseJSONObject(raw)
	if err != nil {
		return nil, err
	}
	rawSlides, ok := m["slides"]
	if !ok {
		return nil, fmt.Errorf("缺少 slides 字段")
	}
	var list []outlineSlide
	if err := json.Unmarshal(rawSlides, &list); err != nil {
		return nil, err
	}
	for len(list) < wantN {
		list = append(list, outlineSlide{
			Title:     fmt.Sprintf("第 %d 页：%s（延展）", len(list)+1, strings.TrimSpace(topic)),
			KeyPoints: []string{"小结与练习", "回顾本课重点"},
		})
	}
	if len(list) > wantN {
		list = list[:wantN]
	}
	for i := range list {
		if strings.TrimSpace(list[i].Title) == "" {
			list[i].Title = fmt.Sprintf("第 %d 页", i+1)
		}
		if len(keyPointsFor(list[i])) == 0 {
			list[i].KeyPoints = []string{"核心概念", "示例与应用"}
		}
	}
	return list, nil
}

func stripHTMLFences(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		lines := strings.Split(s, "\n")
		if len(lines) > 0 {
			lines = lines[1:]
		}
		for len(lines) > 0 && strings.HasPrefix(strings.TrimSpace(lines[len(lines)-1]), "```") {
			lines = lines[:len(lines)-1]
		}
		s = strings.TrimSpace(strings.Join(lines, "\n"))
		s = strings.TrimPrefix(s, "html")
		s = strings.TrimSpace(s)
	}
	return s
}

func truncRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

func escapeMinimal(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}
