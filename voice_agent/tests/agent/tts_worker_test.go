package agent_test

import (
	agent "voiceagent/agent"
	"context"
	"errors"
	"testing"
	"time"
)

type errTTSProvider struct{}

func (e *errTTSProvider) Synthesize(ctx context.Context, text string, bufSize int) (<-chan []byte, error) {
	return nil, errors.New("tts synth failed")
}

type chunkTTSProvider struct{}

func (c *chunkTTSProvider) Synthesize(ctx context.Context, text string, bufSize int) (<-chan []byte, error) {
	ch := make(chan []byte, 2)
	ch <- []byte{0x01, 0x02}
	ch <- []byte{0x03}
	close(ch)
	return ch, nil
}

func TestTTSWorker_SynthesizeErrorBranch(t *testing.T) {
	s := agent.NewTestSession(&agent.MockServices{})
	p := agent.NewTestPipeline(s, s.GetClients())
	p.SetTTSClient(&errTTSProvider{})

	sentenceCh := make(chan string, 1)
	sentenceCh <- "这是一句会触发错误的文本"
	close(sentenceCh)

	p.TTSWorker(context.Background(), sentenceCh)
	// no panic is the primary assertion
}

func TestTTSWorker_SendsBinaryAudioChunks(t *testing.T) {
	s := agent.NewTestSession(&agent.MockServices{})
	p := agent.NewTestPipeline(s, s.GetClients())
	p.SetTTSClient(&chunkTTSProvider{})

	sentenceCh := make(chan string, 1)
	sentenceCh <- "发送音频"
	close(sentenceCh)

	p.TTSWorker(context.Background(), sentenceCh)

	binaryCount := 0
	for {
		mt, _, ok := agent.DrainNextWriteItem(s)
		if !ok {
			break
		}
		if mt == 2 { // websocket.BinaryMessage
			binaryCount++
		}
	}
	if binaryCount != 2 {
		t.Fatalf("expected 2 binary chunks, got %d", binaryCount)
	}
}

func TestTTSWorker_ContextCanceledDuringChunkLoop(t *testing.T) {
	s := agent.NewTestSession(&agent.MockServices{})
	p := agent.NewTestPipeline(s, s.GetClients())
	p.SetTTSClient(&chunkTTSProvider{})

	ctx, cancel := context.WithCancel(context.Background())
	sentenceCh := make(chan string, 1)
	sentenceCh <- "取消场景"
	close(sentenceCh)

	done := make(chan struct{})
	go func() {
		p.TTSWorker(ctx, sentenceCh)
		close(done)
	}()
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("ttsWorker did not exit after context cancellation")
	}
}
