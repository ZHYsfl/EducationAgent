package engineserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"educationagent/internal/episode"
	"educationagent/internal/player"
	"educationagent/internal/protocol"
	"educationagent/internal/voiceengine"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// subscribeFrameOrder mimics the browser: one connection sends the subscribe
// frame, then receives pushed sentences. Tokens are fed straight into the
// engine queue; the consumer (broadcast sink) assembles and pushes.
func TestSubscribeFrameOrder(t *testing.T) {
	chunk1 := repeatBytes(0x21, 1500)
	chunk2 := repeatBytes(0x22, 2600)
	fake := fakeTTSUpstream(t, http.StatusOK, chunk1, chunk2)
	defer fake.Close()

	engine := voiceengine.NewEngine(context.Background())
	server := NewServer(fake.URL)
	srv := httptest.NewServer(http.HandlerFunc(server.HandleTTSInfer))
	defer srv.Close()

	consumer := NewSentenceConsumerWithSink(engine, NewBroadcastSink(server))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go consumer.Run(ctx)

	conn := dialTTSServer(t, ctx, srv.URL)
	defer conn.Close(websocket.StatusNormalClosure, "")

	sub := protocol.NewEnvelope(protocol.TypeTTSSubscribe, "sub-1", nil)
	if err := wsjson.Write(ctx, conn, sub); err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	// Registration lands when the server has read the subscribe frame; push
	// only after the subscriber is visible, or the sentences get dropped.
	regDeadline := time.Now().Add(5 * time.Second)
	for len(server.subscriberSnapshot()) == 0 && time.Now().Before(regDeadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if len(server.subscriberSnapshot()) == 0 {
		t.Fatal("subscriber not registered")
	}

	batch := []voiceengine.Token{
		{Content: "第一句。", State: voiceengine.StateTTSTokens, Gen: 0},
		{Content: "第二句！", State: voiceengine.StateTTSTokens, Gen: 0},
		{Content: "", State: voiceengine.StateIdle, Gen: 0},
	}
	if err := engine.Queue().Push(ctx, batch); err != nil {
		t.Fatalf("push: %v", err)
	}

	// Two sentences, each: sentence_start -> audio_header -> binaries -> finished.
	type frame struct {
		typ  string
		text string
		bin  int
		code int
	}
	var got []frame
	finishedCount := 0
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && finishedCount < 2 {
		readCtx, cancelRead := context.WithTimeout(ctx, 2*time.Second)
		dt, data, err := conn.Read(readCtx)
		cancelRead()
		if err != nil {
			t.Fatalf("read: %v (got %+v)", err, got)
		}
		if dt == websocket.MessageBinary {
			got = append(got, frame{typ: "binary", bin: len(data)})
			continue
		}
		var env protocol.Envelope
		if err := json.Unmarshal(data, &env); err != nil {
			t.Fatalf("text frame: %v", err)
		}
		switch env.Type {
		case protocol.TypeTTSSentenceStart:
			d, _ := json.Marshal(env.Data)
			var sd protocol.SentenceStartData
			_ = json.Unmarshal(d, &sd)
			got = append(got, frame{typ: "sentence_start", text: sd.Text})
		case protocol.TypeTTSInferResponseAudio:
			got = append(got, frame{typ: "audio_header"})
		case protocol.TypeTTSInferResponseFinished:
			got = append(got, frame{typ: "finished", code: env.Code})
			finishedCount++
		}
	}
	if finishedCount != 2 {
		t.Fatalf("frames = %+v", got)
	}

	wantTexts := []string{"第一句。", "第二句！"}
	idx := 0
	for s := 0; s < 2; s++ {
		if got[idx].typ != "sentence_start" || got[idx].text != wantTexts[s] {
			t.Fatalf("sentence %d start = %+v", s, got[idx])
		}
		idx++
		if got[idx].typ != "audio_header" {
			t.Fatalf("sentence %d header = %+v", s, got[idx])
		}
		idx++
		bins := 0
		for got[idx].typ == "binary" {
			bins++
			idx++
		}
		if bins == 0 {
			t.Fatalf("sentence %d has no binary frames", s)
		}
		if got[idx].typ != "finished" || got[idx].code != 200 {
			t.Fatalf("sentence %d finished = %+v", s, got[idx])
		}
		idx++
	}

	records := consumer.Records()
	if len(records) != 2 || records[0].Bytes != len(chunk1)+len(chunk2) {
		t.Fatalf("records = %+v", records)
	}
}

func TestBroadcastSinkDropsWithoutSubscribers(t *testing.T) {
	fake := fakeTTSUpstream(t, http.StatusOK, repeatBytes(0x33, 500))
	defer fake.Close()

	engine := voiceengine.NewEngine(context.Background())
	server := NewServer(fake.URL)
	consumer := NewSentenceConsumerWithSink(engine, NewBroadcastSink(server))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go consumer.Run(ctx)

	if err := engine.Queue().Push(ctx, []voiceengine.Token{
		{Content: "没人听的句子。", State: voiceengine.StateTTSTokens, Gen: 0},
		{Content: "", State: voiceengine.StateIdle, Gen: 0},
	}); err != nil {
		t.Fatalf("push: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if len(consumer.Records()) == 0 {
			time.Sleep(20 * time.Millisecond)
			continue
		}
		t.Fatal("sentence without subscriber must not be booked")
	}
}

func TestVadStartSaidOverride(t *testing.T) {
	engine := voiceengine.NewEngine(context.Background())
	p := player.New()
	p.OnSentenceStart("播了一半的句子")
	p.OnProgress(len([]rune("播了一半")))
	mgr := episode.NewManager(context.Background(), engine, p, episode.NewRing(episode.DefaultRingCapacity),
		nopASR{}, func() episode.Runner { return nopRunner{} }, nil)

	srv := httptest.NewServer(HandleVadStart(mgr))
	defer srv.Close()

	body := strings.NewReader(`{"type":"vad_engine/vad_start_request","id":"v9","data":{"said_words":"浏览器快照的话"}}`)
	resp, err := http.Post(srv.URL, "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var env protocol.Envelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(env.Data)
	var vd protocol.VadStartResponseData
	if err := json.Unmarshal(data, &vd); err != nil {
		t.Fatal(err)
	}
	if vd.SaidWords != "浏览器快照的话" {
		t.Fatalf("said_words = %q, want override", vd.SaidWords)
	}
	if p.Stopped() {
		t.Fatal("Go player must not be stopped when said_words overrides (browser mode)")
	}
	if got := p.DoneCount(); got != 0 {
		t.Fatalf("player ledger polluted, DoneCount = %d", got)
	}
}

func TestVadStartWithoutOverrideStopsPlayer(t *testing.T) {
	engine := voiceengine.NewEngine(context.Background())
	p := player.New()
	p.OnSentenceStart("播了一半的句子")
	p.OnProgress(len([]rune("播了一半")))
	mgr := episode.NewManager(context.Background(), engine, p, episode.NewRing(episode.DefaultRingCapacity),
		nopASR{}, func() episode.Runner { return nopRunner{} }, nil)

	srv := httptest.NewServer(HandleVadStart(mgr))
	defer srv.Close()

	body := strings.NewReader(`{"type":"vad_engine/vad_start_request","id":"v10"}`)
	resp, err := http.Post(srv.URL, "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var env protocol.Envelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(env.Data)
	var vd protocol.VadStartResponseData
	if err := json.Unmarshal(data, &vd); err != nil {
		t.Fatal(err)
	}
	if vd.SaidWords != "播了一半" {
		t.Fatalf("said_words = %q, want player snapshot", vd.SaidWords)
	}
	if !p.Stopped() {
		t.Fatal("Go player must be stopped in in-process mode")
	}
}

func repeatBytes(b byte, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = b
	}
	return out
}

var _ = fmt.Sprintf
