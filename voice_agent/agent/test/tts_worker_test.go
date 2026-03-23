package agent

import (
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
	s := newTestSession(&mockServices{})
	p := newTestPipeline(s, s.clients)
	p.ttsClient = &errTTSProvider{}

	sentenceCh := make(chan string, 1)
	sentenceCh <- "这是一句会触发错误的文本"
	close(sentenceCh)

	p.ttsWorker(context.Background(), sentenceCh)
	// no panic is the primary assertion
}

func TestTTSWorker_SendsBinaryAudioChunks(t *testing.T) {
	s := newTestSession(&mockServices{})
	p := newTestPipeline(s, s.clients)
	p.ttsClient = &chunkTTSProvider{}

	sentenceCh := make(chan string, 1)
	sentenceCh <- "发送音频"
	close(sentenceCh)

	p.ttsWorker(context.Background(), sentenceCh)

	binaryCount := 0
	for {
		select {
		case item := <-s.writeCh:
			if item.msgType == 2 { // websocket.BinaryMessage
				binaryCount++
			}
		default:
			if binaryCount != 2 {
				t.Fatalf("expected 2 binary chunks, got %d", binaryCount)
			}
			return
		}
	}
}

func TestTTSWorker_ContextCanceledDuringChunkLoop(t *testing.T) {
	s := newTestSession(&mockServices{})
	p := newTestPipeline(s, s.clients)
	p.ttsClient = &chunkTTSProvider{}

	ctx, cancel := context.WithCancel(context.Background())
	sentenceCh := make(chan string, 1)
	sentenceCh <- "取消场景"
	close(sentenceCh)

	done := make(chan struct{})
	go func() {
		p.ttsWorker(ctx, sentenceCh)
		close(done)
	}()
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("ttsWorker did not exit after context cancellation")
	}
}
