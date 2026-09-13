package episode

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/openai/openai-go/v3"

	"educationagent/internal/player"
	"educationagent/internal/voiceengine"
)

type asrScript struct {
	text  string
	err   error
	delay time.Duration
}

type fakeASR struct {
	mu      sync.Mutex
	scripts []asrScript
	calls   int
}

func (f *fakeASR) Transcribe(ctx context.Context, pcm []byte) (string, error) {
	f.mu.Lock()
	i := f.calls
	f.calls++
	var s asrScript
	if i < len(f.scripts) {
		s = f.scripts[i]
	} else if len(f.scripts) > 0 {
		s = f.scripts[len(f.scripts)-1]
	}
	f.mu.Unlock()
	if s.delay > 0 {
		time.Sleep(s.delay)
	}
	return s.text, s.err
}

type fakeRunner struct {
	mu    sync.Mutex
	calls [][]openai.ChatCompletionMessageParamUnion
	// behave runs before done closes; nil means close immediately.
	behave func(ctx context.Context)
}

func (f *fakeRunner) Run(ctx context.Context, messages []openai.ChatCompletionMessageParamUnion, done chan<- struct{}) {
	f.mu.Lock()
	f.calls = append(f.calls, append([]openai.ChatCompletionMessageParamUnion(nil), messages...))
	f.mu.Unlock()
	if f.behave != nil {
		f.behave(ctx)
	}
	close(done)
}

func (f *fakeRunner) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeRunner) call(i int) []openai.ChatCompletionMessageParamUnion {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]openai.ChatCompletionMessageParamUnion(nil), f.calls[i]...)
}

func waitForCond(t *testing.T, what string, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestTryCompleteBacktracksWhenVadArrivesDuringWait(t *testing.T) {
	engine := voiceengine.NewEngine(context.Background())
	p := player.New()

	runner1 := &fakeRunner{behave: func(ctx context.Context) { time.Sleep(450 * time.Millisecond) }}
	runner2 := &fakeRunner{}
	asr := &fakeASR{scripts: []asrScript{
		{text: "第一句"}, // fast, fires turn 1
		{text: "第二句", delay: 250 * time.Millisecond}, // completes while turn 1 alive
		{text: "第三句", delay: 150 * time.Millisecond}, // lands inside the prevDone wait
	}}
	runners := []Runner{runner1, runner2}
	i := 0
	factory := func() Runner { r := runners[i]; i++; return r }

	m := NewManager(context.Background(), engine, p, NewRing(DefaultRingCapacity), asr, factory, nil)

	// Turn 1: fires immediately (prevDone pre-closed).
	m.OnVadStart()
	m.OnVadEnd([]byte{0x01})
	waitForCond(t, "turn 1 fired", 2*time.Second, func() bool { return runner1.callCount() == 1 })

	// Turn 2 begins while turn 1 is still alive; its ASR completes ->
	// tryComplete parks on <-prevDone.
	engine.SetLLMState(voiceengine.LLMStateTTS)
	p.OnSentenceStart("播了一半的句子")
	p.OnProgress(len([]rune("播了一半")))
	m.OnVadStart()
	m.OnVadEnd([]byte{0x02})
	time.Sleep(50 * time.Millisecond)

	// Third vad_start lands inside the prevDone wait: episode re-opens.
	m.OnVadStart()
	m.OnVadEnd([]byte{0x03})

	// Everything settles: exactly one more turn fires, with both segments.
	waitForCond(t, "turn 2 fired", 5*time.Second, func() bool { return runner2.callCount() == 1 })
	if runner1.callCount() != 1 {
		t.Fatalf("runner1 fired %d times, want 1", runner1.callCount())
	}

	msgs := runner2.call(0)
	var users []string
	for _, msg := range msgs {
		if msg.OfUser != nil && msg.OfUser.Content.OfString.Valid() {
			users = append(users, msg.OfUser.Content.OfString.Value)
		}
	}
	// The input is the full history: turn 1's user message comes first, then
	// this episode's segments in ASR *completion* order (第三句 finished first).
	if len(users) != 3 {
		t.Fatalf("user messages = %v, want 3 (turn 1 input + 2 segments)", users)
	}
	if users[0] != "第一句<queue_status>empty</queue_status>" {
		t.Fatalf("turn 1 history lost: %v", users)
	}
	if users[1] != "</interrupted>第三句" || users[2] != "第二句<queue_status>empty</queue_status>" {
		t.Fatalf("segments lost or misordered: %v", users)
	}
	// firstState was TTS at the first vad_start of the burst and something
	// had been said -> the first segment carries the interrupted prefix on
	// the ASSISTANT side (said) — user segment stays plain.
	var said string
	for _, msg := range msgs {
		if msg.OfAssistant != nil && msg.OfAssistant.Content.OfString.Valid() {
			said = msg.OfAssistant.Content.OfString.Value
		}
	}
	if said != "播了一半" {
		t.Fatalf("said assistant message = %q, want %q", said, "播了一半")
	}
}

func TestComposeWithToolRecords(t *testing.T) {
	engine := voiceengine.NewEngine(context.Background())
	p := player.New()
	engine.ToolInfo().Add(voiceengine.ToolCallRecord{Name: "remember", ArgsJSON: `{"k":"v"}`, Result: "已记住"})

	runner := &fakeRunner{}
	m := NewManager(context.Background(), engine, p, NewRing(DefaultRingCapacity),
		&fakeASR{scripts: []asrScript{{text: "补充一句"}}},
		func() Runner { return runner }, nil)

	engine.SetLLMState(voiceengine.LLMStateTTS)
	p.OnSentenceStart("被打断的半句")
	p.OnProgress(len([]rune("被打断的半句")))
	m.OnVadStart()
	m.OnVadEnd([]byte{0x01})

	waitForCond(t, "turn fired", 2*time.Second, func() bool { return runner.callCount() == 1 })
	msgs := runner.call(0)

	if len(msgs) != 4 {
		t.Fatalf("got %d messages, want 4 (said, assistant tool_calls, tool, user)", len(msgs))
	}
	if msgs[0].OfAssistant == nil || msgs[0].OfAssistant.Content.OfString.Value != "被打断的半句" {
		t.Fatalf("msgs[0] = %+v, want said assistant", msgs[0])
	}
	if msgs[1].OfAssistant == nil || len(msgs[1].OfAssistant.ToolCalls) != 1 {
		t.Fatalf("msgs[1] = %+v, want assistant tool_calls", msgs[1])
	}
	tc := msgs[1].OfAssistant.ToolCalls[0]
	if tc.OfFunction == nil || tc.OfFunction.Function.Name != "remember" || tc.OfFunction.Function.Arguments != `{"k":"v"}` {
		t.Fatalf("tool call = %+v", tc)
	}
	if msgs[2].OfTool == nil || msgs[2].OfTool.Content.OfString.Value != "已记住" {
		t.Fatalf("msgs[2] = %+v, want tool result", msgs[2])
	}
	wantUser := "</interrupted>补充一句<queue_status>empty</queue_status>"
	if msgs[3].OfUser == nil || msgs[3].OfUser.Content.OfString.Value != wantUser {
		t.Fatalf("msgs[3] = %+v, want %q", msgs[3], wantUser)
	}
	if len(engine.ToolInfo().Get()) != 0 {
		t.Fatal("ToolInfo not reset after compose")
	}
}

func TestInterruptedPrefixRules(t *testing.T) {
	cases := []struct {
		name       string
		state      int32
		withSaid   bool
		wantPrefix bool
	}{
		{"tts state with said gets prefix", voiceengine.LLMStateTTS, true, true},
		{"tts state without said stays plain", voiceengine.LLMStateTTS, false, false},
		{"tool state with said stays plain", voiceengine.LLMStateTool, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			engine := voiceengine.NewEngine(context.Background())
			p := player.New()
			runner := &fakeRunner{}
			m := NewManager(context.Background(), engine, p, NewRing(DefaultRingCapacity),
				&fakeASR{scripts: []asrScript{{text: "新输入"}}},
				func() Runner { return runner }, nil)

			engine.SetLLMState(tc.state)
			if tc.withSaid {
				p.OnSentenceStart("说了的话")
				p.OnSentenceEnded() // said joins done; snapshot includes it
			}
			m.OnVadStart()
			m.OnVadEnd([]byte{0x01})

			waitForCond(t, "turn fired", 2*time.Second, func() bool { return runner.callCount() == 1 })
			msgs := runner.call(0)
			var user string
			for _, msg := range msgs {
				if msg.OfUser != nil && msg.OfUser.Content.OfString.Valid() {
					user = msg.OfUser.Content.OfString.Value
				}
			}
			if strings.HasPrefix(user, "</interrupted>") != tc.wantPrefix {
				t.Fatalf("user = %q, wantPrefix=%v", user, tc.wantPrefix)
			}
		})
	}
}

func TestAsrFailureBooksPlaceholder(t *testing.T) {
	engine := voiceengine.NewEngine(context.Background())
	p := player.New()
	runner := &fakeRunner{}
	m := NewManager(context.Background(), engine, p, NewRing(DefaultRingCapacity),
		&fakeASR{scripts: []asrScript{{err: context.DeadlineExceeded}}},
		func() Runner { return runner }, nil)

	m.OnVadStart()
	m.OnVadEnd([]byte{0x01})

	waitForCond(t, "turn fired despite ASR failure", 2*time.Second, func() bool { return runner.callCount() == 1 })
	msgs := runner.call(0)
	var user string
	for _, msg := range msgs {
		if msg.OfUser != nil && msg.OfUser.Content.OfString.Valid() {
			user = msg.OfUser.Content.OfString.Value
		}
	}
	if user != "<queue_status>empty</queue_status>" {
		t.Fatalf("placeholder segment = %q", user)
	}
}

func TestMergeTurnMessagesAppendsToHistory(t *testing.T) {
	engine := voiceengine.NewEngine(context.Background())
	m := NewManager(context.Background(), engine, player.New(), NewRing(DefaultRingCapacity),
		&fakeASR{}, func() Runner { return &fakeRunner{} },
		[]openai.ChatCompletionMessageParamUnion{openai.UserMessage("seed")})

	m.MergeTurnMessages([]openai.ChatCompletionMessageParamUnion{openai.AssistantMessage("reply")})
	h := m.History()
	if len(h) != 2 || h[1].OfAssistant == nil {
		t.Fatalf("history = %+v", h)
	}
	// The seed slice must not be mutated by the manager.
	seed := []openai.ChatCompletionMessageParamUnion{openai.UserMessage("x")}
	m.SetHistory(seed)
	m.MergeTurnMessages([]openai.ChatCompletionMessageParamUnion{openai.AssistantMessage("y")})
	if len(seed) != 1 {
		t.Fatal("caller's seed slice mutated")
	}
}
