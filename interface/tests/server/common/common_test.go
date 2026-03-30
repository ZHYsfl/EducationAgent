package common_test

import (
	"testing"

	"multimodal-teaching-agent/internal/server"
)

func TestParseUserID(t *testing.T) {
	t.Parallel()

	if got := server.ParseUserID("Bearer user_123"); got != "user_123" {
		t.Fatalf("expected user_123, got %q", got)
	}
	if got := server.ParseUserID("Bearer abc"); got != "" {
		t.Fatalf("expected empty for invalid user prefix, got %q", got)
	}
	if got := server.ParseUserID(""); got != "" {
		t.Fatalf("expected empty for blank auth, got %q", got)
	}
}

func TestDetectFileType(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"a.pdf":  "pdf",
		"a.docx": "docx",
		"a.pptx": "pptx",
		"a.png":  "image",
		"a.mp4":  "video",
		"a.html": "html",
		"a.xyz":  "other",
	}
	for in, want := range cases {
		if got := server.DetectFileType(in); got != want {
			t.Fatalf("DetectFileType(%q)=%q want=%q", in, got, want)
		}
	}
}

func TestParsePositiveInt(t *testing.T) {
	t.Parallel()

	if got := server.ParsePositiveInt("5", 1); got != 5 {
		t.Fatalf("expected 5, got %d", got)
	}
	if got := server.ParsePositiveInt("0", 3); got != 3 {
		t.Fatalf("expected default 3, got %d", got)
	}
	if got := server.ParsePositiveInt("bad", 9); got != 9 {
		t.Fatalf("expected default 9, got %d", got)
	}
}

func TestHostOfAndTitleHelpers(t *testing.T) {
	t.Parallel()

	if got := server.HostOf("https://example.com/path"); got != "example.com" {
		t.Fatalf("unexpected host: %q", got)
	}
	if got := server.HostOf(":::"); got != "unknown" {
		t.Fatalf("expected unknown host, got %q", got)
	}

	if got := server.TitleOrDefault("", "这是一个很长很长很长很长很长的文本"); got == "" {
		t.Fatal("expected non-empty fallback title")
	}
	if got := server.TitleOrDefault("ok", "ignored"); got != "ok" {
		t.Fatalf("expected explicit title, got %q", got)
	}
}

func TestAllowedEnums(t *testing.T) {
	t.Parallel()

	if !server.IsAllowedPurpose("reference") || server.IsAllowedPurpose("bad") {
		t.Fatal("IsAllowedPurpose unexpected result")
	}
	if !server.IsAllowedSessionStatus("active") || server.IsAllowedSessionStatus("bad") {
		t.Fatal("IsAllowedSessionStatus unexpected result")
	}
}

func TestNormalizeSearchType(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"":                   "general",
		"general":            "general",
		"academic":           "academic",
		"teaching_resource":  "teaching_resource",
		"teaching-resources": "teaching_resource",
		"teaching":           "teaching_resource",
		"unknown":            "general",
	}
	for in, want := range cases {
		if got := server.NormalizeSearchType(in); got != want {
			t.Fatalf("NormalizeSearchType(%q)=%q want=%q", in, got, want)
		}
	}
}

func TestNormalizeStoredStatusForSection8(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"pending":    "pending",
		"completed":  "completed",
		"failed":     "failed",
		"success":    "completed",
		"partial":    "completed",
		"SUCCESS":    "completed",
		"  partial ": "completed",
		"":           "failed",
		"weird":      "completed",
	}
	for in, want := range cases {
		if got := server.NormalizeStoredStatusForSection8(in); got != want {
			t.Fatalf("NormalizeStoredStatusForSection8(%q)=%q want=%q", in, got, want)
		}
	}
}

func TestRefineSnippetAndSummary(t *testing.T) {
	t.Parallel()

	if got := server.RefineSnippet("", "查询词"); got == "" {
		t.Fatal("expected fallback snippet for empty input")
	}

	long := "<a>" + string(make([]rune, 300)) + "</a>"
	refined := server.RefineSnippet(long, "q")
	if refined == "" {
		t.Fatal("expected refined snippet not empty")
	}

	summary := server.BuildSummary("Go", []server.SearchResultItem{{Title: "t1"}, {Title: "t2"}})
	if summary == "" {
		t.Fatal("expected non-empty summary")
	}
	if server.BuildSummary("Go", nil) != "" {
		t.Fatal("expected empty summary for empty items")
	}
}

func TestEmptyToNilAndToString(t *testing.T) {
	t.Parallel()

	if got := server.EmptyToNil("   "); got != nil {
		t.Fatal("expected nil for blank input")
	}
	if got := server.EmptyToNil("abc"); got == nil || *got != "abc" {
		t.Fatal("expected non-nil pointer for non-empty input")
	}

	if got := server.ToString("  x  "); got != "x" {
		t.Fatalf("expected trimmed string, got %q", got)
	}
	if got := server.ToString(123); got != "" {
		t.Fatalf("expected empty string for non-string input, got %q", got)
	}
}
