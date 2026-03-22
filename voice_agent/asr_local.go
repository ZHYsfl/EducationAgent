package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"

	"github.com/gorilla/websocket"
)

// ASRClient streams audio to Fun-ASR-Nano and returns recognized text.
// One session per user utterance: Start → send audio blocks → End.
type ASRClient struct {
	url  string
	conn *websocket.Conn
	mu   sync.Mutex
}

func NewASRClient(url string) *ASRClient {
	return &ASRClient{url: url}
}

// StartSession opens a WebSocket to Fun-ASR-Nano and sends config.
func (c *ASRClient) StartSession(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	dialer := websocket.Dialer{}
	conn, _, err := dialer.DialContext(ctx, c.url, nil)
	if err != nil {
		return fmt.Errorf("asr connect: %w", err)
	}
	c.conn = conn

	cfg := map[string]any{
		"mode":        "2pass",         // 两遍识别：先流式出中间结果，再整句结束后做更准的最终识别
		"chunk_size":  []int{5, 10, 5}, // 编码器窗口 [历史帧, 当前帧, 未来帧]，单位 10ms，控制流式延迟
		"audio_fs":    16000,           // 采样率 16kHz，须与麦克风采集一致
		"itn":         true,            // 逆文本正则化：123→一百二十三、3.14→三点一四
		"is_speaking": true,            // 实时说话流；说完后需发 is_speaking:false 表示结束
	}
	if err := conn.WriteJSON(cfg); err != nil {
		conn.Close()
		c.conn = nil
		return fmt.Errorf("asr config send: %w", err)
	}
	return nil
}

// SendAudio sends a raw PCM audio block to the ASR server.
func (c *ASRClient) SendAudio(ctx context.Context, audio []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return fmt.Errorf("asr session not started")
	}
	return c.conn.WriteMessage(websocket.BinaryMessage, audio)
}

// ReadResult blocks until the next ASR result arrives.
func (c *ASRClient) ReadResult(ctx context.Context) (ASRResult, error) {
	if c.conn == nil {
		return ASRResult{}, fmt.Errorf("asr session not started")
	}

	// Set a read deadline from context
	done := make(chan struct{})
	var result ASRResult
	var readErr error

	go func() {
		defer close(done)
		_, msg, err := c.conn.ReadMessage()
		if err != nil {
			readErr = err
			return
		}
		readErr = json.Unmarshal(msg, &result)
	}()

	select {
	case <-done:
		return result, readErr
	case <-ctx.Done():
		c.conn.Close()
		return ASRResult{}, ctx.Err()
	}
}

// EndSession signals end of speech and closes the connection.
func (c *ASRClient) EndSession(ctx context.Context) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return
	}

	err := c.conn.WriteJSON(map[string]any{"is_speaking": false})
	if err != nil {
		log.Printf("asr end session write: %v", err)
	}
	c.conn.Close()
	c.conn = nil
}

// RecognizeStream is a high-level helper: opens a session, streams audio
// from audioCh, and returns recognized text chunks via the returned channel.
// Close audioCh to signal end of speech.
func (c *ASRClient) RecognizeStream(ctx context.Context, audioCh <-chan []byte, resultBufSize int) (<-chan ASRResult, error) {
	if err := c.StartSession(ctx); err != nil {
		return nil, err
	}

	resultCh := make(chan ASRResult, resultBufSize)

	// Writer: forward audio to ASR server
	go func() {
		for {
			select {
			case audio, ok := <-audioCh:
				if !ok {
					c.EndSession(ctx)
					return
				}
				if err := c.SendAudio(ctx, audio); err != nil {
					log.Printf("asr send: %v", err)
					return
				}
			case <-ctx.Done():
				c.EndSession(ctx)
				return
			}
		}
	}()

	// Reader: receive ASR results
	go func() {
		defer close(resultCh)
		for {
			result, err := c.ReadResult(ctx)
			if err != nil {
				if ctx.Err() == nil {
					log.Printf("asr read: %v", err)
				}
				return
			}
			if result.Text == "" {
				continue
			}
			select {
			case resultCh <- result:
			case <-ctx.Done():
				return
			}
		}
	}()

	return resultCh, nil
}
