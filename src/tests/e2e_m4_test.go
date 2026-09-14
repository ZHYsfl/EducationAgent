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
		// 五十句 ≈ 5-6s 的流式轮次：第一句 TTS（~3s）落账时推理仍在进行，
		// "state==TTS 且已有落账"的黄金打断窗才真实存在。
		openai.SystemMessage("你是语音助手。回答必须分成至少五十句口语化短句，每句以中文句号结尾，只输出这些句子本身，不要分点不要列表。"),
	}
	app := startApp(t, cfg, history, nil)
	audio := wavPCM(t, "testdata/asr_test_16k.wav")

	// Pre-warm the local TTS: the first synthesis after a service start is
	// slow (model warmup), and if it overlaps the LLM round the interrupt
	// window (state==TTS with a spoken sentence) never opens.
	if err := app.Engine.Queue().Push(context.Background(), []voiceengine.Token{
		{Content: "预热句子。", State: voiceengine.StateTTSTokens, Gen: app.Engine.Generation()},
		{Content: "", State: voiceengine.StateIdle, Gen: app.Engine.Generation()},
	}); err != nil {
		t.Fatalf("warm push: %v", err)
	}
	waitCond(t, "tts warmup", 90*time.Second, func() bool { return len(app.Consumer.Records()) >= 1 })
	baseRecords := len(app.Consumer.Records()) // warm-up only; turn sentences count from here

	// 两种合法的打断相位：
	//  - 黄金窗：推理中且已有新句落账 → 断言 </interrupted> 前缀（firstState=TTS）；
	//  - 播放相位：轮已结束、账本有货（DeepSeek 流式比 TTS 合成快时常态）→
	//    按分支规则断言无前缀。前缀正向分支由单测确定性覆盖。
	// 黄金窗先试两轮（刀清积压后新回合重试），不中则落到播放相位，总能落闸。
	golden := false
	started := time.Now()
	for attempt := 0; attempt < 2 && !golden; attempt++ {
		if attempt > 0 {
			waitCond(t, "previous turn reaped", 60*time.Second, func() bool {
				return app.Engine.LLMState() == 0 && doneClosed(app.Manager.PrevDone())
			})
			baseRecords = len(app.Consumer.Records())
			app.Manager.OnVadStart()
			app.Manager.OnVadEnd(audio)
			t.Logf("attempt %d: new turn fired, baseRecords=%d", attempt, baseRecords)
		}

		deadline := time.Now().Add(100 * time.Second)
		lastRecords := -1
		lastState := int32(-1)
		for {
			state := app.Engine.LLMState()
			records := len(app.Consumer.Records())
			if state != lastState {
				t.Logf("[t=%.1fs] state %d -> %d, records=%d", time.Since(started).Seconds(), lastState, state, records)
				lastState = state
			}
			if records != lastRecords {
				t.Logf("[t=%.1fs] record #%d at state=%d", time.Since(started).Seconds(), records, state)
				lastRecords = records
			}
			if state == voiceengine.LLMStateTTS && records > baseRecords {
				golden = true
				break
			}
			if state == 0 && records > baseRecords && doneClosed(app.Manager.PrevDone()) {
				// 播放相位：轮已咽气、账本有货，也是合法打断点。
				break
			}
			if time.Now().After(deadline) {
				t.Logf("attempt %d: window missed (llmState=%d records=%d)", attempt, state, records)
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	preRecords := len(app.Consumer.Records())
	oldDone := app.Manager.PrevDone()

	start := time.Now()
	resp := app.Manager.OnVadStart()
	if resp.SaidWords == "" {
		t.Fatal("said_words empty after playback began")
	}
	wantState := "idle"
	if golden {
		wantState = "inferring_tts_tokens"
	}
	if resp.LLMState != wantState {
		t.Fatalf("llm_state = %q, want %q (golden=%v)", resp.LLMState, wantState, golden)
	}
	t.Logf("vad_start: said=%q left=%q state=%s golden=%v", resp.SaidWords, resp.RawTheWordsLeftUnsaid, resp.LLMState, golden)

	app.Manager.OnVadEnd(audio)

	select {
	case <-oldDone:
	case <-time.After(10 * time.Second):
		t.Fatal("previous producer not reaped within 10s of the knife")
	}

	waitCond(t, "turn 2 fired", 90*time.Second, func() bool {
		return len(app.Consumer.Records()) > preRecords || len(app.Manager.History()) > 0 && len(userTexts(app.Manager.History())) >= 2
	})
	if golden {
		waitCond(t, "interrupted segment in history", 30*time.Second, func() bool {
			for _, u := range userTexts(app.Manager.History()) {
				if strings.HasPrefix(u, "</interrupted>") {
					return true
				}
			}
			return false
		})
	} else {
		// 播放相位打断：ep.firstState=idle，按规则不得有前缀。
		h := app.Manager.History()
		for _, u := range userTexts(h) {
			if strings.HasPrefix(u, "</interrupted>") {
				t.Fatalf("playback-phase interrupt must not carry </interrupted>: %q", u)
			}
		}
	}
	t.Logf("turn 2 on fire after %v; records=%d golden=%v", time.Since(start), len(app.Consumer.Records()), golden)
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
