package main

import (
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
	results []ASRResult
}

func (s *scriptedASRProvider) RecognizeStream(ctx context.Context, audioCh <-chan []byte, resultBufSize int) (<-chan ASRResult, error) {
	ch := make(chan ASRResult, len(s.results))
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

func (e *errorASRProvider) RecognizeStream(ctx context.Context, audioCh <-chan []byte, resultBufSize int) (<-chan ASRResult, error) {
	return nil, errors.New("asr start failed")
}

type closeDrivenASRProvider struct {
	ready chan struct{}
}

func (c *closeDrivenASRProvider) RecognizeStream(ctx context.Context, audioCh <-chan []byte, resultBufSize int) (<-chan ASRResult, error) {
	if c.ready != nil {
		select {
		case <-c.ready:
		default:
			close(c.ready)
		}
	}
	ch := make(chan ASRResult, 1)
	go func() {
		defer close(ch)
		for {
			select {
			case _, ok := <-audioCh:
				if !ok {
					ch <- ASRResult{Text: "vad结束文本", IsFinal: true, Mode: "2pass-offline"}
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

func (n *neverReturnASRProvider) RecognizeStream(ctx context.Context, audioCh <-chan []byte, resultBufSize int) (<-chan ASRResult, error) {
	ch := make(chan ASRResult)
	go func() {
		<-ctx.Done()
		close(ch)
	}()
	return ch, nil
}

func TestStartDraftThinking_AppendsOutput(t *testing.T) {
	llm := newMockLLMServer("interrupt", []string{"草稿", "内容"})
	defer llm.Close()

	s := newTestSession(&mockServices{})
	p := newTestPipelineWithTTS(s, s.clients)
	p.largeLLM = newMockAgent(llm.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	p.startDraftThinking(ctx, "用户部分输入")
	waitUntil(t, time.Second, func() bool {
		return p.getDraftOutput() != ""
	}, "draft output not produced")

	if got := p.getDraftOutput(); !strings.Contains(got, "草稿") {
		t.Fatalf("unexpected draft output: %q", got)
	}
	p.cancelDraft()
}

func TestStartProcessing_StreamsAndReturnsIdle(t *testing.T) {
	llm := newMockLLMServer("interrupt", []string{"你好", "，", "世界", "。"})
	defer llm.Close()

	m := &mockServices{}
	s := newTestSession(m)
	p := newTestPipelineWithTTS(s, m)
	p.largeLLM = newMockAgent(llm.URL)

	p.startProcessing(context.Background(), "请解释一下牛顿第二定律")

	if s.GetState() != StateIdle {
		t.Fatalf("state = %v, want idle", s.GetState())
	}
	if len(p.history.messages) < 2 {
		t.Fatalf("history should contain user+assistant, got %d", len(p.history.messages))
	}
	if p.history.messages[0].Role != "user" {
		t.Fatalf("first history role = %s", p.history.messages[0].Role)
	}
}

func TestStartProcessing_FillerPath(t *testing.T) {
	llm := newMockLLMServer("interrupt", []string{"A", "B", "C", "D"})
	defer llm.Close()

	m := &mockServices{}
	s := newTestSession(m)
	p := newTestPipelineWithTTS(s, m)
	p.largeLLM = newMockAgent(llm.URL)
	p.config.TokenBudget = 1
	p.config.FillerInterval = 1
	p.config.MaxFillers = 1
	p.config.FillerPhrases = []string{"稍等一下"}

	p.startProcessing(context.Background(), "没有句号的长回答")

	msgs := drainWriteCh(s)
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

	m := &mockServices{}
	s := newTestSession(m)
	p := NewPipeline(s, s.config, m)
	s.pipeline = p
	p.smallLLM = newMockAgent(small.URL)
	p.largeLLM = newMockAgent(large.URL)
	p.ttsClient = &mockTTS{}
	p.asrClient = &scriptedASRProvider{
		results: []ASRResult{
			{Text: "你好", IsFinal: false, Mode: "streaming"},
			{Text: "你好世界", IsFinal: true, Mode: "2pass-offline"},
		},
	}

	s.SetState(StateListening)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	p.StartListening(ctx)

	if s.GetState() != StateIdle {
		t.Fatalf("state = %v, want idle", s.GetState())
	}
	if len(p.history.messages) < 2 {
		t.Fatalf("history should have user and assistant messages, got %d", len(p.history.messages))
	}
}

func TestStartListening_ASRStartError_SetsIdle(t *testing.T) {
	s := newTestSession(&mockServices{})
	p := NewPipeline(s, s.config, s.clients)
	s.pipeline = p
	p.asrClient = &errorASRProvider{}
	s.SetState(StateListening)

	p.StartListening(context.Background())

	if s.GetState() != StateIdle {
		t.Fatalf("state = %v, want idle on ASR start error", s.GetState())
	}
}

func TestStartListening_VADEndPath(t *testing.T) {
	small := newMockLLMServer("do not interrupt", []string{"忽略"})
	defer small.Close()
	large := newMockLLMServer("interrupt", []string{"好的", "。"})
	defer large.Close()

	m := &mockServices{}
	s := newTestSession(m)
	p := NewPipeline(s, s.config, m)
	s.pipeline = p
	p.smallLLM = newMockAgent(small.URL)
	p.largeLLM = newMockAgent(large.URL)
	p.ttsClient = &mockTTS{}
	ready := make(chan struct{})
	p.asrClient = &closeDrivenASRProvider{ready: ready}
	s.SetState(StateListening)

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

	if s.GetState() != StateIdle {
		t.Fatalf("state = %v, want idle", s.GetState())
	}
}

func TestStartListening_ContextCancelPath(t *testing.T) {
	s := newTestSession(&mockServices{})
	p := NewPipeline(s, s.config, s.clients)
	s.pipeline = p
	p.asrClient = &neverReturnASRProvider{}
	s.SetState(StateListening)

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

	m := &mockServices{
		GetUserProfileFn: func(ctx context.Context, userID string) (UserProfile, error) {
			return UserProfile{DisplayName: "学生A", Subject: "数学"}, nil
		},
	}
	s := newTestSession(m)
	s.reqMu.Lock()
	s.Requirements = NewTaskRequirements(s.SessionID, s.UserID)
	s.Requirements.Status = "collecting"
	s.reqMu.Unlock()

	p := newTestPipelineWithTTS(s, m)
	p.largeLLM = newMockAgent(llm.URL)
	p.startProcessing(context.Background(), "我要做一套课件")

	if s.GetState() != StateIdle {
		t.Fatalf("state = %v, want idle", s.GetState())
	}
}

