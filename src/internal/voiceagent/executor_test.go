package voiceagent

import (
	"testing"
)

func TestMapToRequirements(t *testing.T) {
	r, err := mapToRequirements(map[string]any{
		"topic":       "AI",
		"style":       "modern",
		"total_pages": 10,
		"audience":    "students",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.Topic == nil || *r.Topic != "AI" {
		t.Fatalf("unexpected topic")
	}
	if r.TotalPages == nil || *r.TotalPages != 10 {
		t.Fatalf("unexpected total_pages")
	}
}

func TestMapToRequirementsPartial(t *testing.T) {
	r, err := mapToRequirements(map[string]any{
		"topic": "AI",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r.Topic == nil || *r.Topic != "AI" {
		t.Fatalf("expected topic")
	}
	if r.Style != nil {
		t.Fatalf("expected nil style")
	}
}

func TestToolSchemas(t *testing.T) {
	schemas := (&Executor{}).ToolSchemas()
	if len(schemas) != 4 {
		t.Fatalf("expected 4 tool schemas, got %d", len(schemas))
	}
}
