package agent

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	"toolcalling"
	"voiceagentv2/internal/adaptive"
	"voiceagentv2/internal/asr"
	"voiceagentv2/internal/audio"
	"voiceagentv2/internal/executor"
	"voiceagentv2/internal/history"
	"voiceagentv2/internal/protocol"
	"voiceagentv2/internal/tts"
)

// Pipeline owns all per-session AI processing state.
type Pipeline struct {
	session  *Session
	config   *Config
	clients  ExternalServices
	executor *executor.Executor

	// AI components
	asrClient asr.ASRProvider
	largeLLM  *toolcalling.Agent
	smallLLM  *toolcalling.Agent
	ttsClient tts.TTSProvider

	// session context (set by StartInteractive, used by EnqueueContext)
	sessionCtx   context.Context
	sessionCtxMu sync.RWMutex

	// conversation history
	history *history.ConversationHistory

	// adaptive channel sizing
	adaptive *adaptive.AdaptiveController

	// audio I/O channels (swapped per StartInteractive call)
	audioCh  chan []byte
	vadEndCh chan struct{}
	ioMu     sync.RWMutex
	runMu    sync.Mutex

	// O/T/A channels
	userInputCh chan string // ASR partial → thinkLoop
	tokenCh     chan string // thinkLoop → outputLoop
	sentenceCh  chan string // outputLoop → ttsWorker

	// raw token buffer for interrupt preservation
	rawTokens strings.Builder
	tokensMu  sync.Mutex

	// speculative think draft (consumed by startProcessing)
	thinkDraft   strings.Builder
	thinkDraftMu sync.Mutex

	// think stream cancellation
	thinkCancel   context.CancelFunc
	thinkCancelMu sync.Mutex

	// think action guard (suppresses new think starts after action detected)
	thinkGuardUntil time.Time
	thinkGuardMu    sync.RWMutex

	// context queues
	contextQueue      chan ContextMessage // normal priority tool results
	highPriorityQueue chan ContextMessage // conflict questions, system notifies

	// overflow buffer for messages that couldn't be queued
	pendingContexts []ContextMessage
	pendingMu       sync.Mutex

	// protocol parser (stateful, one per pipeline)
	parser *protocol.Parser

	// audio buffer
	audioBuf *audio.AudioBuffer
}

func NewPipeline(session *Session, config *Config, clients ExternalServices) *Pipeline {
	sizes := adaptive.LoadChannelSizes(config.AdaptiveSizesFile, adaptive.DefaultChannelSizes())

	p := &Pipeline{
		session:           session,
		config:            config,
		clients:           clients,
		asrClient:         asr.NewASRClient(config.ASRWSURL),
		ttsClient:         tts.NewTTSClient(config.TTSURL),
		largeLLM:          toolcalling.NewAgent(toolcalling.LLMConfig{APIKey: config.LargeLLMAPIKey, Model: config.LargeLLMModel, BaseURL: config.LargeLLMBaseURL}),
		smallLLM:          toolcalling.NewAgent(toolcalling.LLMConfig{APIKey: config.SmallLLMAPIKey, Model: config.SmallLLMModel, BaseURL: config.SmallLLMBaseURL}),
		history:           history.NewConversationHistory(config.SystemPrompt),
		adaptive:          adaptive.NewAdaptiveController(sizes),
		contextQueue:      make(chan ContextMessage, 64),
		highPriorityQueue: make(chan ContextMessage, 16),
		parser:            protocol.NewParser(),
		audioBuf:          audio.NewAudioBuffer(),
	}
	if clients != nil {
		p.executor = executor.New(clients)
	}
	return p
}

func (p *Pipeline) OnAudioData(data []byte) {
	p.ioMu.RLock()
	ch := p.audioCh
	p.ioMu.RUnlock()
	if ch != nil {
		select {
		case ch <- data:
		default:
		}
	}
}

func (p *Pipeline) OnVADEnd() {
	p.ioMu.RLock()
	ch := p.vadEndCh
	p.ioMu.RUnlock()
	if ch != nil {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// OnInterrupt is called when the user starts speaking mid-response.
// Waits up to 3s for any open @{...} action to close, then saves interrupted history.
func (p *Pipeline) OnInterrupt() {
	p.cancelThinkStream()

	for i := 0; i < 3; i++ {
		p.tokensMu.Lock()
		raw := p.rawTokens.String()
		p.tokensMu.Unlock()
		if !protocol.HasOpenActionPrefix(raw) {
			break
		}
		time.Sleep(time.Second)
	}

	p.tokensMu.Lock()
	raw := p.rawTokens.String()
	p.rawTokens.Reset()
	p.tokensMu.Unlock()

	if trimmed, changed := protocol.TrimTrailingIncompleteAction(raw); changed {
		raw = trimmed
	}
	if raw != "" {
		p.history.AddInterruptedAssistant(raw)
		log.Printf("[pipeline] interrupt: preserved %d chars", len(raw))
	}
}

func (p *Pipeline) pushRemainingContext() {
	if p.clients == nil {
		return
	}
	p.session.memoryMu.Lock()
	msgs := p.history.Messages()
	start := p.session.lastMemoryPushIdx
	end := len(msgs)
	p.session.memoryMu.Unlock()

	if start >= end {
		return
	}
	turns := make([]ConversationTurn, 0, end-start)
	for _, m := range msgs[start:end] {
		turns = append(turns, ConversationTurn{Role: m.Role, Content: m.Content})
	}
	p.clients.PushContext(context.Background(), PushContextRequest{
		UserID:    p.session.UserID,
		SessionID: p.session.SessionID,
		Messages:  turns,
	})
}
