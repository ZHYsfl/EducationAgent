package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	openai "github.com/openai/openai-go/v3"
	toolcalling "tool_calling_go"

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
	if strings.TrimSpace(rawText) == "" {
		return nil, nil
	}

	canvas, err := p.pptRepo.GetCanvasStatus(taskID)
	if err != nil {
		return nil, err
	}

	if strings.TrimSpace(p.llmCfg.APIKey) == "" || strings.TrimSpace(p.llmCfg.Model) == "" {
		return p.parseByKeyword(rawText, canvas, viewingPageID), nil
	}

	return p.parseByLLM(ctx, rawText, canvas, viewingPageID)
}

// parseByLLM uses the LLM to interpret raw user speech into structured intents.
func (p *IntentParser) parseByLLM(ctx context.Context, rawText string, canvas model.CanvasStatusResponse, viewingPageID string) ([]model.Intent, error) {
	pageTable := p.buildPageTable(canvas)

	system := `你是一个PPT意图解析器。请将用户的口语指令解析为结构化的意图数组。

支持的意图类型（action_type）：
- modify: 修改指定页面内容（必须指定 target_page_id）
- insert_before: 在目标页之前插入新页（必须指定 target_page_id）
- insert_after: 在目标页之后插入新页（必须指定 target_page_id）
- delete: 删除指定页面（必须指定 target_page_id）
- global_modify: 全局修改所有页面（target_page_id 填 "ALL"）
- reorder: 页面重排（target_page_id 填要移动的页面ID，instruction 填目标位置，如 "移到第2页"）

解析规则：
1. 一条自然语言指令可能包含多个意图，请拆分为多个 Intent 对象
2. 如果用户提到"这一页"/"当前页"/"这个页面"，target_page_id 应为 viewing_page_id
3. 如果用户指定了具体页面名称或编号，尝试匹配 page_table 中的页面
4. 如果意图类型不明确，默认为 modify
5. instruction 应填用户的原始修改指令内容

严格输出 JSON 数组，格式：[{"action_type":"...","target_page_id":"...","instruction":"..."}]
不要输出 JSON 以外的任何文字。`

	user := fmt.Sprintf("viewing_page_id=%s\nraw_text=%s\npage_table=%s",
		viewingPageID, rawText, pageTable)

	agent := toolcalling.NewAgent(toolcalling.LLMConfig{
		APIKey:  p.llmCfg.APIKey,
		BaseURL: p.llmCfg.BaseURL,
		Model:   p.llmCfg.Model,
	})

	msgs := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(system),
		openai.UserMessage(user),
	}

	resp, err := agent.ChatText(ctx, msgs)
	if err != nil {
		return p.parseByKeyword(rawText, canvas, viewingPageID), nil
	}

	cleaned := strings.TrimSpace(resp)
	cleaned = strings.TrimPrefix(cleaned, "```json")
	cleaned = strings.TrimPrefix(cleaned, "```")
	cleaned = strings.TrimSuffix(cleaned, "```")
	cleaned = strings.TrimSpace(cleaned)

	var intents []model.Intent
	if err := json.Unmarshal([]byte(cleaned), &intents); err != nil {
		return p.parseByKeyword(rawText, canvas, viewingPageID), nil
	}

	for i := range intents {
		intents[i].ActionType = strings.TrimSpace(strings.ToLower(intents[i].ActionType))
		intents[i].TargetPageID = strings.TrimSpace(intents[i].TargetPageID)
		intents[i].Instruction = strings.TrimSpace(intents[i].Instruction)
		if intents[i].TargetPageID == "" {
			intents[i].TargetPageID = viewingPageID
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
