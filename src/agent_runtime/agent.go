package agent_runtime

import (
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"
)

type Agent struct {
	client openai.Client
	config *LLMConfig
	tools []*Tool
	debug bool
	maxToolRetries int
}

type AgentOption func(*Agent)

func WithDebug(debug bool) AgentOption {
	return func(a *Agent) {
		a.debug = debug
	}
}

func WithMaxToolRetries(maxToolRetries int) AgentOption {
	return func(a *Agent) {
		a.maxToolRetries = maxToolRetries
	}
}

func NewAgent(config *LLMConfig, tools []*Tool, opts ...AgentOption) *Agent {
	client := NewOpenAIClient(config)

	agent := &Agent{
		client: client,
		tools:  tools,
		config: config,
		debug:  false,
		maxToolRetries: 3,
	}

	for _, opt := range opts {
		opt(agent)
	}

	return agent
}

func (a *Agent) AddTool(tool *Tool) {
	a.tools = append(a.tools, tool)
}

func (a *Agent) RemoveTool(name string) {
	filtered := make([]*Tool, 0, len(a.tools))
	for _, t := range a.tools {
		if t.Name != name {
			filtered = append(filtered, t)
		}
	}
	a.tools = filtered
}

func (a *Agent) GetTools() []openai.ChatCompletionToolUnionParam {
	out := make([]openai.ChatCompletionToolUnionParam, len(a.tools))
	for i, t := range a.tools { 
		out[i] = openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
			Name:        t.Name,
			Description: openai.String(t.Description),
			Parameters:  shared.FunctionParameters(t.Parameters),
		})
	}
	return out
}