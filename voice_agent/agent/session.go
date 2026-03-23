package agent

import (
	"context"
	"encoding/json"
	"log"

	"github.com/gorilla/websocket"
	adaptivepkg "voiceagent/internal/adaptive"
	cfgpkg "voiceagent/internal/config"
	svcclients "voiceagent/internal/clients"
	types "voiceagent/internal/types"
)

func NewSession(conn *websocket.Conn, config *cfgpkg.Config, clients svcclients.ExternalServices, sessionID, userID string) *Session {
	if sessionID == "" {
		sessionID = types.NewID("sess_")
	}
	if userID == "" {
		userID = config.DefaultUserID
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
		PendingQuestions: make(map[string]string),
	}
	s.pipeline = NewPipeline(s, config, clients)
	return s
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
		s.conn.Close()
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
