package main

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"
	"toolcalling"
	"unicode"

	"github.com/openai/openai-go/v3"
)

type Pipeline struct {
	session *Session
	config  *Config

	asrClient *ASRClient
	smallLLM  *toolcalling.Agent
	largeLLM  *toolcalling.Agent
	ttsClient *TTSClient

	history *ConversationHistory

	audioBuf *AudioBuffer
	audioCh  chan []byte   // audio data from session → pipeline
	vadEndCh chan struct{} // signal: user stopped speaking

	// For interrupt preservation
	generatedTokens strings.Builder
	tokensMu        sync.Mutex

	// Draft thinking cancel
	draftCancel context.CancelFunc
	draftMu     sync.Mutex
}

func NewPipeline(session *Session, config *Config) *Pipeline {
	return &Pipeline{
		session:   session,
		config:    config,
		asrClient: NewASRClient(config.ASRWSURL),
		smallLLM: toolcalling.NewAgent(toolcalling.LLMConfig{
			APIKey:  config.SmallLLMAPIKey,
			Model:   config.SmallLLMModel,
			BaseURL: config.SmallLLMBaseURL,
		}),
		largeLLM: toolcalling.NewAgent(toolcalling.LLMConfig{
			APIKey:  config.LargeLLMAPIKey,
			Model:   config.LargeLLMModel,
			BaseURL: config.LargeLLMBaseURL,
		}),
		ttsClient: NewTTSClient(config.TTSURL),
		history:   NewConversationHistory(config.SystemPrompt),
		audioBuf:  NewAudioBuffer(),
	}
}

// OnAudioData is called by the session when audio arrives during LISTENING.
func (p *Pipeline) OnAudioData(data []byte) {
	if p.audioCh != nil {
		select {
		case p.audioCh <- data:
		default:
			// Drop frame if channel full (backpressure)
		}
	}
}

// OnVADEnd is called when the browser signals end of speech.
func (p *Pipeline) OnVADEnd() {
	if p.vadEndCh != nil {
		select {
		case p.vadEndCh <- struct{}{}:
		default:
		}
	}
}

// OnInterrupt is called when user starts speaking during PROCESSING/SPEAKING.
// Saves partial LLM output to history before the pipeline context is cancelled.
func (p *Pipeline) OnInterrupt() {
	p.tokensMu.Lock()
	partial := p.generatedTokens.String()
	p.generatedTokens.Reset()
	p.tokensMu.Unlock()

	if partial != "" {
		p.history.AddInterruptedAssistant(partial)
		log.Printf("Interrupt: preserved %d chars of partial response", len(partial))
	}

	p.cancelDraft()
}

// ---------------------------------------------------------------------------
// LISTENING phase
// ---------------------------------------------------------------------------

// StartListening runs the listening pipeline: accumulate audio, run ASR,
// check for interrupt intent, and optionally do draft thinking.
func (p *Pipeline) StartListening(ctx context.Context) {
	p.audioBuf.Reset()
	p.audioCh = make(chan []byte, 200)
	p.vadEndCh = make(chan struct{}, 1)

	p.tokensMu.Lock()
	p.generatedTokens.Reset()
	p.tokensMu.Unlock()

	// Channel for sending 1s audio blocks to ASR
	asrAudioCh := make(chan []byte, 20)

	// Start ASR streaming session
	asrResultCh, err := p.asrClient.RecognizeStream(ctx, asrAudioCh)
	if err != nil {
		log.Printf("ASR start failed: %v", err)
		p.session.SetState(StateIdle)
		return
	}

	var asrTexts []string
	vadEnded := false

	for {
		select {
		case audio := <-p.audioCh:
			p.audioBuf.Write(audio)

			// Extract complete 1s blocks and send to ASR
			for {
				block, ok := p.audioBuf.GetBlock()
				if !ok {
					break
				}
				select {
				case asrAudioCh <- block:
				case <-ctx.Done():
					close(asrAudioCh)
					return
				}
			}

		case result, ok := <-asrResultCh:
			if !ok {
				// ASR channel closed
				if len(asrTexts) > 0 {
					fullText := strings.Join(asrTexts, "")
					p.startProcessing(ctx, fullText)
				} else {
					p.session.SetState(StateIdle)
				}
				return
			}

			asrTexts = append(asrTexts, result.Text)
			fullText := strings.Join(asrTexts, "")

			// Send transcript to browser
			p.session.SendJSON(WSMessage{Type: "transcript", Text: fullText})

			// Small LLM: check if this is a real interrupt
			if isInterrupt(ctx, p.smallLLM, fullText) {
				close(asrAudioCh) // end ASR session
				p.cancelDraft()
				p.startProcessing(ctx, fullText)
				return
			}

			// 边听边想: draft thinking with accumulated text
			p.startDraftThinking(ctx, fullText)

		case <-p.vadEndCh:
			vadEnded = true
			// Send remaining audio to ASR
			if remaining := p.audioBuf.Flush(); len(remaining) > 0 {
				select {
				case asrAudioCh <- remaining:
				default:
				}
			}
			close(asrAudioCh)

			// Wait briefly for final ASR results
			p.drainASRResults(ctx, asrResultCh, &asrTexts, 2*time.Second)

			p.cancelDraft()

			fullText := strings.Join(asrTexts, "")
			if fullText == "" {
				p.session.SetState(StateIdle)
				return
			}
			p.startProcessing(ctx, fullText)
			return

		case <-ctx.Done():
			if !vadEnded {
				close(asrAudioCh)
			}
			return
		}
	}
}

// drainASRResults reads remaining ASR results until timeout or channel close.
func (p *Pipeline) drainASRResults(ctx context.Context, ch <-chan ASRResult, texts *[]string, timeout time.Duration) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case result, ok := <-ch:
			if !ok {
				return
			}
			if result.Text != "" {
				*texts = append(*texts, result.Text)
			}
			if result.IsFinal {
				return
			}
		case <-timer.C:
			return
		case <-ctx.Done():
			return
		}
	}
}

// ---------------------------------------------------------------------------
// 边听边想: Draft thinking
// ---------------------------------------------------------------------------

func (p *Pipeline) startDraftThinking(ctx context.Context, partialText string) {
	p.cancelDraft()

	p.draftMu.Lock()
	draftCtx, cancel := context.WithCancel(ctx)
	p.draftCancel = cancel
	p.draftMu.Unlock()

	// Fire-and-forget: warm up vLLM prefix cache with partial user text.
	// We discard the output — the benefit is that when the real request
	// arrives, vLLM reuses the cached prefix KV.
	go func() {
		messages := p.history.ToOpenAIWithDraft(partialText)
		tokenCh := p.largeLLM.StreamChat(draftCtx, messages)
		for range tokenCh {
			// discard draft tokens
		}
	}()
}

func (p *Pipeline) cancelDraft() {
	p.draftMu.Lock()
	if p.draftCancel != nil {
		p.draftCancel()
		p.draftCancel = nil
	}
	p.draftMu.Unlock()
}

// ---------------------------------------------------------------------------
// PROCESSING → SPEAKING phase
// ---------------------------------------------------------------------------

func (p *Pipeline) startProcessing(ctx context.Context, userText string) {
	p.session.SetState(StateProcessing)
	p.history.AddUser(userText)

	log.Printf("Processing user input: %s", truncate(userText, 100))

	// Send user text to browser for display
	p.session.SendJSON(WSMessage{Type: "transcript", Text: userText})

	// TTS sentence queue — decouples LLM generation from TTS synthesis
	sentenceCh := make(chan string, 20)

	var ttsWg sync.WaitGroup
	ttsWg.Add(1)
	go func() {
		defer ttsWg.Done()
		p.ttsWorker(ctx, sentenceCh)
	}()

	// Stream tokens from Large LLM
	messages := p.history.ToOpenAI()
	tokenCh := p.largeLLM.StreamChat(ctx, messages)

	tokenCount := 0
	var sentenceBuf strings.Builder
	var allTokens strings.Builder
	fillerSent := false
	firstSentenceSent := false

	for token := range tokenCh {
		if ctx.Err() != nil {
			break
		}

		// Track all generated tokens for interrupt preservation
		p.tokensMu.Lock()
		p.generatedTokens.WriteString(token)
		p.tokensMu.Unlock()

		allTokens.WriteString(token)
		tokenCount++

		// Send response text to browser incrementally
		p.session.SendJSON(WSMessage{Type: "response", Text: token})

		// 边想边说: first N tokens are the thinking window
		if tokenCount <= p.config.TokenBudget {
			sentenceBuf.WriteString(token)
			// Even during thinking window, check for early sentences
			if isSentenceEnd(token) && sentenceBuf.Len() > 0 {
				sentence := sentenceBuf.String()
				sentenceBuf.Reset()
				sentenceCh <- sentence
				firstSentenceSent = true
				if p.session.GetState() == StateProcessing {
					p.session.SetState(StateSpeaking)
				}
			}
			continue
		}

		// Past token budget: if no sentence produced yet, inject filler
		if !firstSentenceSent && !fillerSent {
			sentenceCh <- p.config.FillerPhrase1
			fillerSent = true
			if p.session.GetState() == StateProcessing {
				p.session.SetState(StateSpeaking)
			}
		}

		sentenceBuf.WriteString(token)
		if isSentenceEnd(token) {
			sentence := sentenceBuf.String()
			sentenceBuf.Reset()
			sentenceCh <- sentence
			firstSentenceSent = true
		}
	}

	// Flush remaining text
	if sentenceBuf.Len() > 0 {
		sentenceCh <- sentenceBuf.String()
	}
	close(sentenceCh)

	// Wait for all TTS audio to be sent
	ttsWg.Wait()

	// Save to history (if not interrupted)
	if ctx.Err() == nil {
		finalText := allTokens.String()
		if finalText != "" {
			p.history.AddAssistant(finalText)
		}
		p.session.SetState(StateIdle)
	}
}

// ttsWorker reads sentences from the channel, synthesizes audio, and sends
// WAV chunks back to the browser.
func (p *Pipeline) ttsWorker(ctx context.Context, sentenceCh <-chan string) {
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

			audioCh, err := p.ttsClient.Synthesize(ctx, sentence)
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

// ---------------------------------------------------------------------------
// Interrupt detection (uses Small LLM via SDK Agent)
// ---------------------------------------------------------------------------

const interruptDetectionPrompt = `你是语音意图检测模型。给定一段ASR识别文本，判断其是否包含有意义的用户意图。
interrupt — 文本含有实际语义：问题、指令、陈述、确认（好/对/可以）、否定（不要/停/别说了）、哪怕是未说完的半句话（"我想问"、"那个东西"）。
do not interrupt — 文本仅为无语义噪声：语气词（嗯、啊、哦、呃、emm）、咳嗽/笑声的误识别、重复填充音（啊啊啊）、空白或乱码。
只输出 interrupt 或 do not interrupt，不要输出任何其他内容。`

func isInterrupt(ctx context.Context, agent *toolcalling.Agent, text string) bool {
	messages := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(interruptDetectionPrompt),
		openai.UserMessage(text),
	}
	resp, err := agent.ChatText(ctx, messages)
	if err != nil {
		log.Printf("interrupt detection error: %v", err)
		return true
	}
	return strings.Contains(strings.ToLower(resp), "interrupt") &&
		!strings.Contains(strings.ToLower(resp), "do not interrupt")
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

var sentenceEnders = map[rune]bool{
	'。': true, '！': true, '？': true, '；': true,
	'.': true, '!': true, '?': true, ';': true,
	'，': true, ',': true, // also split on commas for faster streaming
	'\n': true,
}

func isSentenceEnd(s string) bool {
	s = strings.TrimRightFunc(s, unicode.IsSpace)
	if s == "" {
		return false
	}
	lastRune := []rune(s)[len([]rune(s))-1]
	return sentenceEnders[lastRune]
}

func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}
