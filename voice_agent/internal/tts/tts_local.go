package tts

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type TTSClient struct {
	baseURL string
	client  *http.Client
}

func NewTTSClient(baseURL string) *TTSClient {
	return &TTSClient{
		baseURL: baseURL,
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

// BaseURL returns the configured HTTP base URL (for tests).
func (c *TTSClient) BaseURL() string { return c.baseURL }

// Synthesize sends text to CosyVoice2 and returns a channel that yields
// WAV audio chunks. The channel is closed when synthesis is complete.
func (c *TTSClient) Synthesize(ctx context.Context, text string, bufSize int) (<-chan []byte, error) {
	ch := make(chan []byte, bufSize)

	form := url.Values{}
	form.Set("tts_text", text)
	form.Set("mode", "sft")
	form.Set("stream", "1")

	req, err := http.NewRequestWithContext(
		ctx, "POST",
		c.baseURL+"/inference_sft",
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		close(ch)
		return ch, fmt.Errorf("tts request build: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	go func() {
		defer close(ch)

		resp, err := c.client.Do(req)
		if err != nil {
			if ctx.Err() == nil {
				log.Printf("tts request: %v", err)
			}
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
			log.Printf("tts status %d: %s", resp.StatusCode, string(body))
			return
		}

		buf := make([]byte, 8192)
		for {
			n, err := resp.Body.Read(buf)
			if n > 0 {
				if ctx.Err() != nil {
					return
				}
				chunk := make([]byte, n)
				copy(chunk, buf[:n])
				select {
				case ch <- chunk:
				case <-ctx.Done():
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	return ch, nil
}

// AddWAVHeader wraps raw PCM data with a standard WAV header.
// Useful if TTS returns raw PCM instead of WAV.
func AddWAVHeader(pcm []byte, sampleRate, bitsPerSample, channels int) []byte {
	dataLen := uint32(len(pcm))
	header := make([]byte, 44)

	copy(header[0:4], "RIFF")
	binary.LittleEndian.PutUint32(header[4:8], 36+dataLen)
	copy(header[8:12], "WAVE")

	copy(header[12:16], "fmt ")
	binary.LittleEndian.PutUint32(header[16:20], 16)
	binary.LittleEndian.PutUint16(header[20:22], 1) // PCM format
	binary.LittleEndian.PutUint16(header[22:24], uint16(channels))
	binary.LittleEndian.PutUint32(header[24:28], uint32(sampleRate))
	byteRate := uint32(sampleRate * channels * bitsPerSample / 8)
	binary.LittleEndian.PutUint32(header[28:32], byteRate)
	blockAlign := uint16(channels * bitsPerSample / 8)
	binary.LittleEndian.PutUint16(header[32:34], blockAlign)
	binary.LittleEndian.PutUint16(header[34:36], uint16(bitsPerSample))

	copy(header[36:40], "data")
	binary.LittleEndian.PutUint32(header[40:44], dataLen)

	return append(header, pcm...)
}
