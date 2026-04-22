package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/openai/openai-go/v3"
)

const (
	// pptCompressTokenThreshold is a rough budget: when estimated context tokens
	// exceed this, older turns are summarized into one user message.
	pptCompressTokenThreshold = 100_000
	// pptCompressTailMessages is how many recent messages to keep verbatim after each compression round.
	pptCompressTailMessages = 20
	// pptCompressMaxTranscriptBytes caps the text fed to the summarizer to avoid oversized requests.
	pptCompressMaxTranscriptBytes = 400_000
	pptCompressMaxRounds = 3
)

const pptCompressSystemPrompt = `You compress a PPT-agent conversation transcript for later turns.
Output a single concise summary in Chinese unless the transcript is clearly English-only.
Preserve: user goals and constraints, topic/style/pages/audience, file paths edited, commands run, errors and fixes, export status, and any pending user questions.
Omit: full file contents, long tool dumps (say only that a file was read/written and the outcome).
Do not invent facts. Use bullet points or short paragraphs.`

// estimatePPTTextTokens is a cheap UTF-8 length heuristic (~ bytes/3 suits mixed CJK/Latin for budget purposes).
func estimatePPTTextTokens(s string) int {
	if s == "" {
		return 0
	}
	return (len(s) + 2) / 3
}

func pptParamContentString(m openai.ChatCompletionMessageParamUnion) string {
	var b strings.Builder
	switch v := m.GetContent().AsAny().(type) {
	case *string:
		if v != nil {
			b.WriteString(*v)
		}
	case *[]openai.ChatCompletionContentPartTextParam:
		if v != nil {
			for _, p := range *v {
				b.WriteString(p.Text)
			}
		}
	case *[]openai.ChatCompletionContentPartUnionParam:
		if v != nil {
			for _, p := range *v {
				if t := p.GetText(); t != nil {
					b.WriteString(*t)
				}
			}
		}
	case *[]openai.ChatCompletionAssistantMessageParamContentArrayOfContentPartUnion:
		if v != nil {
			for _, p := range *v {
				if t := p.GetText(); t != nil {
					b.WriteString(*t)
				} else if r := p.GetRefusal(); r != nil {
					b.WriteString(*r)
				}
			}
		}
	default:
		// Multimodal or empty content
	}
	for _, tc := range m.GetToolCalls() {
		if fn := tc.GetFunction(); fn != nil {
			b.WriteString("\n[tool_call ")
			b.WriteString(fn.Name)
			b.WriteString(" ")
			b.WriteString(fn.Arguments)
			b.WriteString("]")
		}
	}
	return b.String()
}

func pptParamSummaryLine(m openai.ChatCompletionMessageParamUnion) string {
	role := "message"
	if r := m.GetRole(); r != nil {
		role = *r
	}
	body := pptParamContentString(m)
	if tcid := m.GetToolCallID(); tcid != nil && *tcid != "" {
		body = fmt.Sprintf("(tool_call_id=%s) %s", *tcid, body)
	}
	return role + ": " + body
}

func estimatePPTHistoryTokens(msgs []openai.ChatCompletionMessageParamUnion) int {
	n := 0
	for _, m := range msgs {
		n += estimatePPTTextTokens(pptParamSummaryLine(m))
		n += 8 // per-message overhead
	}
	return n
}

func (s *PPTService) compressPPTHistoryOnce(ctx context.Context, history []openai.ChatCompletionMessageParamUnion) ([]openai.ChatCompletionMessageParamUnion, bool) {
	if s.agent == nil || len(history) < 3 {
		return history, false
	}
	tailN := pptCompressTailMessages
	if len(history) <= 1+tailN {
		tailN = 1
	}
	midEnd := len(history) - tailN
	if midEnd <= 1 {
		return history, false
	}

	var sb strings.Builder
	for _, m := range history[1:midEnd] {
		sb.WriteString(pptParamSummaryLine(m))
		sb.WriteString("\n\n---\n\n")
	}
	transcript := sb.String()
	if len(transcript) > pptCompressMaxTranscriptBytes {
		transcript = transcript[:pptCompressMaxTranscriptBytes] + "\n\n[... transcript truncated for summarization ...]"
	}

	summary, err := s.agent.ChatText(ctx, []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(pptCompressSystemPrompt),
		openai.UserMessage(transcript),
	})
	if err != nil || strings.TrimSpace(summary) == "" {
		return history, false
	}

	out := make([]openai.ChatCompletionMessageParamUnion, 0, 2+tailN)
	out = append(out, history[0])
	out = append(out, openai.UserMessage("[Earlier PPT-agent context — compressed summary]\n"+strings.TrimSpace(summary)))
	out = append(out, history[midEnd:]...)
	return out, true
}

// compressPPTHistoryIfNeeded replaces long prefixes with a summary when estimated tokens exceed the threshold.
func (s *PPTService) compressPPTHistoryIfNeeded(ctx context.Context, history []openai.ChatCompletionMessageParamUnion) []openai.ChatCompletionMessageParamUnion {
	if s.agent == nil {
		return history
	}
	for round := 0; round < pptCompressMaxRounds; round++ {
		if estimatePPTHistoryTokens(history) <= pptCompressTokenThreshold {
			break
		}
		next, ok := s.compressPPTHistoryOnce(ctx, history)
		if !ok {
			break
		}
		history = next
		s.state.BroadcastPPTLog(fmt.Sprintf(
			"[memory] context compressed (round %d); est. ~%d tokens",
			round+1,
			estimatePPTHistoryTokens(history),
		))
	}
	if estimatePPTHistoryTokens(history) > pptCompressTokenThreshold && len(history) > pptCompressTailMessages+1 {
		history = append(
			[]openai.ChatCompletionMessageParamUnion{history[0]},
			history[len(history)-pptCompressTailMessages:]...,
		)
		s.state.BroadcastPPTLog("[memory] token budget fallback: kept system + last messages only")
	}
	return history
}
