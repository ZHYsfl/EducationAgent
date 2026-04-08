package service

import (
	"context"
	"fmt"
	"strings"

	toolcalling "tool_calling_go"

	openai "github.com/openai/openai-go/v3"

	"zcxppt/internal/model"
	"zcxppt/internal/repository"
)

// IntentParser resolves ambiguous natural-language user speech into structured Intents.
type IntentParser struct {
	pptRepo repository.PPTRepository
	llmCfg  LLMClientConfig
}

func NewIntentParser(pptRepo repository.PPTRepository, llmCfg LLMClientConfig) *IntentParser {
	return &IntentParser{pptRepo: pptRepo, llmCfg: llmCfg}
}

// Parse resolves RawText into a []Intent using the current canvas page table.
// If LLM is not configured, falls back to a simple keyword-based parser.
func (p *IntentParser) Parse(ctx context.Context, taskID, viewingPageID, rawText string) ([]model.Intent, error) {
	return p.ParseWithContext(ctx, taskID, viewingPageID, rawText, "", "", "")
}

// ParseWithContext resolves RawText into a []Intent with additional context.
// topic/subject/kbSummary are used to enrich content_diversity intents.
func (p *IntentParser) ParseWithContext(ctx context.Context, taskID, viewingPageID, rawText, topic, subject, kbSummary string) ([]model.Intent, error) {
	if strings.TrimSpace(rawText) == "" {
		return nil, nil
	}

	canvas, err := p.pptRepo.GetCanvasStatus(taskID)
	if err != nil {
		return nil, err
	}

	if strings.TrimSpace(p.llmCfg.APIKey) == "" || strings.TrimSpace(p.llmCfg.Model) == "" {
		intents := p.parseByKeyword(rawText, canvas, viewingPageID)
		// 用上下文补充内容多样性 intent 的 instruction
		intents = p.enrichDiversityIntents(intents, rawText, topic, subject, kbSummary)
		return intents, nil
	}

	intents, err := p.parseByLLM(ctx, rawText, canvas, viewingPageID, topic, subject, kbSummary)
	if err != nil {
		// 如果结构化输出失败（重试耗尽），回退到关键词解析
		intents = p.parseByKeyword(rawText, canvas, viewingPageID)
		intents = p.enrichDiversityIntents(intents, rawText, topic, subject, kbSummary)
		return intents, nil
	}
	return intents, nil
}

// parseByLLM uses the LLM to interpret raw user speech into structured intents.
func (p *IntentParser) parseByLLM(ctx context.Context, rawText string, canvas model.CanvasStatusResponse, viewingPageID string, topic, subject, kbSummary string) ([]model.Intent, error) {
	pageTable := p.buildPageTable(canvas)

	system := `你是一个PPT意图解析器。请将用户的口语指令解析为结构化的意图数组。

支持的意图类型（action_type）：
- modify: 修改指定页面内容（必须指定 target_page_id）
- insert_before: 在目标页之前插入新页（必须指定 target_page_id）
- insert_after: 在目标页之后插入新页（必须指定 target_page_id）
- delete: 删除指定页面（必须指定 target_page_id）
- global_modify: 全局修改所有页面（target_page_id 填 "ALL"）
- reorder: 页面重排（target_page_id 填要移动的页面ID，instruction 填目标位置，如 "移到第2页"）
- generate_animation: 生成动画创意（HTML5动画），生成后通过回调通知前端
- generate_game: 生成互动小游戏（HTML5），生成后通过回调通知前端

解析规则：
1. 一条自然语言指令可能包含多个意图，请拆分为多个 Intent 对象
2. 如果用户提到"这一页"/"当前页"/"这个页面"，target_page_id 应为 viewing_page_id
3. 如果用户指定了具体页面名称或编号，尝试匹配 page_table 中的页面
4. 如果意图类型不明确，默认为 modify
5. instruction 应填用户的原始修改指令内容
6. generate_animation 和 generate_game 不需要 target_page_id，instruction 中可以指定知识点或风格偏好（如"做一个关于导数的动画"、"生成一个选择题游戏"）
7. 如果用户要求生成动画但未指定风格，默认使用 "all"
8. 如果用户要求生成游戏但未指定类型，默认使用 "quiz"

严格输出 JSON 数组，格式：[{"action_type":"...","target_page_id":"...","instruction":"...","animation_style":"...","game_type":"..."}]
不要输出 JSON 以外的任何文字。`

	user := fmt.Sprintf("viewing_page_id=%s\nraw_text=%s\npage_table=%s\ntopic=%s\nsubject=%s\nkb_summary=%s",
		viewingPageID, rawText, pageTable, topic, subject, kbSummary)

	msgs := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(system),
		openai.UserMessage(user),
	}

	llmCfg := toolcalling.LLMConfig{
		APIKey:  p.llmCfg.APIKey,
		BaseURL: p.llmCfg.BaseURL,
		Model:   p.llmCfg.Model,
	}

	var intents []model.Intent
	err := toolcalling.ChatCompletionStructured(ctx, llmCfg, msgs, &intents, nil)
	if err != nil {
		// 如果结构化输出失败（重试耗尽），回退到关键词解析
		return p.parseByKeyword(rawText, canvas, viewingPageID), nil
	}

	// 标准化字段
	for i := range intents {
		intents[i].ActionType = strings.TrimSpace(strings.ToLower(intents[i].ActionType))
		intents[i].TargetPageID = strings.TrimSpace(intents[i].TargetPageID)
		intents[i].Instruction = strings.TrimSpace(intents[i].Instruction)
		intents[i].AnimationStyle = strings.TrimSpace(strings.ToLower(intents[i].AnimationStyle))
		intents[i].GameType = strings.TrimSpace(strings.ToLower(intents[i].GameType))
		if intents[i].TargetPageID == "" && intents[i].ActionType != "generate_animation" && intents[i].ActionType != "generate_game" {
			intents[i].TargetPageID = viewingPageID
		}
		// 标准化 animation_style
		validAnimStyles := map[string]bool{"slide_in": true, "fade": true, "zoom": true, "draw": true, "pulse": true, "all": true}
		if intents[i].AnimationStyle != "" && !validAnimStyles[intents[i].AnimationStyle] {
			intents[i].AnimationStyle = "all"
		}
		// 标准化 game_type
		validGameTypes := map[string]bool{"quiz": true, "matching": true, "ordering": true, "fill_blank": true, "random": true}
		if intents[i].GameType != "" && !validGameTypes[intents[i].GameType] {
			intents[i].GameType = "quiz"
		}
	}

	return intents, nil
}

// buildPageTable formats the canvas page order for the LLM prompt.
func (p *IntentParser) buildPageTable(canvas model.CanvasStatusResponse) string {
	var parts []string
	for i, pid := range canvas.PageOrder {
		parts = append(parts, fmt.Sprintf("第%d页: %s", i+1, pid))
	}
	if len(parts) == 0 {
		return "(无页面)"
	}
	return strings.Join(parts, "\n")
}

// parseByKeyword is a fallback rule-based parser when LLM is unavailable.
func (p *IntentParser) parseByKeyword(rawText string, canvas model.CanvasStatusResponse, viewingPageID string) []model.Intent {
	raw := strings.TrimSpace(rawText)
	lower := strings.ToLower(raw)

	// 内容多样性 intent 检测（优先级高于页面操作）
	animationKeywords := []string{"动画", "animat", "动效", "特效", "过渡动画"}
	gameKeywords := []string{"游戏", "quiz", "答题", "测验", "互动", "答题器", "选择题游戏", "匹配", "排序题", "填空"}

	isAnimation := false
	for _, kw := range animationKeywords {
		if strings.Contains(lower, kw) {
			isAnimation = true
			break
		}
	}
	isGame := false
	if !isAnimation {
		for _, kw := range gameKeywords {
			if strings.Contains(lower, kw) {
				isGame = true
				break
			}
		}
	}

	if isAnimation {
		intent := model.Intent{ActionType: "generate_animation", Instruction: raw}
		if strings.Contains(lower, "滑入") || strings.Contains(lower, "slide") {
			intent.AnimationStyle = "slide_in"
		} else if strings.Contains(lower, "淡入") || strings.Contains(lower, "fade") {
			intent.AnimationStyle = "fade"
		} else if strings.Contains(lower, "缩放") || strings.Contains(lower, "zoom") {
			intent.AnimationStyle = "zoom"
		} else if strings.Contains(lower, "绘制") || strings.Contains(lower, "draw") {
			intent.AnimationStyle = "draw"
		} else if strings.Contains(lower, "脉冲") || strings.Contains(lower, "pulse") {
			intent.AnimationStyle = "pulse"
		}
		return []model.Intent{intent}
	}
	if isGame {
		intent := model.Intent{ActionType: "generate_game", Instruction: raw}
		// 尝试解析游戏类型
		if strings.Contains(lower, "匹配") || strings.Contains(lower, "连连看") {
			intent.GameType = "matching"
		} else if strings.Contains(lower, "排序") {
			intent.GameType = "ordering"
		} else if strings.Contains(lower, "填空") {
			intent.GameType = "fill_blank"
		} else {
			intent.GameType = "quiz" // 默认选择题
		}
		return []model.Intent{intent}
	}

	// global_modify shortcuts
	if strings.Contains(lower, "全部") || strings.Contains(lower, "所有页面") ||
		strings.Contains(lower, "全局") || strings.Contains(lower, "每一页") {
		return []model.Intent{{ActionType: "global_modify", TargetPageID: "ALL", Instruction: raw}}
	}

	// delete shortcuts
	if strings.Contains(lower, "删除") || strings.Contains(lower, "删掉") {
		target := viewingPageID
		if pid := p.matchPageID(lower, canvas); pid != "" {
			target = pid
		}
		return []model.Intent{{ActionType: "delete", TargetPageID: target, Instruction: raw}}
	}

	// insert shortcuts
	if strings.Contains(lower, "在") && strings.Contains(lower, "前插入") {
		ref := viewingPageID
		if pid := p.matchPageID(lower, canvas); pid != "" {
			ref = pid
		}
		return []model.Intent{{ActionType: "insert_before", TargetPageID: ref, Instruction: raw}}
	}
	if strings.Contains(lower, "在") && strings.Contains(lower, "后插入") {
		ref := viewingPageID
		if pid := p.matchPageID(lower, canvas); pid != "" {
			ref = pid
		}
		return []model.Intent{{ActionType: "insert_after", TargetPageID: ref, Instruction: raw}}
	}

	// reorder shortcuts
	if strings.Contains(lower, "移到") || strings.Contains(lower, "移动") ||
		strings.Contains(lower, "调换") || strings.Contains(lower, "调整顺序") {
		target := viewingPageID
		if pid := p.matchPageID(lower, canvas); pid != "" {
			target = pid
		}
		return []model.Intent{{ActionType: "reorder", TargetPageID: target, Instruction: raw}}
	}

	// default: modify current page
	return []model.Intent{{ActionType: "modify", TargetPageID: viewingPageID, Instruction: raw}}
}

// matchPageID attempts to match a page identifier from the text against the canvas.
// Looks for ordinals like "第2页" or partial page IDs.
func (p *IntentParser) matchPageID(text string, canvas model.CanvasStatusResponse) string {
	lower := strings.ToLower(text)

	// Try ordinal patterns like "第2页", "第一页"
	for i, pid := range canvas.PageOrder {
		ordinal := fmt.Sprintf("第%d页", i+1)
		ordinalCn := ""
		cnDigits := []string{"一", "二", "三", "四", "五", "六", "七", "八", "九", "十"}
		if i < len(cnDigits) {
			ordinalCn = "第" + cnDigits[i] + "页"
		}
		if strings.Contains(lower, ordinal) || (ordinalCn != "" && strings.Contains(lower, ordinalCn)) {
			return pid
		}
	}

	// Try partial page ID match
	for _, pid := range canvas.PageOrder {
		short := strings.ToLower(pid)
		if len(short) > 4 && strings.Contains(lower, short[len(short)-6:]) {
			return pid
		}
	}

	return ""
}

// enrichDiversityIntents fills in missing topic/subject/kb_summary for generate_animation/game intents
// when the LLM parser did not extract them explicitly.
func (p *IntentParser) enrichDiversityIntents(intents []model.Intent, rawText, topic, subject, kbSummary string) []model.Intent {
	for i := range intents {
		if intents[i].ActionType == "generate_animation" || intents[i].ActionType == "generate_game" {
			// 如果 instruction 为空或与 rawText 相同，用 topic 补充
			if strings.TrimSpace(intents[i].Instruction) == "" || intents[i].Instruction == rawText {
				if topic != "" {
					intents[i].Instruction = fmt.Sprintf("主题：%s；原始指令：%s", topic, rawText)
				}
			}
			// 如果没有指定 animation_style 且为空，默认 all
			if intents[i].ActionType == "generate_animation" && intents[i].AnimationStyle == "" {
				intents[i].AnimationStyle = "all"
			}
			// 如果没有指定 game_type 且为空，默认 quiz
			if intents[i].ActionType == "generate_game" && intents[i].GameType == "" {
				intents[i].GameType = "quiz"
			}
		}
	}
	return intents
}
