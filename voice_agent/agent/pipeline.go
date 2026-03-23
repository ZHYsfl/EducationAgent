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

	var asrProv asr.ASRProvider
	if config.ASRMode == "remote" {
		asrProv = asr.NewDouBaoASRClient(asr.DouBaoASRConfig{
			AppKey:     config.DouBaoASRAppKey,
			AccessKey:  config.DouBaoASRAccessKey,
			ResourceId: config.DouBaoASRResourceId,
		})
		log.Printf("ASR mode: remote (Doubao)")
	} else {
		asrProv = asr.NewASRClient(config.ASRWSURL)
		log.Printf("ASR mode: local (%s)", config.ASRWSURL)
	}

	var ttsProv tts.TTSProvider
	if config.TTSMode == "remote" {
		ttsProv = tts.NewDouBaoTTSClient(tts.DouBaoTTSConfig{
			AppId:     config.DouBaoTTSAppId,
			Token:     config.DouBaoTTSToken,
			Cluster:   config.DouBaoTTSCluster,
			VoiceType: config.DouBaoTTSVoiceType,
		})
		log.Printf("TTS mode: remote (Doubao %s)", config.DouBaoTTSVoiceType)
	} else {
		ttsProv = tts.NewTTSClient(config.TTSURL)
		log.Printf("TTS mode: local (%s)", config.TTSURL)
	}

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
