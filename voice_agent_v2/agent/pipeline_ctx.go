package agent

import (
	"context"
	"log"
	"strings"
)

// ttsWorker reads sentences from sentenceCh, synthesizes audio, and streams to client.
func (p *Pipeline) ttsWorker(ctx context.Context, sentenceCh <-chan string) {
	if p.ttsClient == nil {
		for range sentenceCh {
		}
		return
	}
	for {
		select {
		case sentence, ok := <-sentenceCh:
			if !ok {
				return
			}
			sentence = strings.TrimSpace(sentence)
			if sentence == "" {
				continue
			}
			log.Printf("[tts] %s", truncate(sentence, 60))
			audioCh, err := p.ttsClient.Synthesize(ctx, sentence, p.adaptive.Get("tts_chunk_ch"))
			if err != nil {
				log.Printf("[tts] synthesize error: %v", err)
				continue
			}
			for chunk := range audioCh {
				if ctx.Err() != nil {
					return
				}
				p.session.SendAudio(chunk)
			}
		case <-ctx.Done():
			return
		}
	}
}
