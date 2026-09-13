package engineserver

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/openai/openai-go/v3"

	"educationagent/internal/episode"
	"educationagent/internal/player"
	"educationagent/internal/protocol"
	"educationagent/internal/voiceengine"
)

type nopASR struct{}

func (nopASR) Transcribe(ctx context.Context, pcm []byte) (string, error) { return "测试音频", nil }

type nopRunner struct{}

func (nopRunner) Run(ctx context.Context, msgs []openai.ChatCompletionMessageParamUnion, done chan<- struct{}) {
	close(done)
}

type stubManagerDeps struct {
	engine *voiceengine.Engine
	player *player.Player
	mgr    *episode.Manager
}

func newStubManager() *stubManagerDeps {
	engine := voiceengine.NewEngine(context.Background())
	p := player.New()
	mgr := episode.NewManager(context.Background(), engine, p, episode.NewRing(episode.DefaultRingCapacity),
		nopASR{}, func() episode.Runner { return nopRunner{} }, nil)
	return &stubManagerDeps{engine: engine, player: p, mgr: mgr}
}

func TestHandleVadStart(t *testing.T) {
	d := newStubManager()
	srv := httptest.NewServer(HandleVadStart(d.mgr))
	defer srv.Close()

	d.player.OnSentenceStart("已经播完的句子。")
	d.player.OnSentenceEnded()

	body := bytes.NewReader([]byte(`{"type":"vad_engine/vad_start_request","id":"v1"}`))
	resp, err := http.Post(srv.URL, "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var env protocol.Envelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatal(err)
	}
	if env.Type != protocol.TypeVadStartResponse || env.Code != 200 || env.ID != "v1" {
		t.Fatalf("envelope = %+v", env)
	}
	data, _ := json.Marshal(env.Data)
	var vd protocol.VadStartResponseData
	if err := json.Unmarshal(data, &vd); err != nil {
		t.Fatal(err)
	}
	if vd.SaidWords != "已经播完的句子。" {
		t.Fatalf("said_words = %q", vd.SaidWords)
	}
}

func TestHandleVadEnd(t *testing.T) {
	d := newStubManager()
	srv := httptest.NewServer(HandleVadEnd(d.mgr))
	defer srv.Close()

	audio := base64.StdEncoding.EncodeToString([]byte{0x01, 0x02})
	body := bytes.NewReader([]byte(`{"type":"vad_engine/vad_end_request","id":"v2","data":{"audio":"` + audio + `"}}`))
	resp, err := http.Post(srv.URL, "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var env protocol.Envelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatal(err)
	}
	if env.Type != protocol.TypeVadEndResponse || env.Code != 200 || env.ID != "v2" {
		t.Fatalf("envelope = %+v", env)
	}
	data, _ := json.Marshal(env.Data)
	var vd protocol.VadEndResponseData
	if err := json.Unmarshal(data, &vd); err != nil {
		t.Fatal(err)
	}
	if dec, _ := base64.StdEncoding.DecodeString(vd.Audio); !bytes.Equal(dec, []byte{0x01, 0x02}) {
		t.Fatalf("audio echo mismatch")
	}
}

func TestHandleVadEndRejectsBadBase64(t *testing.T) {
	d := newStubManager()
	srv := httptest.NewServer(HandleVadEnd(d.mgr))
	defer srv.Close()

	body := bytes.NewReader([]byte(`{"type":"vad_engine/vad_end_request","id":"v3","data":{"audio":"!!!"}}`))
	resp, err := http.Post(srv.URL, "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}
