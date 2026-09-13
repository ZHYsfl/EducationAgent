package tests

import (
	"context"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openai/openai-go/v3"

	"agent_runtime"
	"educationagent/internal/config"
	"educationagent/internal/engineserver"
	"educationagent/internal/voiceengine"
)

func loadE2E(t *testing.T) config.Config {
	t.Helper()
	if os.Getenv("E2E") != "1" {
		t.Skip("set E2E=1 to run the end-to-end tests")
	}
	if err := config.LoadFile("../.env"); err != nil {
		t.Fatalf("load ../.env: %v", err)
	}
	cfg := config.Load()
	if cfg.DeepSeekAPIKey == "" {
		t.Skip("DEEPSEEK_API_KEY missing")
	}
	return cfg
}

// wavPCM strips the 44-byte canonical header and returns raw pcm16le.
func wavPCM(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read wav: %v", err)
	}
	if len(data) < 44 || string(data[:4]) != "RIFF" {
		t.Fatalf("not a canonical wav: %q", data[:12])
	}
	return data[44:]
}

func startApp(t *testing.T, cfg config.Config, history []openai.ChatCompletionMessageParamUnion, tools []*agent_runtime.Tool) *engineserver.App {
	t.Helper()
	app := engineserver.NewApp(cfg,
		engineserver.WithOutDir(t.TempDir()),
		engineserver.WithHistory(history),
		engineserver.WithTools(tools),
	)
	// The WS server must listen before the consumer can dial it.
	srv := startWSServer(t, app)
	app.Consumer.SetEndpoint(wsURLOf(srv))
	go func() {
		if err := app.Consumer.Run(context.Background()); err != nil {
			t.Logf("consumer stopped: %v", err)
		}
	}()
	return app
}

func waitCond(t *testing.T, what string, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func userTexts(msgs []openai.ChatCompletionMessageParamUnion) []string {
	var out []string
	for _, m := range msgs {
		if m.OfUser != nil && m.OfUser.Content.OfString.Valid() {
			out = append(out, m.OfUser.Content.OfString.Value)
		}
	}
	return out
}

// TestBargeInTTSPhase: turn 1 streams a long answer; while the LLM is still
// inferring (state=inferring_tts_tokens) and at least one sentence has been
// spoken, vad_start+vad_end cut it. The next turn must fire with the
// </interrupted> segment, the playback snapshot non-empty and the old
// producer reaped.
func TestBargeInTTSPhase(t *testing.T) {
	cfg := loadE2E(t)
	history := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage("你是语音助手。回答必须分成至少二十句口语化短句，每句以中文句号结尾，只输出这些句子本身，不要分点不要列表。"),
	}
	app := startApp(t, cfg, history, nil)
	audio := wavPCM(t, "testdata/asr_test_16k.wav")

	app.Manager.OnVadStart()
	app.Manager.OnVadEnd(audio)

	// Interrupt only while the LLM is still streaming AND a full sentence has
	// been spoken (guarantees said_words non-empty and firstState=TTS). ASR of
	// the 10s fixture plus TTS of the first sentence can take a while.
	waitCond(t, "streaming with one spoken sentence", 150*time.Second, func() bool {
		return app.Engine.LLMState() == voiceengine.LLMStateTTS && len(app.Consumer.Records()) >= 1
	})
	preRecords := len(app.Consumer.Records())
	oldDone := app.Manager.PrevDone()

	start := time.Now()
	resp := app.Manager.OnVadStart()
	if resp.SaidWords == "" {
		t.Fatal("said_words empty after playback began")
	}
	if resp.LLMState != "inferring_tts_tokens" {
		t.Fatalf("llm_state = %q, want inferring_tts_tokens", resp.LLMState)
	}
	t.Logf("vad_start: said=%q left=%q state=%s", resp.SaidWords, resp.RawTheWordsLeftUnsaid, resp.LLMState)

	app.Manager.OnVadEnd(audio)

	select {
	case <-oldDone:
	case <-time.After(10 * time.Second):
		t.Fatal("previous producer not reaped within 10s of the knife")
	}

	waitCond(t, "turn 2 fired with interrupted segment", 90*time.Second, func() bool {
		for _, u := range userTexts(app.Manager.History()) {
			if strings.HasPrefix(u, "</interrupted>") {
				return len(app.Consumer.Records()) > preRecords
			}
		}
		return false
	})
	t.Logf("turn 2 on fire after %v; records=%d", time.Since(start), len(app.Consumer.Records()))
}

// TestBargeInToolPhaseImmune: every round must call get_time (3s sleep).
// Interrupt during the tool phase: the tool still completes, ToolInfo is
// recorded, the old producer dies only after its final round, and the new
// turn's history carries the tool result WITHOUT an </interrupted> prefix.
func TestBargeInToolPhaseImmune(t *testing.T) {
	cfg := loadE2E(t)

	var toolCtxErr atomic.Value
	toolRan := make(chan struct{}, 1)
	tools := []*agent_runtime.Tool{
		{
			Name:        "get_time",
			Description: "获取当前时间",
			Func: func(ctx context.Context, args map[string]any) (string, error) {
				if err := ctx.Err(); err != nil {
					toolCtxErr.Store(err)
				}
				time.Sleep(3 * time.Second)
				toolRan <- struct{}{}
				return "现在是2026年9月14日下午3点04分", nil
			},
			Parameters: map[string]any{"type": "object"},
		},
	}
	history := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage("你是语音助手。铁律：无论用户说什么，每一轮你都必须先调用 get_time 工具，拿到结果后用一句口语化中文回复用户，这句以中文句号结尾。"),
	}
	app := startApp(t, cfg, history, tools)
	audio := wavPCM(t, "testdata/asr_test_16k.wav")

	app.Manager.OnVadStart()
	app.Manager.OnVadEnd(audio)

	waitCond(t, "tool phase latched", 60*time.Second, func() bool {
		return app.Engine.LLMState() == voiceengine.LLMStateTool
	})
	oldDone := app.Manager.PrevDone()

	start := time.Now()
	resp := app.Manager.OnVadStart()
	t.Logf("vad_start: said=%q state=%s", resp.SaidWords, resp.LLMState)
	app.Manager.OnVadEnd(audio)

	// Immunity: the cancelled-ctx knife must not kill the tool phase.
	select {
	case <-oldDone:
		t.Fatal("producer died during tool phase despite immunity")
	case <-time.After(1500 * time.Millisecond):
	}

	select {
	case <-toolRan:
	case <-time.After(15 * time.Second):
		t.Fatal("get_time never executed")
	}
	if err, _ := toolCtxErr.Load().(error); err != nil {
		t.Fatalf("tool ran with cancelled ctx: %v", err)
	}
	waitCond(t, "tool record booked", 15*time.Second, func() bool {
		for _, r := range app.Engine.ToolInfo().Get() {
			if r.Name == "get_time" {
				return true
			}
		}
		return false
	})

	// The old producer finishes its final round, then the next turn fires.
	waitCond(t, "turn 2 fired with tool result in history", 90*time.Second, func() bool {
		h := app.Manager.History()
		hasToolResult := false
		for _, m := range h {
			if m.OfTool != nil && strings.Contains(m.OfTool.Content.OfString.Value, "2026年9月14日") {
				hasToolResult = true
			}
		}
		if !hasToolResult {
			return false
		}
		for _, u := range userTexts(h) {
			if strings.Contains(u, "<queue_status>empty</queue_status>") && !strings.HasPrefix(u, "</interrupted>") {
				return true
			}
		}
		return false
	})
	t.Logf("tool-phase interrupt settled in %v", time.Since(start))
}
