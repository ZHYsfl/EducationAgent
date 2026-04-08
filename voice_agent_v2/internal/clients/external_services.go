package clients

import (
	"context"
	"encoding/json"
	"net/http"

	types "voiceagentv2/internal/types"
)

// ExternalServices abstracts all outbound HTTP calls so that
// production code uses the real ServiceClients while tests can
// inject mocks without an HTTP server.
type ExternalServices interface {
	QueryKB(ctx context.Context, req types.KBQueryRequest) (types.KBQueryResponse, error)
	RecallMemory(ctx context.Context, req types.MemoryRecallRequest) (types.MemoryRecallResponse, error)
	PushContext(ctx context.Context, req types.PushContextRequest) error
	SearchWeb(ctx context.Context, req types.SearchRequest) (types.SearchResponse, error)
	InitPPT(ctx context.Context, req types.PPTInitRequest) (types.PPTInitResponse, error)
	SendFeedback(ctx context.Context, req types.PPTFeedbackRequest) error
	GetCanvasStatus(ctx context.Context, taskID string) (types.CanvasStatusResponse, error)
	UploadFile(r *http.Request) (json.RawMessage, error)
	NotifyVADEvent(ctx context.Context, event types.VADEvent) error
}
