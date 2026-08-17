package voiceagent

import (
	"context"
	"encoding/json"
	"fmt"

	"educationagent/internal/model"
)

// VoiceService is the subset of service.VoiceService used by the executor.
// It is defined here to avoid an import cycle.
type VoiceService interface {
	UpdateRequirements(req map[string]any) (*model.UpdateRequirementsData, error)
	RequireConfirm(req model.Requirements) error
	SendToPPTAgent(data string) error
	GetMessagesFromPPTAgent() (string, error)
}

// Executor runs voice-agent tool calls through the Module 1 endpoints.
type Executor struct {
	tools map[string]func(ctx context.Context, args map[string]any) (string, error)
}

// NewExecutor creates an executor backed by the voice service.
func NewExecutor(voiceSvc VoiceService) *Executor {
	e := &Executor{
		tools: make(map[string]func(ctx context.Context, args map[string]any) (string, error)),
	}
	e.registerTools(voiceSvc)
	return e
}

// Execute runs a parsed tool call and returns its name and result.
func (e *Executor) Execute(ctx context.Context, tc ToolCall) (string, string, error) {
	fn, ok := e.tools[tc.Name]
	if !ok {
		return tc.Name, "", fmt.Errorf("unknown tool: %s", tc.Name)
	}
	result, err := fn(ctx, tc.Arguments)
	return tc.Name, result, err
}

// ToolSchema describes one tool for the Qwen3 <tools> section.
type ToolSchema struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// ToolSchemas returns the tool definitions as Qwen3 tool schemas.
func (e *Executor) ToolSchemas() []ToolSchema {
	return []ToolSchema{
		{
			Name:        "update_requirements",
			Description: "Update one or more PPT requirement fields and return the remaining missing fields.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"topic":       map[string]any{"type": "string", "description": "PPT topic"},
					"style":       map[string]any{"type": "string", "description": "PPT visual style"},
					"total_pages": map[string]any{"type": "integer", "description": "Total number of pages"},
					"audience":    map[string]any{"type": "string", "description": "Target audience"},
				},
			},
		},
		{
			Name:        "require_confirm",
			Description: "Send the finalized requirements to the frontend for user confirmation.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"requirements": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"topic":       map[string]any{"type": "string"},
							"style":       map[string]any{"type": "string"},
							"total_pages": map[string]any{"type": "integer"},
							"audience":    map[string]any{"type": "string"},
						},
						"required": []string{"topic", "style", "total_pages", "audience"},
					},
				},
				"required": []string{"requirements"},
			},
		},
		{
			Name:        "send_to_ppt_agent",
			Description: "Forward data, feedback, or instructions to the PPT agent.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"data": map[string]any{"type": "string", "description": "The information to forward"},
				},
				"required": []string{"data"},
			},
		},
		{
			Name:        "get_messages_from_ppt_agent",
			Description: "Pull messages queued from the PPT agent.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{},
			},
		},
	}
}

func (e *Executor) registerTools(voiceSvc VoiceService) {
	e.tools["update_requirements"] = func(ctx context.Context, args map[string]any) (string, error) {
		data, err := voiceSvc.UpdateRequirements(args)
		if err != nil {
			return "", err
		}
		if len(data.MissingFields) == 0 {
			return "all fields are updated", nil
		}
		b, _ := json.Marshal(data.MissingFields)
		return string(b), nil
	}

	e.tools["require_confirm"] = func(ctx context.Context, args map[string]any) (string, error) {
		reqMap, ok := args["requirements"].(map[string]any)
		if !ok {
			return "", fmt.Errorf("missing requirements argument")
		}
		req, err := mapToRequirements(reqMap)
		if err != nil {
			return "", err
		}
		if err := voiceSvc.RequireConfirm(req); err != nil {
			return "", err
		}
		b, _ := json.Marshal(req)
		return "require_confirm:" + string(b), nil
	}

	e.tools["send_to_ppt_agent"] = func(ctx context.Context, args map[string]any) (string, error) {
		data, _ := args["data"].(string)
		if err := voiceSvc.SendToPPTAgent(data); err != nil {
			return "", err
		}
		return "data is sent to the ppt agent successfully", nil
	}

	e.tools["get_messages_from_ppt_agent"] = func(ctx context.Context, args map[string]any) (string, error) {
		msgs, err := voiceSvc.GetMessagesFromPPTAgent()
		if err != nil {
			return "", err
		}
		if msgs == "" {
			return "no messages", nil
		}
		return msgs, nil
	}
}

func mapToRequirements(m map[string]any) (model.Requirements, error) {
	var r model.Requirements
	if v, ok := m["topic"].(string); ok {
		r.Topic = &v
	}
	if v, ok := m["style"].(string); ok {
		r.Style = &v
	}
	if v, ok := m["total_pages"]; ok {
		switch n := v.(type) {
		case int:
			r.TotalPages = &n
		case int64:
			i := int(n)
			r.TotalPages = &i
		case float64:
			i := int(n)
			r.TotalPages = &i
		case float32:
			i := int(n)
			r.TotalPages = &i
		default:
			return r, fmt.Errorf("total_pages must be a number")
		}
	}
	if v, ok := m["audience"].(string); ok {
		r.Audience = &v
	}
	return r, nil
}
