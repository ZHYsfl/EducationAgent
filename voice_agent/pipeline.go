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

	history  *ConversationHistory
	adaptive *AdaptiveController

	audioBuf *AudioBuffer
	audioCh  chan []byte   // audio data from session → pipeline
	vadEndCh chan struct{} // signal: user stopped speaking

	// For interrupt preservation
	generatedTokens strings.Builder
	tokensMu        sync.Mutex

	// Draft thinking
	draftCancel   context.CancelFunc
	draftMu       sync.Mutex
	draftOutput   strings.Builder // accumulated thinker output across rounds
	draftOutputMu sync.Mutex
}

func NewPipeline(session *Session, config *Config) *Pipeline {
	sizes := LoadChannelSizes(config.AdaptiveSizesFile, DefaultChannelSizes())
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
		adaptive:  NewAdaptiveController(sizes),
	}
}

// OnAudioData is called by the session when audio arrives during LISTENING.
func (p *Pipeline) OnAudioData(data []byte) {
	if p.audioCh != nil {
		p.adaptive.RecordLen("audio_ch", len(p.audioCh))
		select {
		case p.audioCh <- data:
		default:
			p.adaptive.RecordBlock("audio_ch")
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
	p.audioCh = make(chan []byte, p.adaptive.Get("audio_ch"))
	p.vadEndCh = make(chan struct{}, 1)

	p.tokensMu.Lock()
	p.generatedTokens.Reset()
	p.tokensMu.Unlock()

	p.resetDraftOutput()

	asrAudioCh := make(chan []byte, p.adaptive.Get("asr_audio_ch"))

	asrResultCh, err := p.asrClient.RecognizeStream(ctx, asrAudioCh, p.adaptive.Get("asr_result_ch"))
	if err != nil {
		log.Printf("ASR start failed: %v", err)
		p.session.SetState(StateIdle)
		return
	}

	var partialTexts []string
	var finalText string
	vadEnded := false
	draftStarted := false
	asrSinceDraft := 0
	draftInterval := 1

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
				p.adaptive.RecordLen("asr_audio_ch", len(asrAudioCh))
				select {
				case asrAudioCh <- block:
				case <-ctx.Done():
					close(asrAudioCh)
					return
				}
			}

		case result, ok := <-asrResultCh:
			if !ok {
				fullText := finalText
				if fullText == "" {
					fullText = strings.Join(partialTexts, "")
				}
				if fullText != "" {
					p.startProcessing(ctx, fullText)
				} else {
					p.session.SetState(StateIdle)
				}
				return
			}

			if result.Mode == "2pass-offline" {
				finalText = result.Text
				p.session.SendJSON(WSMessage{Type: "transcript_final", Text: finalText})
			} else {
				partialTexts = append(partialTexts, result.Text)
				partialText := strings.Join(partialTexts, "")
				p.session.SendJSON(WSMessage{Type: "transcript", Text: partialText})
			}

			// Draft thinking uses partial text (low latency)
			currentText := strings.Join(partialTexts, "")
			if currentText == "" {
				break
			}

			if !draftStarted {
				if isInterrupt(ctx, p.smallLLM, currentText) {
					draftStarted = true
					asrSinceDraft = 0
					draftInterval = 2
					p.startDraftThinking(ctx, currentText)
				}
			} else {
				asrSinceDraft++
				if asrSinceDraft >= draftInterval {
					asrSinceDraft = 0
					draftInterval++
					p.startDraftThinking(ctx, currentText)
				}
			}

		case <-p.vadEndCh:
			vadEnded = true
			if remaining := p.audioBuf.Flush(); len(remaining) > 0 {
				select {
				case asrAudioCh <- remaining:
				default:
				}
			}
			close(asrAudioCh)

			// Wait for final ASR results (including 2pass-offline)
			p.drainASRResults(ctx, asrResultCh, &partialTexts, &finalText, 2*time.Second)

			p.cancelDraft()

			fullText := finalText
			if fullText == "" {
				fullText = strings.Join(partialTexts, "")
			}
			if fullText == "" {
				p.session.SetState(StateIdle)
				return
			}
			if finalText != "" {
				p.session.SendJSON(WSMessage{Type: "transcript_final", Text: finalText})
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
// 2pass-offline results update finalText; others append to partialTexts.
func (p *Pipeline) drainASRResults(ctx context.Context, ch <-chan ASRResult, partialTexts *[]string, finalText *string, timeout time.Duration) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	for {
		select {
		case result, ok := <-ch:
			if !ok {
				return
			}
			if result.Text == "" {
				continue
			}
			if result.Mode == "2pass-offline" {
				*finalText = result.Text
			} else {
				*partialTexts = append(*partialTexts, result.Text)
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
	previousThought := p.getDraftOutput()
	p.cancelDraft()
	p.resetDraftOutput()

	p.draftMu.Lock()
	draftCtx, cancel := context.WithCancel(ctx)
	p.draftCancel = cancel
	p.draftMu.Unlock()

	go func() {
		messages := p.history.ToOpenAIWithDraftAndThought(partialText, previousThought)
		tokenCh := p.largeLLM.StreamChat(draftCtx, messages)
		for token := range tokenCh {
			p.appendDraftOutput(token)
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

func (p *Pipeline) getDraftOutput() string {
	p.draftOutputMu.Lock()
	defer p.draftOutputMu.Unlock()
	return p.draftOutput.String()
}

func (p *Pipeline) appendDraftOutput(token string) {
	p.draftOutputMu.Lock()
	p.draftOutput.WriteString(token)
	p.draftOutputMu.Unlock()
}

func (p *Pipeline) resetDraftOutput() {
	p.draftOutputMu.Lock()
	p.draftOutput.Reset()
	p.draftOutputMu.Unlock()
}

// ---------------------------------------------------------------------------
// PROCESSING → SPEAKING phase
// ---------------------------------------------------------------------------

func (p *Pipeline) startProcessing(ctx context.Context, userText string) {
	p.session.SetState(StateProcessing)

	// Grab accumulated thinker output before clearing
	previousThought := p.getDraftOutput()
	p.resetDraftOutput()

	p.history.AddUser(userText)

	log.Printf("Processing user input: %s", truncate(userText, 100))
	if previousThought != "" {
		log.Printf("With %d chars of pre-thinking", len(previousThought))
	}

	// Send user text to browser for display
	p.session.SendJSON(WSMessage{Type: "transcript", Text: userText})

	// TTS sentence queue — decouples LLM generation from TTS synthesis
	sentenceCh := make(chan string, p.adaptive.Get("sentence_ch"))

	var ttsWg sync.WaitGroup
	ttsWg.Add(1)
	go func() {
		defer ttsWg.Done()
		p.ttsWorker(ctx, sentenceCh)
	}()

	// Stream tokens from Large LLM (with accumulated thought if available)
	messages := p.history.ToOpenAIWithThought(previousThought)
	tokenCh := p.largeLLM.StreamChat(ctx, messages)

	totalTokens := 0 // ALL tokens (including <think>) for budget accounting
	var sentenceBuf strings.Builder
	var allTokens strings.Builder
	fillerSent := false
	firstSentenceSent := false

	var tf thinkFilter

	for token := range tokenCh {
		if ctx.Err() != nil {
			break
		}

		totalTokens++

		// Strip <think>...</think> blocks — only pass visible content through
		visible := tf.Feed(token)

		if visible != "" {
			p.tokensMu.Lock()
			p.generatedTokens.WriteString(visible)
			p.tokensMu.Unlock()

			allTokens.WriteString(visible)
			p.session.SendJSON(WSMessage{Type: "response", Text: visible})
		}

		// Budget window counts ALL tokens: gives the model time to think
		// internally, but if the budget runs out with nothing spoken, inject filler.
		if totalTokens <= p.config.TokenBudget {
			if visible != "" {
				sentenceBuf.WriteString(visible)
				if isSentenceEnd(visible) && sentenceBuf.Len() > 0 {
					sentence := sentenceBuf.String()
					sentenceBuf.Reset()
					p.adaptive.RecordLen("sentence_ch", len(sentenceCh))
					sentenceCh <- sentence
					firstSentenceSent = true
					if p.session.GetState() == StateProcessing {
						p.session.SetState(StateSpeaking)
					}
				}
			}
			continue
		}

		// Past token budget: if no sentence produced yet, inject filler
		if !firstSentenceSent && !fillerSent {
			p.adaptive.RecordLen("sentence_ch", len(sentenceCh))
			sentenceCh <- p.config.FillerPhrase1
			fillerSent = true
			if p.session.GetState() == StateProcessing {
				p.session.SetState(StateSpeaking)
			}
		}

		if visible != "" {
			sentenceBuf.WriteString(visible)
			if isSentenceEnd(visible) {
				sentence := sentenceBuf.String()
				sentenceBuf.Reset()
				p.adaptive.RecordLen("sentence_ch", len(sentenceCh))
				sentenceCh <- sentence
				firstSentenceSent = true
			}
		}
	}

	// Flush any buffered partial content from the filter
	if flushed := tf.Flush(); flushed != "" {
		allTokens.WriteString(flushed)
		p.tokensMu.Lock()
		p.generatedTokens.WriteString(flushed)
		p.tokensMu.Unlock()
		sentenceBuf.WriteString(flushed)
	}

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

	p.adaptive.Adjust()
	p.adaptive.Save(p.config.AdaptiveSizesFile)
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

// ---------------------------------------------------------------------------
// Interrupt detection (uses Small LLM via SDK Agent)
// ---------------------------------------------------------------------------

const interruptDetectionPrompt = `你是语音意图检测模型。给定一段ASR识别文本，判断其是否包含有意义的用户意图。
interrupt — 文本含有实际语义：问题、指令、陈述、确认、否定、哪怕是未说完的半句话。
do not interrupt — 文本仅为无语义噪声：语气词（嗯、啊、哦、呃、emm）、咳嗽/笑声的误识别、重复填充音、空白或乱码。
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
	label := strings.ToLower(strings.TrimSpace(resp))
	label = strings.Trim(label, " \t\r\n\"'`.,!?;:()[]{}<>，。！？；：")

	// Prefer exact label match first.
	switch label {
	case "interrupt":
		return true
	case "do not interrupt", "do-not-interrupt", "donotinterrupt":
		return false
	}

	// Fallback for noisy outputs, e.g. "interrupt." / "do not interrupt\n".
	if strings.Contains(label, "do not interrupt") || strings.Contains(label, "do-not-interrupt") {
		return false
	}
	if strings.Contains(label, "interrupt") {
		return true
	}

	// Conservative fallback: treat unknown output as interrupt.
	log.Printf("interrupt detection unexpected output: %q", resp)
	return true
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
