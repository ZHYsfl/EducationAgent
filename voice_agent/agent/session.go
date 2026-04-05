package agent

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"

	adaptivepkg "voiceagent/internal/adaptive"
	svcclients "voiceagent/internal/clients"
	cfgpkg "voiceagent/internal/config"
	types "voiceagent/internal/types"

	"github.com/gorilla/websocket"
)

// ErrUserIDRequired is returned when NewSession is called with an empty user_id.
var ErrUserIDRequired = errors.New("session: user_id is required")

func NewSession(conn *websocket.Conn, config *cfgpkg.Config, clients svcclients.ExternalServices, sessionID, userID string) (*Session, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, ErrUserIDRequired
	}
	if sessionID == "" {
		sessionID = types.NewID("sess_")
	}
	sizes := adaptivepkg.LoadChannelSizes(config.AdaptiveSizesFile, adaptivepkg.DefaultChannelSizes())
	s := &Session{
		conn:             conn,
		config:           config,
		SessionID:        sessionID,
		UserID:           userID,
		state:            StateIdle,
		writeCh:          make(chan writeItem, sizes.WriteCh),
		done:             make(chan struct{}),
		clients:          clients,
		OwnedTasks:       make(map[string]string),
		PendingRequests:  make(map[string]string),
		PendingQuestions: make(map[string]PendingQuestion),
	}
	s.pipeline = NewPipeline(s, config, clients)
	return s, nil
}

func (s *Session) Run() {
	defer s.Close()
	go s.writeLoop()
	s.readLoop()
}

func (s *Session) Close() {
	s.once.Do(func() {
		close(s.done)
		s.cancelCurrentPipeline()
		if s.pipeline != nil {
			s.pipeline.pushRemainingContext()
		}
		if err := s.conn.Close(); err != nil {
			log.Printf("[session] WebSocket close error: %v", err)
		}
	})
}

func (s *Session) GetState() SessionState {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	return s.state
}

func (s *Session) SetState(state SessionState) {
	s.stateMu.Lock()
	s.state = state
	s.stateMu.Unlock()
	s.SendJSON(WSMessage{Type: "status", State: state.String()})
}

func (s *Session) CompareAndSetState(expected, next SessionState) bool {
	s.stateMu.Lock()
	if s.state != expected {
		s.stateMu.Unlock()
		return false
	}
	s.state = next
	s.stateMu.Unlock()
	s.SendJSON(WSMessage{Type: "status", State: next.String()})
	return true
}

func (s *Session) cancelCurrentPipeline() {
	s.pipelineMu.Lock()
	if s.pipelineCancel != nil {
		s.pipelineCancel()
		s.pipelineCancel = nil
	}
	s.pipelineMu.Unlock()
}

func (s *Session) newPipelineContext() context.Context {
	s.pipelineMu.Lock()
	if s.pipelineCancel != nil {
		s.pipelineCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.pipelineCancel = cancel
	s.pipelineMu.Unlock()
	return ctx
}

func (s *Session) SendJSON(msg WSMessage) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	if s.pipeline != nil {
		s.pipeline.adaptive.RecordLen("write_ch", len(s.writeCh))
	}
	select {
	case s.writeCh <- writeItem{msgType: websocket.TextMessage, data: data}:
	case <-s.done:
	}
}

func (s *Session) SendAudio(data []byte) {
	if s.pipeline != nil {
		s.pipeline.adaptive.RecordLen("write_ch", len(s.writeCh))
	}
	select {
	case s.writeCh <- writeItem{msgType: websocket.BinaryMessage, data: data}:
	case <-s.done:
	}
}

func (s *Session) readLoop() {
	for {
		msgType, data, err := s.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("WebSocket read error: %v", err)
			}
			return
		}

		switch msgType {
		case websocket.TextMessage:
			var msg WSMessage
			if err := json.Unmarshal(data, &msg); err != nil {
				log.Printf("Invalid JSON: %v", err)
				s.SendJSON(WSMessage{Type: "error", Code: 40001, Message: "Invalid message format"})
				continue
			}
			s.handleTextMessage(msg)

		case websocket.BinaryMessage:
			s.handleAudioData(data)
		}
	}
}

func (s *Session) writeLoop() {
	for {
		select {
		case item := <-s.writeCh:
			if err := s.conn.WriteMessage(item.msgType, item.data); err != nil {
				log.Printf("WebSocket write error: %v", err)
				return
			}
		case <-s.done:
			return
		}
	}
}

// RegisterSession adds a session to the global registry.
func RegisterSession(s *Session) {
	registerSession(s)
}

// UnregisterSession removes a session from the global registry.
func UnregisterSession(s *Session) {
	unregisterSession(s)
}

// ---------------------------------------------------------------------------
// Exported accessors for testing (used from tests/agent, package agent_test)
// ---------------------------------------------------------------------------

// GetRequirements returns the current TaskRequirements under reqMu.
func (s *Session) GetRequirements() *TaskRequirements {
	s.reqMu.RLock()
	defer s.reqMu.RUnlock()
	return s.Requirements
}

// SetRequirements sets the current TaskRequirements under reqMu.
func (s *Session) SetRequirements(r *TaskRequirements) {
	s.reqMu.Lock()
	defer s.reqMu.Unlock()
	s.Requirements = r
}

// GetPendingQuestions returns a snapshot copy of pending questions.
func (s *Session) GetPendingQuestions() map[string]PendingQuestion {
	s.pendingQMu.RLock()
	defer s.pendingQMu.RUnlock()
	out := make(map[string]PendingQuestion)
	for k, v := range s.PendingQuestions {
		out[k] = v
	}
	return out
}

// GetActiveTaskID returns the active task ID under activeTaskMu.
func (s *Session) GetActiveTaskID() string {
	s.activeTaskMu.RLock()
	defer s.activeTaskMu.RUnlock()
	return s.ActiveTaskID
}

// GetOwnedTasks returns a snapshot copy of owned tasks.
func (s *Session) GetOwnedTasks() map[string]string {
	s.activeTaskMu.RLock()
	defer s.activeTaskMu.RUnlock()
	out := make(map[string]string)
	for k, v := range s.OwnedTasks {
		out[k] = v
	}
	return out
}

// GetPipeline returns the current pipeline under pipelineMu.
func (s *Session) GetPipeline() *Pipeline {
	s.pipelineMu.Lock()
	defer s.pipelineMu.Unlock()
	return s.pipeline
}

// SetPipeline sets the current pipeline under pipelineMu.
func (s *Session) SetPipeline(p *Pipeline) {
	s.pipelineMu.Lock()
	defer s.pipelineMu.Unlock()
	s.pipeline = p
}

// GetPipelineCancel returns the current pipeline cancel func under pipelineMu.
func (s *Session) GetPipelineCancel() context.CancelFunc {
	s.pipelineMu.Lock()
	defer s.pipelineMu.Unlock()
	return s.pipelineCancel
}

// SetPipelineCancel sets the pipeline cancel func under pipelineMu.
func (s *Session) SetPipelineCancel(c context.CancelFunc) {
	s.pipelineMu.Lock()
	defer s.pipelineMu.Unlock()
	s.pipelineCancel = c
}

// GetWriteCh returns the write channel.
func (s *Session) GetWriteCh() chan writeItem {
	return s.writeCh
}

// SetViewingPageID sets the ViewingPageID field directly (for testing).
func (s *Session) SetViewingPageID(id string) {
	s.activeTaskMu.Lock()
	s.ViewingPageID = id
	s.activeTaskMu.Unlock()
}

// SetLastVADTimestamp sets the LastVADTimestamp field directly (for testing).
func (s *Session) SetLastVADTimestamp(ts int64) {
	s.activeTaskMu.Lock()
	s.LastVADTimestamp = ts
	s.activeTaskMu.Unlock()
}

// SetStateRaw sets the state directly without sending a WS message (for testing).
func (s *Session) SetStateRaw(state SessionState) {
	s.stateMu.Lock()
	s.state = state
	s.stateMu.Unlock()
}

// GetClients returns the ExternalServices clients.
func (s *Session) GetClients() ExternalServices {
	return s.clients
}

// GetConfig returns the Config.
func (s *Session) GetConfig() *Config {
	return s.config
}
