package main

import "context"

type TTSProvider interface {
	Synthesize(ctx context.Context, text string, bufSize int) (<-chan []byte, error)
}
