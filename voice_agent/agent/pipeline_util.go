package agent

import (
	"context"
	"log"
	"strings"
	"toolcalling"
	"unicode"

	"voiceagent/internal/think"

	"github.com/openai/openai-go/v3"
)

const interruptDetectionPrompt = `你是语音意图检测模型。给定一段 ASR 识别文本，判断其是否包含有意义的用户意图。
- 输出 "interrupt"：文本包含实际语义（问题、指令、陈述、确认、否定，即使是未说完的半句）。
- 输出 "do not interrupt"：文本仅是无语义噪声（语气词：嗯、啊、哦、呃、emm；咳嗽/笑声误识别；重复填充音；空白或乱码）。
只输出 interrupt 或 do not interrupt，不要输出任何其他内容。`

func isInterrupt(ctx context.Context, agent *toolcalling.Agent, text string) bool {
	messages := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(interruptDetectionPrompt),
		openai.UserMessage(text),
	}
	resp, err := agent.ChatText(ctx, messages)
	if err != nil {
		log.Printf("interrupt detection error: %v", err)
		return true
	}
	// Small LLM is a thinking model (Qwen3.5-0.8B); strip <think>...</think>
	// before parsing the label, otherwise reasoning content that happens to
	// contain "interrupt" would cause false positives.
	resp = think.StripThinkTags(resp)
	label := strings.ToLower(strings.TrimSpace(resp))
	label = strings.Trim(label, " \t\r\n\"'`.,!?;:()[]{}<>，。！？；、")

	// Prefer exact label match first.
	switch label {
	case "interrupt":
		return true
	case "do not interrupt", "do-not-interrupt", "donotinterrupt":
		return false
	}

	// Fallback for noisy outputs, e.g. "interrupt." / "do not interrupt\n".
	if strings.Contains(label, "do not interrupt") || strings.Contains(label, "do-not-interrupt") {
		return false
	}
	if strings.Contains(label, "interrupt") {
		return true
	}

	// Conservative fallback: treat unknown output as interrupt.
	log.Printf("interrupt detection unexpected output: %q", resp)
	return true
}

var sentenceEnders = map[rune]bool{
	'。': true, '！': true, '？': true, '；': true,
	'.': true, '!': true, '?': true, ';': true,
	'，': true, ',': true, // also split on commas for faster streaming
	'\n': true,
}

func isSentenceEnd(s string) bool {
	s = strings.TrimRightFunc(s, unicode.IsSpace)
	runes := []rune(s)
	if len(runes) == 0 {
		return false
	}
	return sentenceEnders[runes[len(runes)-1]]
}

func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}

const pptIntentDetectionPrompt = `
[系统指令 - PPT 操作意图识别]
当用户表达想制作/创建 PPT 课件的意图时，请在回复末尾追加标记：
[TASK_INIT]{"topic":"用户提到的主题（如有）"}[/TASK_INIT]
例如用户说“帮我做一个关于高等数学的 PPT”，你在正常回复后追加：
[TASK_INIT]{"topic":"高等数学"}[/TASK_INIT]

当用户对已有 PPT 提出修改/编辑/调整指令时，请在回复末尾追加标记：
[PPT_FEEDBACK]{"action_type":"modify|insert|delete|reorder|style","page_id":"","instruction":"用户的具体修改要求","scope":"page|global","keywords":[]}[/PPT_FEEDBACK]
action_type 取值：modify(修改内容)、insert(新增页面)、delete(删除页面)、reorder(调整顺序)、style(修改样式)
page_id：如果用户指定了某一页就填入，否则留空
scope：page(只改某页) 或 global(全局修改)

注意：这些标记不会展示给用户，仅供系统后处理使用。正常对话内容中不要提及这些标记。`
