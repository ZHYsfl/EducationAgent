package protocol

import (
	"regexp"
	"strings"
)

type Action struct {
	Type   string
	Params map[string]string
}

type ParseResult struct {
	VisibleText string
	ThinkText   string
	Actions     []Action
}

type Parser struct {
	buffer           strings.Builder
	lastParsedLen    int          // 上次解析到的位置
	processedIndexes map[int]bool // 已处理的 action 起始位置
}

func NewParser() *Parser {
	return &Parser{
		processedIndexes: make(map[int]bool),
	}
}

var (
	actionRegex = regexp.MustCompile(`@\{([^}]+)\}`)
	thinkRegex  = regexp.MustCompile(`#\{([^}]+)\}`)
)

func (p *Parser) Feed(token string) ParseResult {
	p.buffer.WriteString(token)
	text := p.buffer.String()

	result := ParseResult{}

	actionMatches := actionRegex.FindAllStringSubmatchIndex(text, -1)
	for _, match := range actionMatches {
		start, end := match[0], match[1]

		// 优化：跳过已扫描过的位置（整个匹配已在之前处理过）
		if end <= p.lastParsedLen {
			continue
		}

		if p.processedIndexes[start] {
			continue
		}

		innerStart, innerEnd := match[2], match[3]
		actionStr := text[innerStart:innerEnd]
		action := parseAction(actionStr)
		if action != nil {
			result.Actions = append(result.Actions, *action)
			p.processedIndexes[start] = true
		}
	}

	p.lastParsedLen = len(text)

	visible := thinkRegex.ReplaceAllString(text, "")
	visible = actionRegex.ReplaceAllString(visible, "")
	result.VisibleText = visible

	return result
}

func parseAction(s string) *Action {
	parts := strings.Split(s, "|")
	if len(parts) == 0 {
		return nil
	}

	action := &Action{
		Type:   parts[0],
		Params: make(map[string]string),
	}

	for i := 1; i < len(parts); i++ {
		kv := strings.SplitN(parts[i], ":", 2)
		if len(kv) == 2 {
			action.Params[kv[0]] = kv[1]
		}
	}

	return action
}
