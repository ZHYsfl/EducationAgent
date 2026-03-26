package agent

import (
	"context"
	"log"
	"strings"
	"sync"

	"toolcalling"
	adaptivepkg "voiceagent/internal/adaptive"
	"voiceagent/internal/audio"
	"voiceagent/internal/asr"
	svcclients "voiceagent/internal/clients"
	cfgpkg "voiceagent/internal/config"
	hist "voiceagent/internal/history"
	"voiceagent/internal/think"
	"voiceagent/internal/tts"
	types "voiceagent/internal/types"
)

type Pipeline struct {
	session *Session
	config  *cfgpkg.Config
	clients svcclients.ExternalServices

	asrClient asr.ASRProvider
	smallLLM  *toolcalling.Agent
	largeLLM  *toolcalling.Agent
	ttsClient tts.TTSProvider

	history  *hist.ConversationHistory
	adaptive *adaptivepkg.AdaptiveController

	audioBuf *audio.AudioBuffer
	audioCh  chan []byte   // audio data from session → pipeline
	vadEndCh chan struct{} // signal: user stopped speaking
	ioMu     sync.RWMutex  // protects audioCh/vadEndCh pointer swaps
	runMu    sync.Mutex    // ensures only one StartListening runs at a time

	// For interrupt preservation: tracks ALL tokens including <think> content
	rawGeneratedTokens strings.Builder
	tokensMu           sync.Mutex

	// Draft thinking
	draftCancel   context.CancelFunc
	draftMu       sync.Mutex
	draftOutput   strings.Builder // accumulated thinker output across rounds
	draftOutputMu sync.Mutex

	contextQueue      chan types.ContextMessage
	pendingContexts   []types.ContextMessage
	pendingMu         sync.Mutex
	highPriorityQueue chan types.ContextMessage
}

func NewPipeline(session *Session, config *cfgpkg.Config, clients svcclients.ExternalServices) *Pipeline {
	sizes := adaptivepkg.LoadChannelSizes(config.AdaptiveSizesFile, adaptivepkg.DefaultChannelSizes())

	asrProv := asr.NewASRClient(config.ASRWSURL)
	log.Printf("ASR: %s", config.ASRWSURL)

	ttsProv := tts.NewTTSClient(config.TTSURL)
	log.Printf("TTS: %s", config.TTSURL)

	return &Pipeline{
		session:   session,
		config:    config,
		clients:   clients,
		asrClient: asrProv,
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
		ttsClient:         ttsProv,
		history:           hist.NewConversationHistory(config.SystemPrompt),
		audioBuf:          audio.NewAudioBuffer(),
		adaptive:          adaptivepkg.NewAdaptiveController(sizes),
		contextQueue:      make(chan types.ContextMessage, 64),
		highPriorityQueue: make(chan types.ContextMessage, 16),
	}
}

// OnAudioData is called by the session when audio arrives during LISTENING.
func (p *Pipeline) OnAudioData(data []byte) {
	p.ioMu.RLock()
	audioCh := p.audioCh
	p.ioMu.RUnlock()
	if audioCh != nil {
		p.adaptive.RecordLen("audio_ch", len(audioCh))
		select {
		case audioCh <- data:
		default:
			p.adaptive.RecordBlock("audio_ch")
		}
	}
}

// OnVADEnd is called when the browser signals end of speech.
func (p *Pipeline) OnVADEnd() {
	p.ioMu.RLock()
	vadEndCh := p.vadEndCh
	p.ioMu.RUnlock()
	if vadEndCh != nil {
		select {
		case vadEndCh <- struct{}{}:
		default:
		}
	}
}

// ---------------------------------------------------------------------------
// Exported accessors for testing (used from tests/agent, package agent_test)
// ---------------------------------------------------------------------------

// GetAudioBuffer returns the audio buffer.
func (p *Pipeline) GetAudioBuffer() *audio.AudioBuffer { return p.audioBuf }

// GetHistory returns the conversation history.
func (p *Pipeline) GetHistory() *hist.ConversationHistory { return p.history }

// GetAdaptiveController returns the adaptive controller.
func (p *Pipeline) GetAdaptiveController() *adaptivepkg.AdaptiveController { return p.adaptive }

// GetContextQueue returns the context queue channel (bidirectional for tests).
func (p *Pipeline) GetContextQueue() chan types.ContextMessage { return p.contextQueue }

// GetHighPriorityQueue returns the high-priority queue channel (bidirectional for tests).
func (p *Pipeline) GetHighPriorityQueue() chan types.ContextMessage { return p.highPriorityQueue }

// GetTTSClient returns the TTS provider.
func (p *Pipeline) GetTTSClient() tts.TTSProvider { return p.ttsClient }

// SetTTSClient sets the TTS provider.
func (p *Pipeline) SetTTSClient(c tts.TTSProvider) { p.ttsClient = c }

// GetASRClient returns the ASR provider.
func (p *Pipeline) GetASRClient() asr.ASRProvider { return p.asrClient }

// SetASRClient sets the ASR provider.
func (p *Pipeline) SetASRClient(c asr.ASRProvider) { p.asrClient = c }

// GetSmallLLM returns the small LLM agent.
func (p *Pipeline) GetSmallLLM() *toolcalling.Agent { return p.smallLLM }

// SetSmallLLM sets the small LLM agent.
func (p *Pipeline) SetSmallLLM(a *toolcalling.Agent) { p.smallLLM = a }

// GetLargeLLM returns the large LLM agent.
func (p *Pipeline) GetLargeLLM() *toolcalling.Agent { return p.largeLLM }

// SetLargeLLM sets the large LLM agent.
func (p *Pipeline) SetLargeLLM(a *toolcalling.Agent) { p.largeLLM = a }

// GetConfig returns the pipeline config (pointer, so tests can mutate fields).
func (p *Pipeline) GetConfig() *cfgpkg.Config { return p.config }


// GetAudioCh returns the current audioCh (may be nil).
func (p *Pipeline) GetAudioCh() chan []byte {
	p.ioMu.RLock()
	defer p.ioMu.RUnlock()
	return p.audioCh
}

// SetAudioCh sets the audioCh directly (for testing — bypasses ioMu swap logic).
func (p *Pipeline) SetAudioCh(ch chan []byte) {
	p.ioMu.Lock()
	p.audioCh = ch
	p.ioMu.Unlock()
}

// GetVADEndCh returns the current vadEndCh (may be nil).
func (p *Pipeline) GetVADEndCh() chan struct{} {
	p.ioMu.RLock()
	defer p.ioMu.RUnlock()
	return p.vadEndCh
}

// SetVADEndCh sets the vadEndCh directly (for testing).
func (p *Pipeline) SetVADEndCh(ch chan struct{}) {
	p.ioMu.Lock()
	p.vadEndCh = ch
	p.ioMu.Unlock()
}

// WriteRawTokens appends to the rawGeneratedTokens builder under tokensMu.
func (p *Pipeline) WriteRawTokens(s string) {
	p.tokensMu.Lock()
	p.rawGeneratedTokens.WriteString(s)
	p.tokensMu.Unlock()
}

// RawTokensLen returns the length of the rawGeneratedTokens builder under tokensMu.
func (p *Pipeline) RawTokensLen() int {
	p.tokensMu.Lock()
	defer p.tokensMu.Unlock()
	return p.rawGeneratedTokens.Len()
}

// LockPending acquires the pendingMu lock (for testing inspection of pendingContexts).
func (p *Pipeline) LockPending() { p.pendingMu.Lock() }

// UnlockPending releases the pendingMu lock.
func (p *Pipeline) UnlockPending() { p.pendingMu.Unlock() }

// GetPendingContexts returns the pendingContexts slice (caller must hold pendingMu).
func (p *Pipeline) GetPendingContexts() []types.ContextMessage { return p.pendingContexts }

// SetPendingContexts sets the pendingContexts slice (caller must hold pendingMu or use under lock).
func (p *Pipeline) SetPendingContexts(msgs []types.ContextMessage) {
	p.pendingMu.Lock()
	p.pendingContexts = msgs
	p.pendingMu.Unlock()
}

// GetDraftCancel returns the draft cancel func.
func (p *Pipeline) GetDraftCancel() context.CancelFunc {
	p.draftMu.Lock()
	defer p.draftMu.Unlock()
	return p.draftCancel
}

// SetDraftCancel sets the draft cancel func.
func (p *Pipeline) SetDraftCancel(c context.CancelFunc) {
	p.draftMu.Lock()
	defer p.draftMu.Unlock()
	p.draftCancel = c
}

// ---------------------------------------------------------------------------

// OnInterrupt is called when user starts speaking during PROCESSING/SPEAKING.
// Saves partial LLM output (including <think> reasoning) to history before
// the pipeline context is cancelled, so the model can resume from where it
// left off in the next turn.
func (p *Pipeline) OnInterrupt() {
	p.tokensMu.Lock()
	raw := p.rawGeneratedTokens.String()
	p.rawGeneratedTokens.Reset()
	p.tokensMu.Unlock()

	if raw != "" {
		// Close unclosed <think> tag so the model sees well-formed history.
		// Strip any partial </think> suffix (e.g. "</thi") before appending.
		if strings.Contains(raw, "<think>") && !strings.Contains(raw, "</think>") {
			if partial := think.LongestSuffixPrefix(raw, "</think>"); partial != "" {
				raw = raw[:len(raw)-len(partial)]
			}
			raw += "</think>"
		}
		p.history.AddInterruptedAssistant(raw)
		log.Printf("Interrupt: preserved %d chars (including thinking)", len(raw))
	}

	p.cancelDraft()
}
