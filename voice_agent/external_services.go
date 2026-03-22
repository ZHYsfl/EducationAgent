package main

import (
	"context"
	"encoding/json"
	"net/http"
)

// ExternalServices abstracts all outbound HTTP calls so that
// production code uses the real ServiceClients while tests can
// inject mocks without an HTTP server.
type ExternalServices interface {
	QueryKB(ctx context.Context, req KBQueryRequest) (KBQueryResponse, error)
	RecallMemory(ctx context.Context, req MemoryRecallRequest) (MemoryRecallResponse, error)
	GetUserProfile(ctx context.Context, userID string) (UserProfile, error)
	SearchWeb(ctx context.Context, req SearchRequest) (SearchResponse, error)
	InitPPT(ctx context.Context, req PPTInitRequest) (PPTInitResponse, error)
	SendFeedback(ctx context.Context, req PPTFeedbackRequest) error
	GetCanvasStatus(ctx context.Context, taskID string) (CanvasStatusResponse, error)
	UploadFile(r *http.Request) (json.RawMessage, error)
	ExtractMemory(ctx context.Context, req MemoryExtractRequest) (MemoryExtractResponse, error)
	SaveWorkingMemory(ctx context.Context, req WorkingMemorySaveRequest) error
	GetWorkingMemory(ctx context.Context, sessionID string) (*WorkingMemory, error)
	NotifyVADEvent(ctx context.Context, event VADEvent) error
	IngestFromSearch(ctx context.Context, req IngestFromSearchRequest) error
}
