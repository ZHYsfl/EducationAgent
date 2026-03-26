package agent

import (
	clients "voiceagent/internal/clients"
	cfg "voiceagent/internal/config"
	types "voiceagent/internal/types"
)

// ---------------------------------------------------------------------------
// Type aliases from internal/types — package agent code uses these directly.
// ---------------------------------------------------------------------------

type APIResponse = types.APIResponse
type ContextMessage = types.ContextMessage
type ReferenceFileReq = types.ReferenceFileReq
type InitTeachingElements = types.InitTeachingElements
type ReferenceFile = types.ReferenceFile
type PPTInitRequest = types.PPTInitRequest
type PPTInitResponse = types.PPTInitResponse
type PPTFeedbackRequest = types.PPTFeedbackRequest
type Intent = types.Intent
type CanvasStatusResponse = types.CanvasStatusResponse
type PageStatusInfo = types.PageStatusInfo
type KBQueryRequest = types.KBQueryRequest
type KBQueryResponse = types.KBQueryResponse
type RetrievedChunk = types.RetrievedChunk
type MemoryRecallRequest = types.MemoryRecallRequest
type MemoryRecallResponse = types.MemoryRecallResponse
type MemoryEntry = types.MemoryEntry
type WorkingMemory = types.WorkingMemory
type UserProfile = types.UserProfile
type SearchRequest = types.SearchRequest
type SearchResponse = types.SearchResponse
type SearchResult = types.SearchResult
type IngestFromSearchRequest = types.IngestFromSearchRequest
type SearchIngestItem = types.SearchIngestItem
type MemoryExtractRequest = types.MemoryExtractRequest
type ConversationTurn = types.ConversationTurn
type MemoryExtractResponse = types.MemoryExtractResponse
type WorkingMemorySaveRequest = types.WorkingMemorySaveRequest
type VADEvent = types.VADEvent
type FileUploadData = types.FileUploadData

// NewID wraps types.NewID so package agent code can call it without qualification.
func NewID(prefix string) string { return types.NewID(prefix) }

// decodeAPIData wraps clients.DecodeAPIData so package agent (and its tests)
// can call the function without qualification.
func decodeAPIData(raw []byte, out any) error { return clients.DecodeAPIData(raw, out) }

// ---------------------------------------------------------------------------
// Type aliases from internal/clients and internal/config.
// ---------------------------------------------------------------------------

type ExternalServices = clients.ExternalServices
type Config = cfg.Config
