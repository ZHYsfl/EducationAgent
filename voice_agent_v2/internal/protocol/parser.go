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
	VisibleText   string
	ThinkText     string
	Actions       []Action
	HasOpenAction bool
}

type Parser struct {
	buffer           strings.Builder
	lastParsedLen    int
	processedIndexes map[int]bool
}

func NewParser() *Parser {
	return &Parser{processedIndexes: make(map[int]bool)}
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

	result.VisibleText = StripProtocolText(text)
	result.HasOpenAction = HasOpenActionPrefix(text)
	return result
}

// StripProtocolText removes #{...} and @{...} blocks from text.
func StripProtocolText(text string) string {
	visible := thinkRegex.ReplaceAllString(text, "")
	visible = actionRegex.ReplaceAllString(visible, "")
	return visible
}

// HasOpenActionPrefix returns true if text contains an unclosed @{... block,
// or a dangling "@" tail that may start an action in the next chunk.
func HasOpenActionPrefix(text string) bool {
	open := 0
	for i := 0; i < len(text); i++ {
		if i+1 < len(text) && text[i] == '@' && text[i+1] == '{' {
			open++
			i++
			continue
		}
		if text[i] == '}' && open > 0 {
			open--
		}
	}
	if open > 0 {
		return true
	}
	return len(text) > 0 && text[len(text)-1] == '@'
}

// TrimTrailingIncompleteAction drops the trailing incomplete action fragment.
// It returns (trimmed, changed).
func TrimTrailingIncompleteAction(text string) (string, bool) {
	if text == "" {
		return text, false
	}

	open := 0
	lastOpenIdx := -1
	for i := 0; i < len(text); i++ {
		if i+1 < len(text) && text[i] == '@' && text[i+1] == '{' {
			if open == 0 {
				lastOpenIdx = i
			}
			open++
			i++
			continue
		}
		if text[i] == '}' && open > 0 {
			open--
			if open == 0 {
				lastOpenIdx = -1
			}
		}
	}
	if open > 0 && lastOpenIdx >= 0 {
		return text[:lastOpenIdx], true
	}
	if text[len(text)-1] == '@' {
		return text[:len(text)-1], true
	}
	return text, false
}

func parseAction(s string) *Action {
	parts := strings.Split(s, "|")
	if len(parts) == 0 {
		return nil
	}
	action := &Action{Type: parts[0], Params: make(map[string]string)}
	for i := 1; i < len(parts); i++ {
		kv := strings.SplitN(parts[i], ":", 2)
		if len(kv) == 2 {
			action.Params[kv[0]] = kv[1]
		}
	}
	return action
}
