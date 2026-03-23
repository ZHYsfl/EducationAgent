package agent_test

import (
	agent "voiceagent/agent"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"toolcalling"

	"voiceagent/internal/asr"
)

func newMockLLMServer(chatText string, streamTokens []string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		isStream := strings.Contains(string(body), `"stream":true`)

		if isStream {
			w.Header().Set("Content-Type", "text/event-stream")
			flusher, _ := w.(http.Flusher)
			for _, token := range streamTokens {
				chunk := fmt.Sprintf(`data: {"id":"t","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":%q},"finish_reason":null}]}`+"\n\n", token)
				_, _ = w.Write([]byte(chunk))
				if flusher != nil {
					flusher.Flush()
				}
			}
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
			if flusher != nil {
				flusher.Flush()
			}
			return
		}

		w.Header().Set("Content-Type", "application/json")
		resp := fmt.Sprintf(`{"id":"c","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":%q},"finish_reason":"stop"}]}`, chatText)
		_, _ = w.Write([]byte(resp))
	}))
}

func newMockAgent(baseURL string) *toolcalling.Agent {
	return toolcalling.NewAgent(toolcalling.LLMConfig{
		APIKey:  "test-key",
		Model:   "test-model",
		BaseURL: baseURL,
	})
}

type scriptedASRProvider struct {
	results []asr.ASRResult
}

func (s *scriptedASRProvider) RecognizeStream(ctx context.Context, audioCh <-chan []byte, resultBufSize int) (<-chan asr.ASRResult, error) {
	ch := make(chan asr.ASRResult, len(s.results))
	go func() {
		defer close(ch)
		for _, r := range s.results {
			select {
			case ch <- r:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch, nil
}

type errorASRProvider struct{}

func (e *errorASRProvider) RecognizeStream(ctx context.Context, audioCh <-chan []byte, resultBufSize int) (<-chan asr.ASRResult, error) {
	return nil, errors.New("asr start failed")
}

type closeDrivenASRProvider struct {
	ready chan struct{}
}

func (c *closeDrivenASRProvider) RecognizeStream(ctx context.Context, audioCh <-chan []byte, resultBufSize int) (<-chan asr.ASRResult, error) {
	if c.ready != nil {
		select {
		case <-c.ready:
		default:
			close(c.ready)
		}
	}
	ch := make(chan asr.ASRResult, 1)
	go func() {
		defer close(ch)
		for {
			select {
			case _, ok := <-audioCh:
				if !ok {
					ch <- asr.ASRResult{Text: "vad结束文本", IsFinal: true, Mode: "2pass-offline"}
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch, nil
}

type neverReturnASRProvider struct{}

func (n *neverReturnASRProvider) RecognizeStream(ctx context.Context, audioCh <-chan []byte, resultBufSize int) (<-chan asr.ASRResult, error) {
	ch := make(chan asr.ASRResult)
	go func() {
		<-ctx.Done()
		close(ch)
	}()
	return ch, nil
}

func TestStartDraftThinking_AppendsOutput(t *testing.T) {
	llm := newMockLLMServer("interrupt", []string{"草稿", "内容"})
	defer llm.Close()

	s := agent.NewTestSession(&agent.MockServices{})
	p := agent.NewTestPipelineWithTTS(s, s.GetClients())
	p.SetLargeLLM(newMockAgent(llm.URL))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	p.StartDraftThinking(ctx, "用户部分输入")
	waitUntil(t, time.Second, func() bool {
		return p.GetDraftOutput() != ""
	}, "draft output not produced")

	if got := p.GetDraftOutput(); !strings.Contains(got, "草稿") {
		t.Fatalf("unexpected draft output: %q", got)
	}
	p.CancelDraft()
}

func TestStartProcessing_StreamsAndReturnsIdle(t *testing.T) {
	llm := newMockLLMServer("interrupt", []string{"你好", "，", "世界", "。"})
	defer llm.Close()

	m := &agent.MockServices{}
	s := agent.NewTestSession(m)
	p := agent.NewTestPipelineWithTTS(s, m)
	p.SetLargeLLM(newMockAgent(llm.URL))

	p.StartProcessing(context.Background(), "请解释一下牛顿第二定律")

	if s.GetState() != agent.StateIdle {
		t.Fatalf("state = %v, want idle", s.GetState())
	}
	if len(p.GetHistory().Messages()) < 2 {
		t.Fatalf("history should contain user+assistant, got %d", len(p.GetHistory().Messages()))
	}
	if p.GetHistory().Messages()[0].Role != "user" {
		t.Fatalf("first history role = %s", p.GetHistory().Messages()[0].Role)
	}
}

func TestStartProcessing_FillerPath(t *testing.T) {
	llm := newMockLLMServer("interrupt", []string{"A", "B", "C", "D"})
	defer llm.Close()

	m := &agent.MockServices{}
	s := agent.NewTestSession(m)
	p := agent.NewTestPipelineWithTTS(s, m)
	p.SetLargeLLM(newMockAgent(llm.URL))
	p.GetConfig().TokenBudget = 1
	p.GetConfig().FillerInterval = 1
	p.GetConfig().MaxFillers = 1
	p.GetConfig().FillerPhrases = []string{"稍等一下"}

	p.StartProcessing(context.Background(), "没有句号的长回答")

	msgs := agent.DrainWriteCh(s)
	foundSpeaking := false
	for _, msg := range msgs {
		if msg.Type == "status" && msg.State == "speaking" {
			foundSpeaking = true
			break
		}
	}
	if !foundSpeaking {
		t.Fatal("expected speaking status from filler path")
	}
}

func TestStartListening_EndToEndPath(t *testing.T) {
	small := newMockLLMServer("do not interrupt", []string{"忽略"})
	defer small.Close()
	large := newMockLLMServer("interrupt", []string{"收到", "。"})
	defer large.Close()

	m := &agent.MockServices{}
	s := agent.NewTestSession(m)
	p := agent.NewPipeline(s, s.GetConfig(), m)
	s.SetPipeline(p)
	p.SetSmallLLM(newMockAgent(small.URL))
	p.SetLargeLLM(newMockAgent(large.URL))
	p.SetTTSClient(&agent.MockTTS{})
	p.SetASRClient(&scriptedASRProvider{
		results: []asr.ASRResult{
			{Text: "你好", IsFinal: false, Mode: "streaming"},
			{Text: "你好世界", IsFinal: true, Mode: "2pass-offline"},
		},
	})

	s.SetState(agent.StateListening)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	p.StartListening(ctx)

	if s.GetState() != agent.StateIdle {
		t.Fatalf("state = %v, want idle", s.GetState())
	}
	if len(p.GetHistory().Messages()) < 2 {
		t.Fatalf("history should have user and assistant messages, got %d", len(p.GetHistory().Messages()))
	}
}

func TestStartListening_ASRStartError_SetsIdle(t *testing.T) {
	s := agent.NewTestSession(&agent.MockServices{})
	p := agent.NewPipeline(s, s.GetConfig(), s.GetClients())
	s.SetPipeline(p)
	p.SetASRClient(&errorASRProvider{})
	s.SetState(agent.StateListening)

	p.StartListening(context.Background())

	if s.GetState() != agent.StateIdle {
		t.Fatalf("state = %v, want idle on ASR start error", s.GetState())
	}
}

func TestStartListening_VADEndPath(t *testing.T) {
	small := newMockLLMServer("do not interrupt", []string{"忽略"})
	defer small.Close()
	large := newMockLLMServer("interrupt", []string{"好的", "。"})
	defer large.Close()

	m := &agent.MockServices{}
	s := agent.NewTestSession(m)
	p := agent.NewPipeline(s, s.GetConfig(), m)
	s.SetPipeline(p)
	p.SetSmallLLM(newMockAgent(small.URL))
	p.SetLargeLLM(newMockAgent(large.URL))
	p.SetTTSClient(&agent.MockTTS{})
	ready := make(chan struct{})
	p.SetASRClient(&closeDrivenASRProvider{ready: ready})
	s.SetState(agent.StateListening)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		p.StartListening(ctx)
		close(done)
	}()

	select {
	case <-ready:
	case <-time.After(time.Second):
		t.Fatal("listening channels not initialized in time")
	}

	// Send a tiny audio fragment; VAD end will flush remaining bytes.
	p.OnAudioData([]byte{1, 2, 3, 4})
	p.OnVADEnd()

	waitUntil(t, 2*time.Second, func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	}, "StartListening VAD-end path did not finish")

	if s.GetState() != agent.StateIdle {
		t.Fatalf("state = %v, want idle", s.GetState())
	}
}

func TestStartListening_ContextCancelPath(t *testing.T) {
	s := agent.NewTestSession(&agent.MockServices{})
	p := agent.NewPipeline(s, s.GetConfig(), s.GetClients())
	s.SetPipeline(p)
	p.SetASRClient(&neverReturnASRProvider{})
	s.SetState(agent.StateListening)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() {
		p.StartListening(ctx)
		close(done)
	}()

	waitUntil(t, time.Second, func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	}, "StartListening did not exit on ctx cancel")
}

func TestStartProcessing_RequirementsMode(t *testing.T) {
	llm := newMockLLMServer("interrupt", []string{"已记录", "。"})
	defer llm.Close()

	m := &agent.MockServices{
		GetUserProfileFn: func(ctx context.Context, userID string) (agent.UserProfile, error) {
			return agent.UserProfile{DisplayName: "学生A", Subject: "数学"}, nil
		},
	}
	s := agent.NewTestSession(m)
	s.LockReqMu()
	s.SetRequirements(agent.NewTaskRequirements(s.SessionID, s.UserID))
	s.GetRequirements().Status = "collecting"
	s.UnlockReqMu()

	p := agent.NewTestPipelineWithTTS(s, m)
	p.SetLargeLLM(newMockAgent(llm.URL))
	p.StartProcessing(context.Background(), "我要做一套课件")

	if s.GetState() != agent.StateIdle {
		t.Fatalf("state = %v, want idle", s.GetState())
	}
}
