package main

import (
	"context"
	"encoding/json"
	"log"
	"sync"

	"github.com/gorilla/websocket"
)

type SessionState int

const (
	StateIdle SessionState = iota
	StateListening
	StateProcessing
	StateSpeaking
)

func (s SessionState) String() string {
	switch s {
	case StateIdle:
		return "idle"
	case StateListening:
		return "listening"
	case StateProcessing:
		return "processing"
	case StateSpeaking:
		return "speaking"
	}
	return "unknown"
}

type WSMessage struct {
	Type  string `json:"type"`
	Text  string `json:"text,omitempty"`
	State string `json:"state,omitempty"`
}

type Session struct {
	conn   *websocket.Conn
	config *Config

	state   SessionState
	stateMu sync.RWMutex

	pipeline *Pipeline

	pipelineCancel context.CancelFunc
	pipelineMu     sync.Mutex

	// All writes go through this channel to avoid concurrent WS writes
	writeCh chan writeItem

	done chan struct{}
	once sync.Once
}

type writeItem struct {
	msgType int
	data    []byte
}

func NewSession(conn *websocket.Conn, config *Config) *Session {
	sizes := LoadChannelSizes(config.AdaptiveSizesFile, DefaultChannelSizes())
	s := &Session{
		conn:    conn,
		config:  config,
		state:   StateIdle,
		writeCh: make(chan writeItem, sizes.WriteCh),
		done:    make(chan struct{}),
	}
	s.pipeline = NewPipeline(s, config)
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

func (s *Session) handleTextMessage(msg WSMessage) {
	switch msg.Type {
	case "vad_start":
		s.onVADStart()
	case "vad_end":
		s.onVADEnd()
	}
}

func (s *Session) handleAudioData(data []byte) {
	state := s.GetState()
	if state == StateListening {
		s.pipeline.OnAudioData(data)
	}
}

func (s *Session) onVADStart() {
	state := s.GetState()

	switch state {
	case StateIdle:
		s.SetState(StateListening)
		ctx := s.newPipelineContext()
		go s.pipeline.StartListening(ctx)

	case StateProcessing, StateSpeaking:
		// User interrupts — preserve history, start new listening
		s.pipeline.OnInterrupt()
		s.cancelCurrentPipeline()
		s.SetState(StateListening)
		ctx := s.newPipelineContext()
		go s.pipeline.StartListening(ctx)
	}
}

func (s *Session) onVADEnd() {
	state := s.GetState()
	if state == StateListening {
		s.pipeline.OnVADEnd()
	}
}
