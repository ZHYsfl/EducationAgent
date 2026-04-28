package service

import (
	"context"
	"fmt"
	"strings"

	"educationagent/internal/toolcalling"

	"github.com/openai/openai-go/v3"
)

// InterruptService decides whether a user utterance is a real interruption.
type InterruptService interface {
	Check(ctx context.Context, transcript string) (bool, error)
}

// LLMInterruptService uses a small LLM call for the interrupt check.
type LLMInterruptService struct {
	agent *toolcalling.Agent
}

// NewInterruptService creates a new interrupt checker from environment config.
func NewInterruptService(cfg toolcalling.LLMConfig) InterruptService {
	return &LLMInterruptService{
		agent: toolcalling.NewAgent(cfg),
	}
}

// Check asks the LLM whether the transcript is a real interruption.
// It returns true for "yes" and false for everything else.
func (s *LLMInterruptService) Check(ctx context.Context, transcript string) (bool, error) {
	prompt := fmt.Sprintf(
		`You are an interrupt detector for a voice assistant. `+
			`A user just said: "%s". `+
			`Is this a real interruption (the user wants to cut in and change topic or give a new command)? `+
			`Reply with exactly one word: "yes" or "no".`,
		transcript,
	)
	resp, err := s.agent.ChatText(ctx, []openai.ChatCompletionMessageParamUnion{
		openai.UserMessage(prompt),
	})
	if err != nil {
		return false, err
	}
	return strings.EqualFold(strings.TrimSpace(resp), "yes"), nil
}
