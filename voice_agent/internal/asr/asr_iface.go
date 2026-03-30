package asr

import "context"

type ASRResult struct {
	Text    string `json:"text"`
	IsFinal bool   `json:"is_final"`
	Mode    string `json:"mode"`
}

type ASRProvider interface {
	RecognizeStream(ctx context.Context, audioCh <-chan []byte, resultBufSize int) (<-chan ASRResult, error)
}
