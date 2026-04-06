package agent_test

import (
	"strings"
	"testing"

	agent "voiceagent/agent"
)

func TestNewTaskRequirements(t *testing.T) {
	req := agent.NewTaskRequirements("sess_1", "user_1")
	if req.SessionID != "sess_1" {
		t.Errorf("SessionID = %q, want sess_1", req.SessionID)
	}
	if req.UserID != "user_1" {
		t.Errorf("UserID = %q, want user_1", req.UserID)
	}
	if req.Status != "collecting" {
		t.Errorf("Status = %q, want collecting", req.Status)
	}
	if len(req.OutputFormats) != 1 || req.OutputFormats[0] != "pptx" {
		t.Errorf("OutputFormats = %v, want [pptx]", req.OutputFormats)
	}
	if req.CreatedAt == 0 || req.UpdatedAt == 0 {
		t.Error("timestamps should be initialized")
	}
}

func TestGetMissingFields_AllMissing(t *testing.T) {
	req := agent.NewTaskRequirements("s", "u")
	got := req.GetMissingFields()
	want := []string{
		"topic",
		"subject",
		"audience",
		"knowledge_points",
		"teaching_goals",
		"teaching_logic",
		"key_difficulties",
		"duration",
		"total_pages",
		"global_style",
		"interaction_design",
	}

	if len(got) != len(want) {
		t.Fatalf("missing = %v, want = %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("missing[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestGetMissingFields_PartiallyFilled(t *testing.T) {
	req := agent.NewTaskRequirements("s", "u")
	req.Topic = "Calculus"
	req.Subject = "Math"
	req.TargetAudience = "Undergrads"

	got := req.GetMissingFields()
	if len(got) != 8 {
		t.Fatalf("expected 8 missing fields, got %d: %v", len(got), got)
	}
	for _, f := range got {
		if f == "topic" || f == "subject" || f == "audience" {
			t.Fatalf("field %q should not be missing", f)
		}
	}
}

func TestGetMissingFields_AllFilled(t *testing.T) {
	req := makeFullRequirements()
	got := req.GetMissingFields()
	if len(got) != 0 {
		t.Fatalf("expected no missing fields, got: %v", got)
	}
}

func TestGetMissingFields_Nil(t *testing.T) {
	var req *agent.TaskRequirements
	got := req.GetMissingFields()
	if len(got) != 12 {
		t.Fatalf("nil requirements should return 12 missing fields, got %d", len(got))
	}
	if got[len(got)-1] != "output_formats" {
		t.Fatalf("last missing field should be output_formats, got %q", got[len(got)-1])
	}
}

func TestIsReadyForConfirm_NotReady(t *testing.T) {
	req := agent.NewTaskRequirements("s", "u")
	if req.IsReadyForConfirm() {
		t.Fatal("should not be ready with missing required fields")
	}
}

func TestIsReadyForConfirm_Ready(t *testing.T) {
	req := makeFullRequirements()
	if !req.IsReadyForConfirm() {
		t.Fatal("should be ready when all required fields are present")
	}
}

func TestRefreshCollectedFields_Empty(t *testing.T) {
	req := agent.NewTaskRequirements("s", "u")
	req.RefreshCollectedFields()
	if len(req.CollectedFields) != 1 {
		t.Fatalf("expected only output_formats collected, got %d: %v", len(req.CollectedFields), req.CollectedFields)
	}
	if req.CollectedFields[0] != "output_formats" {
		t.Fatalf("expected collected field output_formats, got %q", req.CollectedFields[0])
	}
}

func TestRefreshCollectedFields_Full(t *testing.T) {
	req := makeFullRequirements()
	req.RefreshCollectedFields()
	if len(req.CollectedFields) != 12 {
		t.Fatalf("expected 12 collected fields, got %d: %v", len(req.CollectedFields), req.CollectedFields)
	}
}

func TestRefreshCollectedFields_Nil(t *testing.T) {
	var req *agent.TaskRequirements
	req.RefreshCollectedFields()
}

func TestRefreshCollectedFields_OptionalFields(t *testing.T) {
	req := makeFullRequirements()
	req.AdditionalNotes = "extra notes"
	req.ReferenceFiles = []agent.ReferenceFileReq{{FileID: "f1"}}
	req.RefreshCollectedFields()
	if len(req.CollectedFields) != 14 {
		t.Fatalf("expected 14 collected fields (required + optional), got %d: %v", len(req.CollectedFields), req.CollectedFields)
	}
}

func TestBuildRequirementsSystemPrompt_NoProfile(t *testing.T) {
	req := agent.NewTaskRequirements("s", "u")
	req.Topic = "Calculus"

	prompt := req.BuildRequirementsSystemPrompt(nil)
	if !strings.Contains(prompt, "topic") {
		t.Fatal("prompt should include requirements field info")
	}
	if !strings.Contains(prompt, "画像") {
		t.Fatal("prompt should mention missing profile summary")
	}
}

func TestBuildRequirementsSystemPrompt_WithProfile(t *testing.T) {
	req := agent.NewTaskRequirements("s", "u")
	req.Topic = "Calculus"
	profile := &agent.UserProfile{
		DisplayName:   "Teacher Wang",
		Subject:       "Math",
		TeachingStyle: "interactive",
	}

	prompt := req.BuildRequirementsSystemPrompt(profile)
	if !strings.Contains(prompt, "Teacher Wang") {
		t.Fatal("prompt should include profile display name")
	}
	if !strings.Contains(prompt, "Math") {
		t.Fatal("prompt should include profile subject")
	}
}

func TestBuildRequirementsSystemPrompt_AllCollected(t *testing.T) {
	req := makeFullRequirements()
	prompt := req.BuildRequirementsSystemPrompt(nil)

	if !strings.Contains(prompt, "P0") {
		t.Fatalf("prompt should include ready marker for P0 fields, got: %q", prompt)
	}
}

func TestBuildRequirementsSystemPrompt_Nil(t *testing.T) {
	var req *agent.TaskRequirements
	prompt := req.BuildRequirementsSystemPrompt(nil)
	if prompt != "" {
		t.Fatalf("nil requirements should return empty prompt, got %q", prompt)
	}
}

func TestFormatProfileSummary_Nil(t *testing.T) {
	result := agent.FormatProfileSummary(nil)
	if !strings.Contains(result, "画像") {
		t.Fatalf("nil profile should report empty profile summary, got %q", result)
	}
}

func TestFormatProfileSummary_Empty(t *testing.T) {
	result := agent.FormatProfileSummary(&agent.UserProfile{})
	if !strings.Contains(result, "画像") {
		t.Fatalf("empty profile should report empty profile summary, got %q", result)
	}
}

func TestFormatProfileSummary_Populated(t *testing.T) {
	profile := &agent.UserProfile{
		DisplayName:       "Teacher Zhang",
		Subject:           "Physics",
		TeachingStyle:     "lecture",
		HistorySummary:    "3 years teaching",
		VisualPreferences: map[string]string{"color_scheme": "blue"},
	}
	result := agent.FormatProfileSummary(profile)
	if !strings.Contains(result, "Teacher Zhang") || !strings.Contains(result, "Physics") {
		t.Fatalf("summary missing expected profile info, got %q", result)
	}
}

func makeFullRequirements() *agent.TaskRequirements {
	req := agent.NewTaskRequirements("sess_test", "user_test")
	req.Topic = "Calculus"
	req.Subject = "Math"
	req.TargetAudience = "Undergrads"
	req.KnowledgePoints = []string{"derivative", "integral"}
	req.TeachingGoals = []string{"understand derivative", "understand integral"}
	req.TeachingLogic = "from concept to examples"
	req.KeyDifficulties = []string{"chain rule"}
	req.Duration = "45min"
	req.TotalPages = 20
	req.GlobalStyle = "clean"
	req.InteractionDesign = "quiz"
	return req
}
