package extractor

import (
	"context"
	"errors"
	"strings"
	"testing"

	"memory_service/internal/model"
)

type stubLLMClient struct {
	resp LLMResponse
	err  error
}

func (s stubLLMClient) ExtractRequirementDialogue(_ context.Context, _ LLMRequest) (LLMResponse, error) {
	if s.err != nil {
		return LLMResponse{}, s.err
	}
	return s.resp, nil
}

func TestRuleBasedExtractorRequirementDialogueFocus(t *testing.T) {
	ex := RuleBasedExtractor{}
	res, err := ex.Extract("user_u1", "sess_1", []model.ConversationTurn{
		{Role: "user", Content: "I teach biology at a high school and I prefer a clean minimalist PPT style."},
		{Role: "user", Content: "The knowledge points include cell structure, membrane transport, and osmosis."},
		{Role: "user", Content: "The teaching goals are helping students explain osmosis and compare passive and active transport."},
		{Role: "user", Content: "This lesson is for grade 10 students and should fit into 45 minutes."},
		{Role: "user", Content: "First explain cell membranes, then compare examples, and finally use a short quiz."},
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(res.Facts) == 0 {
		t.Fatalf("expected durable teacher facts")
	}
	if len(res.Preferences) == 0 {
		t.Fatalf("expected durable teacher preferences")
	}
	if len(res.TeachingElements().KnowledgePoints) == 0 || len(res.TeachingElements().TeachingGoals) == 0 {
		t.Fatalf("expected teaching elements from requirement dialogue")
	}
	if res.TeachingElements().TargetAudience == "" || res.TeachingElements().Duration == "" {
		t.Fatalf("expected target audience and duration")
	}
	if !strings.Contains(res.ConversationSummary, "Requirement collection progress") {
		t.Fatalf("summary should preserve requirement collection progress")
	}
}

func TestHybridExtractorDisabledModeUsesRulesOnly(t *testing.T) {
	ex := NewHybridExtractor(Config{EnableLLM: false}, stubLLMClient{
		resp: LLMResponse{
			ConversationSummary: "should not be used",
		},
	})
	res, err := ex.Extract("user_u1", "sess_1", []model.ConversationTurn{
		{Role: "user", Content: "I teach biology and prefer a clean style."},
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if strings.Contains(res.ConversationSummary, "should not be used") {
		t.Fatalf("disabled llm mode should stay on rule-only path")
	}
}

func TestHybridExtractorFallsBackOnLLMError(t *testing.T) {
	ex := NewHybridExtractor(Config{EnableLLM: true}, stubLLMClient{err: errors.New("boom")})
	res, err := ex.Extract("user_u1", "sess_1", []model.ConversationTurn{
		{Role: "user", Content: "I teach history and I prefer an academic style."},
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(res.Facts) == 0 || len(res.Preferences) == 0 {
		t.Fatalf("fallback should return rule-based extraction")
	}
}

func TestHybridExtractorMergesValidatedLLMOutput(t *testing.T) {
	ex := NewHybridExtractor(Config{EnableLLM: true}, stubLLMClient{
		resp: LLMResponse{
			Preferences: []model.MemoryEntry{
				{Key: "output_style", Value: "academic blue theme", Context: "visual_preferences", Confidence: 0.9, Source: "inferred"},
			},
			TeachingElements: model.TeachingElements{
				KnowledgePoints: []string{"limits", "continuity"},
				TeachingGoals:   []string{"help students explain continuity"},
				TargetAudience:  "first-year calculus students",
			},
			ConversationSummary: "Requirement collection progress: knowledge_points, teaching_goals, target_audience",
		},
	})
	res, err := ex.Extract("user_u1", "sess_1", []model.ConversationTurn{
		{Role: "user", Content: "The lesson covers continuity for first-year calculus students."},
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(res.Preferences) == 0 {
		t.Fatalf("expected llm preference merge")
	}
	if got := res.TeachingElements().TargetAudience; got != "first-year calculus students" {
		t.Fatalf("unexpected target audience: %q", got)
	}
	if !strings.Contains(res.ConversationSummary, "knowledge_points") {
		t.Fatalf("expected llm summary to override fallback summary")
	}
}

func TestHybridExtractorRejectsInvalidLLMStructuredOutput(t *testing.T) {
	ex := NewHybridExtractor(Config{EnableLLM: true}, stubLLMClient{
		resp: LLMResponse{
			Facts: []model.MemoryEntry{
				{Key: "", Value: "invalid fact"},
			},
			ConversationSummary: "should not win",
		},
	})
	res, err := ex.Extract("user_u1", "sess_1", []model.ConversationTurn{
		{Role: "user", Content: "I teach chemistry."},
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(res.Facts) == 0 {
		t.Fatalf("expected rule fallback facts")
	}
	if strings.Contains(res.ConversationSummary, "should not win") {
		t.Fatalf("invalid llm output should not replace fallback summary")
	}
}

func TestHybridExtractorNormalizesMultiFieldTeacherUtteranceBetterThanRules(t *testing.T) {
	dialogue := []model.ConversationTurn{
		{
			Role: "user",
			Content: "We're preparing Newton's laws for grade 8 students; the goal is to help them connect force and motion, " +
				"the key difficulty is friction misconceptions, it should fit into 40 minutes, and for this lesson use a clean dark-blue style.",
		},
	}

	ruleOnly := NewHybridExtractor(Config{EnableLLM: false}, nil)
	ruleRes, err := ruleOnly.Extract("user_u1", "sess_1", dialogue)
	if err != nil {
		t.Fatalf("rule extract: %v", err)
	}

	hybrid := NewHybridExtractor(Config{EnableLLM: true}, stubLLMClient{
		resp: LLMResponse{
			TeachingElements: model.TeachingElements{
				TeachingGoals:   []string{"help students connect force and motion"},
				KeyDifficulties: []string{"friction misconceptions"},
				TargetAudience:  "grade 8 students",
				Duration:        "40 minutes",
				OutputStyle:     "clean dark-blue style",
			},
			ConversationSummary: "Requirement collection progress: topic, target_audience, teaching_goals, duration, output_style, key_difficulties | topic: Newton's laws",
		},
	})
	hybridRes, err := hybrid.Extract("user_u1", "sess_1", dialogue)
	if err != nil {
		t.Fatalf("hybrid extract: %v", err)
	}

	if strings.Contains(ruleRes.ConversationSummary, "topic: Newton's laws") {
		t.Fatalf("expected rule-only path to miss the implicit topic in this compressed utterance")
	}
	hasDifficulty := false
	for _, item := range hybridRes.TeachingElements().KeyDifficulties {
		if strings.Contains(strings.ToLower(item), "friction") {
			hasDifficulty = true
			break
		}
	}
	if !hasDifficulty {
		t.Fatalf("expected hybrid path to normalize the key difficulty, got %#v", hybridRes.TeachingElements().KeyDifficulties)
	}
	if hybridRes.TeachingElements().OutputStyle == "" {
		t.Fatalf("expected hybrid path to normalize output style")
	}
	if !strings.Contains(hybridRes.ConversationSummary, "topic: Newton's laws") {
		t.Fatalf("expected improved hybrid summary")
	}
}
<<<<<<< HEAD
=======

func TestRuleBasedExtractorSupportsChineseLessonPrepSignals(t *testing.T) {
	ex := RuleBasedExtractor{}
	res, err := ex.Extract("user_u1", "sess_cn", []model.ConversationTurn{
		{Role: "user", Content: "这节课讲牛顿第一定律，知识点包括惯性、受力分析。"},
		{Role: "user", Content: "教学目标是让学生理解惯性，面向高一学生，时长45分钟。"},
		{Role: "user", Content: "这次课件用深蓝简洁风格，只用教材里的图。"},
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	signals := res.TaskStateSignals()
	if signals.LessonTopic == "" {
		t.Fatalf("expected chinese topic signal")
	}
	if len(signals.KnowledgePoints) == 0 || len(signals.TeachingGoals) == 0 {
		t.Fatalf("expected chinese structured task signals, got %#v", signals)
	}
	if signals.Duration != "45分钟" {
		t.Fatalf("expected chinese duration extraction, got %q", signals.Duration)
	}
	if len(signals.ReferenceMaterialUsage) == 0 {
		t.Fatalf("expected chinese reference usage signal")
	}
}
>>>>>>> origin/wang
