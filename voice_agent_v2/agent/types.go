package agent

import (
	"context"
	"encoding/json"
	"net/http"

	icfg "voiceagentv2/internal/config"
	itypes "voiceagentv2/internal/types"
)

// Type aliases — agent package uses internal types directly.
type (
	ContextMessage   = itypes.ContextMessage
	ConversationTurn = itypes.ConversationTurn
	PushContextRequest = itypes.PushContextRequest
	ReferenceFileReq = itypes.ReferenceFileReq
	PPTInitRequest   = itypes.PPTInitRequest
	PPTInitResponse  = itypes.PPTInitResponse
	PPTFeedbackRequest = itypes.PPTFeedbackRequest
	KBQueryRequest   = itypes.KBQueryRequest
	KBQueryResponse  = itypes.KBQueryResponse
	MemoryRecallRequest  = itypes.MemoryRecallRequest
	MemoryRecallResponse = itypes.MemoryRecallResponse
	SearchRequest    = itypes.SearchRequest
	SearchResponse   = itypes.SearchResponse
	VADEvent         = itypes.VADEvent
)

func newID(prefix string) string { return itypes.NewID(prefix) }

// Config is the runtime configuration (aliased from internal/config).
type Config = icfg.Config

// ExternalServices is the interface for all outbound service calls.
// Uses internal/types so it is compatible with executor.ClientProvider.
type ExternalServices interface {
	InitPPT(ctx context.Context, req itypes.PPTInitRequest) (itypes.PPTInitResponse, error)
	SendFeedback(ctx context.Context, req itypes.PPTFeedbackRequest) error
	GetCanvasStatus(ctx context.Context, taskID string) (itypes.CanvasStatusResponse, error)
	NotifyVADEvent(ctx context.Context, event itypes.VADEvent) error
	QueryKB(ctx context.Context, req itypes.KBQueryRequest) (itypes.KBQueryResponse, error)
	RecallMemory(ctx context.Context, req itypes.MemoryRecallRequest) (itypes.MemoryRecallResponse, error)
	PushContext(ctx context.Context, req itypes.PushContextRequest) error
	SearchWeb(ctx context.Context, req itypes.SearchRequest) (itypes.SearchResponse, error)
	UploadFile(r *http.Request) (json.RawMessage, error)
}

// APIResponse is the standard HTTP response envelope.
type APIResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message,omitempty"`
	Data    any    `json:"data,omitempty"`
}
