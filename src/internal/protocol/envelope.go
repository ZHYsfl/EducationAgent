// Package protocol defines the wire envelope shared by all six API surfaces.
package protocol

import "time"

// Envelope is the outer JSON frame of every request and response.
type Envelope struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	Timestamp string `json:"timestamp"`
	Code      int    `json:"code,omitempty"`
	Data      any    `json:"data,omitempty"`
}

// NewEnvelope builds an Envelope with the current timestamp.
func NewEnvelope(typ, id string, data any) Envelope {
	return Envelope{
		Type:      typ,
		ID:        id,
		Timestamp: time.Now().Format(time.RFC3339),
		Data:      data,
	}
}

// Message types, grouped by API section (see API.md).
const (
	TypeVadStartRequest  = "vad_engine/vad_start_request"
	TypeVadStartResponse = "vad_engine/vad_start_response"
	TypeVadEndRequest    = "vad_engine/vad_end_request"
	TypeVadEndResponse   = "vad_engine/vad_end_response"
)

const (
	TypeTTSInferRequest          = "tts_engine/infer_request"
	TypeTTSInferResponseAudio    = "tts_engine/infer_response/audio_header"
	TypeTTSInferResponseFinished = "tts_engine/infer_response/finished"
)

const (
	TypeASRInferRequest  = "asr_engine/infer_request"
	TypeASRInferResponse = "asr_engine/infer_response"
)

const (
	TypeLLMInferRequest  = "llm_engine/infer_request"
	TypeLLMInferResponse = "llm_engine/infer_response"
)

const (
	TypeSessionInterruptRequest  = "session/interrupt_request"
	TypeSessionInterruptResponse = "session/interrupt_response"
)
