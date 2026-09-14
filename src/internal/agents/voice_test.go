package agents

import (
	"context"
	"strings"
	"sync"
	"testing"
)

func TestQueuePushDrainEmpty(t *testing.T) {
	q := NewQueue()
	if !q.Empty() || q.Len() != 0 {
		t.Fatal("fresh queue not empty")
	}
	q.Push("a")
	q.Push("b")
	if q.Empty() || q.Len() != 2 {
		t.Fatal("push not visible")
	}
	got := q.Drain()
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("drain = %v", got)
	}
	if !q.Empty() || len(q.Drain()) != 0 {
		t.Fatal("drain did not clear")
	}
}

func TestQueueConcurrent(t *testing.T) {
	q := NewQueue()
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				q.Push("x")
				q.Len()
				q.Empty()
				q.Drain()
			}
		}(i)
	}
	wg.Wait()
}

func TestRequirementsUpdateValidation(t *testing.T) {
	r := &Requirements{}
	missing, err := r.Update(map[string]any{"topic": "西湖"})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if len(missing) != 3 || missing[0] != "style" {
		t.Fatalf("missing = %v", missing)
	}
	if r.Complete() {
		t.Fatal("should not be complete")
	}

	if _, err := r.Update(map[string]any{"bogus": "x"}); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown field must error, got %v", err)
	}
	// rejected wholesale: previous valid writes kept, bogus not applied
	if r.Topic != "西湖" {
		t.Fatal("valid write lost after bogus rejection")
	}

	missing, err = r.Update(map[string]any{
		"style":       "简洁",
		"total_pages": float64(10),
		"audience":    "中学生",
	})
	if err != nil || len(missing) != 0 {
		t.Fatalf("missing = %v err = %v", missing, err)
	}
	if !r.Complete() {
		t.Fatal("should be complete")
	}
	if r.TotalPages != "10" {
		t.Fatalf("numeric total_pages not stringified: %q", r.TotalPages)
	}

	snap := r.Snapshot()
	for _, key := range []string{`"topic":"西湖"`, `"style":"简洁"`, `"total_pages":"10"`, `"audience":"中学生"`} {
		if !strings.Contains(snap, key) {
			t.Fatalf("snapshot missing %s: %s", key, snap)
		}
	}
}

func newTestVoiceState() (*VoiceState, *Queue, *Queue) {
	voiceInbox := NewQueue()
	pptOutbox := NewQueue()
	v := NewVoiceState(&Requirements{}, voiceInbox, pptOutbox, nil)
	return v, voiceInbox, pptOutbox
}

func TestVoicePhase1Flow(t *testing.T) {
	v, voiceInbox, _ := newTestVoiceState()
	ctx := context.Background()

	if _, err := v.SendToPPTAgent(ctx, nil); err == nil || !strings.Contains(err.Error(), "confirmed") {
		t.Fatalf("send before confirm must fail, got %v", err)
	}
	if _, err := v.RequireConfirm(ctx, nil); err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("confirm before complete must fail, got %v", err)
	}

	out, err := v.UpdateRequirements(ctx, map[string]any{"topic": "西湖"})
	if err != nil || !strings.HasPrefix(out, "missing:") {
		t.Fatalf("update = %q, %v", out, err)
	}
	if _, err := v.UpdateRequirements(ctx, map[string]any{
		"style": "简洁", "total_pages": "10", "audience": "中学生",
	}); err != nil {
		t.Fatal(err)
	}

	out, err = v.RequireConfirm(ctx, nil)
	if err != nil || out != "data is sent to the frontend successfully" {
		t.Fatalf("confirm = %q, %v", out, err)
	}

	out, err = v.SendToPPTAgent(ctx, nil)
	if err != nil || out != "sent" {
		t.Fatalf("send = %q, %v", out, err)
	}
	if v.Phase() != Phase2 {
		t.Fatal("phase must be 2")
	}
	got := voiceInbox.Drain()
	if len(got) != 1 || !strings.Contains(got[0], `"topic":"西湖"`) {
		t.Fatalf("inbox = %v", got)
	}

	// phase-1 tools permanently invalid
	if _, err := v.UpdateRequirements(ctx, map[string]any{"topic": "x"}); err == nil {
		t.Fatal("update_requirements must fail in phase 2")
	}
	if _, err := v.RequireConfirm(ctx, nil); err == nil {
		t.Fatal("require_confirm must fail in phase 2")
	}
	if _, err := v.SendToPPTAgent(ctx, nil); err == nil {
		t.Fatal("phase-1 send (no content) must fail in phase 2")
	}
}

func TestVoiceTransitionCallback(t *testing.T) {
	called := 0
	voiceInbox := NewQueue()
	v := NewVoiceState(&Requirements{
		Topic: "t", Style: "s", TotalPages: "5", Audience: "a",
	}, voiceInbox, NewQueue(), func() { called++ })
	v.confirmed.Store(true)

	if _, err := v.SendToPPTAgent(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if called != 1 {
		t.Fatalf("onTransition called %d times", called)
	}
}

func TestVoicePhase2ForwardAndPull(t *testing.T) {
	v, voiceInbox, pptOutbox := newTestVoiceState()
	ctx := context.Background()
	v.phase.Store(int32(Phase2))

	out, err := v.GetMessagesFromPPTAgent(ctx, nil)
	if err != nil || out != "queue is empty" {
		t.Fatalf("empty pull = %q, %v", out, err)
	}

	pptOutbox.Push("第一页好了")
	pptOutbox.Push("pdf 完成")
	out, err = v.GetMessagesFromPPTAgent(ctx, nil)
	if err != nil || !strings.Contains(out, "pdf 完成") {
		t.Fatalf("pull = %q, %v", out, err)
	}
	out, _ = v.GetMessagesFromPPTAgent(ctx, nil)
	if out != "queue is empty" {
		t.Fatalf("second pull = %q", out)
	}

	out, err = v.SendToPPTAgent(ctx, map[string]any{"content": "用户说颜色换蓝色"})
	if err != nil || out != "sent" {
		t.Fatalf("forward = %q, %v", out, err)
	}
	if got := voiceInbox.Drain(); len(got) != 1 || got[0] != "用户说颜色换蓝色" {
		t.Fatalf("forward inbox = %v", got)
	}
	if _, err := v.SendToPPTAgent(ctx, map[string]any{"content": "  "}); err == nil {
		t.Fatal("blank content must fail")
	}
}

func TestVoiceToolSetsPerPhase(t *testing.T) {
	v, _, _ := newTestVoiceState()
	p1 := v.ToolsFor(Phase1)
	if len(p1) != 3 || p1[0].Name != "update_requirements" || p1[1].Name != "require_confirm" || p1[2].Name != "send_to_ppt_agent" {
		names := []string{}
		for _, tl := range p1 {
			names = append(names, tl.Name)
		}
		t.Fatalf("phase1 tools = %v", names)
	}
	p2 := v.ToolsFor(Phase2)
	if len(p2) != 2 || p2[0].Name != "get_messages_from_ppt_agent" || p2[1].Name != "send_to_ppt_agent" {
		t.Fatalf("phase2 tools = %+v", p2)
	}
}
