package tests

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"educationagent/internal/agents"
	"educationagent/internal/config"
	"educationagent/internal/engineserver"
)

// startAppOpts assembles an app with arbitrary options and starts the
// consumer against its httptest WS server.
func startAppOpts(t *testing.T, cfg config.Config, opts ...engineserver.AppOption) *engineserver.App {
	t.Helper()
	all := append([]engineserver.AppOption{engineserver.WithOutDir(t.TempDir())}, opts...)
	app := engineserver.NewApp(cfg, all...)
	srv := httptest.NewServer(app.Handler)
	t.Cleanup(srv.Close)
	app.Consumer.SetEndpoint(wsURLOf(srv))
	go func() {
		if err := app.Consumer.Run(context.Background()); err != nil {
			t.Logf("consumer stopped: %v", err)
		}
	}()
	return app
}

// waitTurnDone blocks until a turn that started after the call has fully
// completed (history grew and the producer reaped). History length is the
// turn counter: compose appends at turn start, merge appends at turn end.
func waitTurnDone(t *testing.T, app *engineserver.App, timeout time.Duration) {
	t.Helper()
	h0 := len(app.Manager.History())
	waitCond(t, "turn completed", timeout, func() bool {
		return len(app.Manager.History()) > h0 &&
			app.Engine.LLMState() == 0 &&
			doneClosed(app.Manager.PrevDone())
	})
}

// TestVoicePhase1FlowE2E drives the voice agent through phase 1 to the
// phase transition, answering with deterministic lines.
func TestVoicePhase1FlowE2E(t *testing.T) {
	cfg := loadE2E(t)
	app := startAppOpts(t, cfg) // production seed: phase-1 system prompt

	app.Manager.OnUserText("我想做一个关于西湖的课件")

	const maxRounds = 8
	for round := 0; round < maxRounds && app.Voice.Phase() == agents.Phase1; round++ {
		waitTurnDone(t, app, 90*time.Second)
		if app.Voice.Phase() == agents.Phase2 {
			break
		}
		missing := app.Voice.Missing()
		t.Logf("round %d: missing=%v", round, missing)
		if len(missing) > 0 {
			app.Manager.OnUserText(answerFor(missing))
		} else {
			app.Manager.OnUserText("确认，需求没问题，发送给 PPT Agent 吧")
		}
	}

	waitCond(t, "phase 2", 90*time.Second, func() bool { return app.Voice.Phase() == agents.Phase2 })
	t.Logf("phase 2 reached; inbox len=%d", app.VoiceInbox.Len())

	if got := app.VoiceInbox.Len(); got < 1 {
		t.Fatal("voice inbox empty after transition")
	}
	items := app.VoiceInbox.Drain()
	if !strings.Contains(items[0], `"topic"`) || !strings.Contains(items[0], `"audience"`) {
		t.Fatalf("snapshot = %q", items[0])
	}

	// One phase-2 turn must build the phase-2 toolset: phase-1 tools gone.
	app.Manager.OnUserText("好的，等 PPT 生成吧")
	waitTurnDone(t, app, 90*time.Second)
	names := app.VoiceToolNames()
	t.Logf("phase-2 tools: %v", names)
	for _, n := range names {
		if n == "update_requirements" || n == "require_confirm" {
			t.Fatalf("phase-1 tool %s still registered in phase 2", n)
		}
	}
	if len(names) == 0 {
		t.Fatal("toolset not recorded")
	}
}

func answerFor(missing []string) string {
	parts := []string{}
	for _, f := range missing {
		switch f {
		case "topic":
			parts = append(parts, "主题是杭州西湖")
		case "style":
			parts = append(parts, "风格简洁优雅")
		case "total_pages":
			parts = append(parts, "总共10页")
		case "audience":
			parts = append(parts, "面向中学生")
		}
	}
	return strings.Join(parts, "，")
}

// TestPPTAgentE2E feeds a complete requirements snapshot straight into the
// voice inbox and waits for the ppt agent to converge on slides.md.
// E2E_FULL=1 additionally runs the real slidev export and asserts ppt.pdf.
func TestPPTAgentE2E(t *testing.T) {
	cfg := loadE2E(t)

	workdir := t.TempDir()
	runner := agents.StubCommandRunner()
	if os.Getenv("E2E_FULL") == "1" {
		abs, err := filepath.Abs("../workspace/ppt")
		if err != nil {
			t.Fatalf("resolve workdir: %v", err)
		}
		workdir = abs
		runner = agents.RealCommandRunner()
	}

	app := startAppOpts(t, cfg,
		engineserver.WithPPTWorkDir(workdir),
		engineserver.WithCommandRunner(runner),
	)
	app.StartPPTAgent(context.Background())

	reqs := &agents.Requirements{
		Topic:      "杭州西湖",
		Style:      "简洁优雅",
		TotalPages: "5",
		Audience:   "中学生",
	}
	app.VoiceInbox.Push(reqs.Snapshot())
	t.Logf("pushed requirements, workdir=%s", workdir)

	slides := filepath.Join(workdir, "slides.md")
	waitCond(t, "slides.md written", 6*time.Minute, func() bool {
		data, err := os.ReadFile(slides)
		if err != nil {
			return false
		}
		content := string(data)
		return strings.HasPrefix(content, "---") && strings.Count(content, "\n---") >= 2
	})

	data, _ := os.ReadFile(slides)
	lines := strings.Split(string(data), "\n")
	if len(lines) > 20 {
		lines = lines[:20]
	}
	t.Logf("slides.md head:\n%s", strings.Join(lines, "\n"))

	if os.Getenv("E2E_FULL") != "1" {
		t.Skip("set E2E_FULL=1 to run the real slidev export")
	}
	waitCond(t, "ppt.pdf exported", 6*time.Minute, func() bool {
		info, err := os.Stat(filepath.Join(workdir, "ppt.pdf"))
		return err == nil && info.Size() > 0
	})
}
