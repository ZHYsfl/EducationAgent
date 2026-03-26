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
	buffer strings.Builder
}

var (
	actionRegex = regexp.MustCompile(`@\{([^}]+)\}`)
	thinkRegex  = regexp.MustCompile(`#\{([^}]+)\}`)
)

func (p *Parser) Feed(token string) ParseResult {
	p.buffer.WriteString(token)
	text := p.buffer.String()

	result := ParseResult{}

	thinkMatches := thinkRegex.FindAllStringSubmatch(text, -1)
	for _, match := range thinkMatches {
		if len(match) > 1 {
			result.ThinkText += match[1]
		}
	}

	actionMatches := actionRegex.FindAllStringSubmatch(text, -1)
	for _, match := range actionMatches {
		if len(match) > 1 {
			action := parseAction(match[1])
			if action != nil {
				result.Actions = append(result.Actions, *action)
			}
		}
	}

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
