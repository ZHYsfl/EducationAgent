package agent_test

import (
	agent "voiceagent/agent"
	"strings"
	"testing"
)

// ===========================================================================
// NewTaskRequirements
// ===========================================================================

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
	if req.CreatedAt == 0 {
		t.Error("CreatedAt should be non-zero")
	}
}

// ===========================================================================
// GetMissingFields
// ===========================================================================

func TestGetMissingFields_AllMissing(t *testing.T) {
	req := agent.NewTaskRequirements("s", "u")
	missing := req.GetMissingFields()
	expected := []string{"topic", "knowledge_points", "teaching_goals", "teaching_logic", "target_audience"}
	if len(missing) != len(expected) {
		t.Fatalf("missing = %v, want %v", missing, expected)
	}
	for i, f := range expected {
		if missing[i] != f {
			t.Errorf("missing[%d] = %q, want %q", i, missing[i], f)
		}
	}
}

func TestGetMissingFields_PartiallyFilled(t *testing.T) {
	req := agent.NewTaskRequirements("s", "u")
	req.Topic = "高等数学"
	req.TargetAudience = "大一学生"
	missing := req.GetMissingFields()
	if len(missing) != 3 {
		t.Fatalf("expected 3 missing, got %d: %v", len(missing), missing)
	}
	for _, m := range missing {
		if m == "topic" || m == "target_audience" {
			t.Errorf("should not report %q as missing", m)
		}
	}
}

func TestGetMissingFields_AllFilled(t *testing.T) {
	req := agent.NewTaskRequirements("s", "u")
	req.Topic = "线性代数"
	req.KnowledgePoints = []string{"矩阵", "行列式"}
	req.TeachingGoals = []string{"掌握矩阵运算"}
	req.TeachingLogic = "从特殊到一般"
	req.TargetAudience = "大一学生"
	missing := req.GetMissingFields()
	if len(missing) != 0 {
		t.Errorf("all filled but missing = %v", missing)
	}
}

func TestGetMissingFields_Nil(t *testing.T) {
	var req *agent.TaskRequirements
	missing := req.GetMissingFields()
	if len(missing) != 5 {
		t.Errorf("nil should return 5 missing, got %d", len(missing))
	}
}

// ===========================================================================
// IsReadyForConfirm
// ===========================================================================

func TestIsReadyForConfirm_NotReady(t *testing.T) {
	req := agent.NewTaskRequirements("s", "u")
	if req.IsReadyForConfirm() {
		t.Error("should not be ready when fields are missing")
	}
}

func TestIsReadyForConfirm_Ready(t *testing.T) {
	req := makeFullRequirements()
	if !req.IsReadyForConfirm() {
		t.Error("should be ready when all P0 fields are filled")
	}
}

// ===========================================================================
// RefreshCollectedFields
// ===========================================================================

func TestRefreshCollectedFields_Empty(t *testing.T) {
	req := agent.NewTaskRequirements("s", "u")
	req.RefreshCollectedFields()
	if len(req.CollectedFields) != 1 {
		t.Errorf("expected 1 collected (output_formats), got %d: %v", len(req.CollectedFields), req.CollectedFields)
	}
}

func TestRefreshCollectedFields_Full(t *testing.T) {
	req := makeFullRequirements()
	req.RefreshCollectedFields()
	if len(req.CollectedFields) < 5 {
		t.Errorf("expected at least 5 collected fields, got %d: %v", len(req.CollectedFields), req.CollectedFields)
	}
	foundTopic := false
	for _, f := range req.CollectedFields {
		if f == "topic" {
			foundTopic = true
		}
	}
	if !foundTopic {
		t.Error("expected topic in collected fields")
	}
}

func TestRefreshCollectedFields_Nil(t *testing.T) {
	var req *agent.TaskRequirements
	req.RefreshCollectedFields() // should not panic
}

func TestRefreshCollectedFields_OptionalFields(t *testing.T) {
	req := makeFullRequirements()
	req.KeyDifficulties = []string{"极限"}
	req.Duration = "2小时"
	req.TotalPages = 30
	req.GlobalStyle = "简洁蓝"
	req.InteractionDesign = "问答"
	req.AdditionalNotes = "特别注意"
	req.ReferenceFiles = []agent.ReferenceFileReq{{FileID: "f1"}}
	req.RefreshCollectedFields()
	if len(req.CollectedFields) < 12 {
		t.Errorf("expected 12+ collected fields with optional, got %d: %v", len(req.CollectedFields), req.CollectedFields)
	}
}

// ===========================================================================
// BuildRequirementsSystemPrompt
// ===========================================================================

func TestBuildRequirementsSystemPrompt_NoProfile(t *testing.T) {
	req := agent.NewTaskRequirements("s", "u")
	req.Topic = "微积分"
	prompt := req.BuildRequirementsSystemPrompt(nil)
	if !strings.Contains(prompt, "topic") {
		t.Error("prompt should mention topic in collected/missing fields")
	}
	if !strings.Contains(prompt, "暂无画像") {
		t.Error("prompt should say no profile")
	}
}

func TestBuildRequirementsSystemPrompt_WithProfile(t *testing.T) {
	req := agent.NewTaskRequirements("s", "u")
	req.Topic = "高等数学"
	profile := &agent.UserProfile{
		DisplayName:   "王老师",
		Subject:       "数学",
		TeachingStyle: "互动式",
	}
	prompt := req.BuildRequirementsSystemPrompt(profile)
	if !strings.Contains(prompt, "王老师") {
		t.Error("prompt should include profile name")
	}
	if !strings.Contains(prompt, "数学") {
		t.Error("prompt should include subject")
	}
}

func TestBuildRequirementsSystemPrompt_AllCollected(t *testing.T) {
	req := makeFullRequirements()
	prompt := req.BuildRequirementsSystemPrompt(nil)
	if !strings.Contains(prompt, "所有 P0 字段已收集完成") {
		t.Error("prompt should indicate all P0 fields collected")
	}
}

func TestBuildRequirementsSystemPrompt_Nil(t *testing.T) {
	var req *agent.TaskRequirements
	prompt := req.BuildRequirementsSystemPrompt(nil)
	if prompt != "" {
		t.Errorf("nil req should return empty prompt, got %q", prompt)
	}
}

// ===========================================================================
// formatProfileSummary
// ===========================================================================

func TestFormatProfileSummary_Nil(t *testing.T) {
	result := agent.FormatProfileSummary(nil)
	if result != "暂无画像信息。" {
		t.Errorf("nil profile: %q", result)
	}
}

func TestFormatProfileSummary_Empty(t *testing.T) {
	result := agent.FormatProfileSummary(&agent.UserProfile{})
	if result != "暂无画像信息。" {
		t.Errorf("empty profile: %q", result)
	}
}

func TestFormatProfileSummary_Populated(t *testing.T) {
	profile := &agent.UserProfile{
		DisplayName:       "张老师",
		Subject:           "物理",
		TeachingStyle:     "讲授式",
		HistorySummary:    "三年教龄",
		VisualPreferences: map[string]string{"color_scheme": "blue"},
	}
	result := agent.FormatProfileSummary(profile)
	if !strings.Contains(result, "张老师") || !strings.Contains(result, "物理") {
		t.Errorf("missing info: %q", result)
	}
}

// ===========================================================================
// BuildSummaryText
// ===========================================================================

func TestBuildSummaryText_Full(t *testing.T) {
	req := makeFullRequirements()
	req.KeyDifficulties = []string{"极限"}
	req.TotalPages = 20
	req.GlobalStyle = "商务蓝"
	summary := req.BuildSummaryText()
	if !strings.Contains(summary, "高等数学") {
		t.Error("summary missing topic")
	}
	if !strings.Contains(summary, "微分") {
		t.Error("summary missing knowledge_points")
	}
	if !strings.Contains(summary, "20 页") {
		t.Error("summary missing page count")
	}
	if !strings.Contains(summary, "理解对吗") || !strings.Contains(summary, "调整") {
		t.Error("summary should ask for confirmation")
	}
}

func TestBuildSummaryText_Nil(t *testing.T) {
	var req *agent.TaskRequirements
	if s := req.BuildSummaryText(); s != "" {
		t.Errorf("nil req summary should be empty, got %q", s)
	}
}

func TestBuildSummaryText_Minimal(t *testing.T) {
	req := agent.NewTaskRequirements("s", "u")
	req.Topic = "测试"
	summary := req.BuildSummaryText()
	if !strings.Contains(summary, "测试") {
		t.Error("summary missing topic")
	}
}

// ===========================================================================
// ToPPTInitRequest
// ===========================================================================

func TestToPPTInitRequest(t *testing.T) {
	req := makeFullRequirements()
	req.ReferenceFiles = []agent.ReferenceFileReq{
		{FileID: "f1", FileURL: "http://a.com/f1.pdf", FileType: "pdf", Instruction: "用作参考"},
	}
	initReq := req.ToPPTInitRequest()
	if initReq.UserID != req.UserID {
		t.Errorf("UserID = %q, want %q", initReq.UserID, req.UserID)
	}
	if initReq.Topic != req.Topic {
		t.Errorf("Topic = %q, want %q", initReq.Topic, req.Topic)
	}
	if initReq.TeachingElements == nil {
		t.Fatal("TeachingElements should not be nil")
	}
	if len(initReq.TeachingElements.KnowledgePoints) != len(req.KnowledgePoints) {
		t.Error("KnowledgePoints count mismatch")
	}
	if len(initReq.ReferenceFiles) != 1 {
		t.Fatalf("expected 1 reference file, got %d", len(initReq.ReferenceFiles))
	}
	if initReq.ReferenceFiles[0].FileID != "f1" {
		t.Error("reference file ID mismatch")
	}
}

func TestToPPTInitRequest_EmptyReferenceFiles(t *testing.T) {
	req := makeFullRequirements()
	initReq := req.ToPPTInitRequest()
	if initReq.ReferenceFiles == nil {
		t.Error("ReferenceFiles should be non-nil empty slice")
	}
}

func TestToPPTInitRequest_Description(t *testing.T) {
	req := makeFullRequirements()
	initReq := req.ToPPTInitRequest()
	if initReq.Description == "" {
		t.Error("Description should be non-empty")
	}
	if !strings.Contains(initReq.Description, "高等数学") {
		t.Error("Description should contain topic")
	}
}

// ===========================================================================
// toReferenceFiles
// ===========================================================================

func TestToReferenceFiles_Empty(t *testing.T) {
	result := agent.ToReferenceFiles(nil)
	if len(result) != 0 {
		t.Errorf("expected empty, got %d items", len(result))
	}
}

func TestToReferenceFiles_Conversion(t *testing.T) {
	input := []agent.ReferenceFileReq{
		{FileID: "f1", FileURL: "http://a.com", FileType: "pdf", Instruction: "参考"},
		{FileID: "f2", FileURL: "http://b.com", FileType: "docx", Instruction: "引用"},
	}
	output := agent.ToReferenceFiles(input)
	if len(output) != 2 {
		t.Fatalf("expected 2, got %d", len(output))
	}
	if output[0].FileID != "f1" || output[1].FileType != "docx" {
		t.Error("field mapping incorrect")
	}
}

// ===========================================================================
// buildDetailedDescription
// ===========================================================================

func TestBuildDetailedDescription_Full(t *testing.T) {
	req := makeFullRequirements()
	req.Duration = "2课时"
	req.TotalPages = 25
	req.AdditionalNotes = "包含练习题"
	desc := agent.BuildDetailedDescription(req)
	if !strings.Contains(desc, "高等数学") {
		t.Error("missing topic")
	}
	if !strings.Contains(desc, "微分") {
		t.Error("missing knowledge points")
	}
	if !strings.Contains(desc, "2课时") {
		t.Error("missing duration")
	}
	if !strings.Contains(desc, "练习题") {
		t.Error("missing additional notes")
	}
}

func TestBuildDetailedDescription_Nil(t *testing.T) {
	result := agent.BuildDetailedDescription(nil)
	if result != "" {
		t.Errorf("nil should return empty, got %q", result)
	}
}

func TestBuildDetailedDescription_ReferenceFiles(t *testing.T) {
	req := makeFullRequirements()
	req.ReferenceFiles = []agent.ReferenceFileReq{
		{FileID: "ref1", FileType: "pdf", Instruction: "参考教材"},
	}
	desc := agent.BuildDetailedDescription(req)
	if !strings.Contains(desc, "参考教材") {
		t.Error("missing reference files")
	}
}

// ===========================================================================
// Helper
// ===========================================================================

func makeFullRequirements() *agent.TaskRequirements {
	req := agent.NewTaskRequirements("sess_test", "user_test")
	req.Topic = "高等数学"
	req.KnowledgePoints = []string{"微分", "积分"}
	req.TeachingGoals = []string{"掌握求导", "理解定积分"}
	req.TeachingLogic = "先微分后积分"
	req.TargetAudience = "大一理工科学生"
	return req
}
