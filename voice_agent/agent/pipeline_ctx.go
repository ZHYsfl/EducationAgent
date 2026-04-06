package agent

import (
	"context"
	"log"
	"strings"
)

func (p *Pipeline) ttsWorker(ctx context.Context, sentenceCh <-chan string) {
	if p.ttsClient == nil {
		// Drain channel and return (for tests without TTS)
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

			log.Printf("TTS: %s", truncate(sentence, 60))

			audioCh, err := p.ttsClient.Synthesize(ctx, sentence, p.adaptive.Get("tts_chunk_ch"))
			if err != nil {
				log.Printf("tts synthesize: %v", err)
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
