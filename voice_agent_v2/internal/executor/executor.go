package executor

import (
	"context"
	"fmt"
	"time"

	"voiceagentv2/internal/protocol"
	"voiceagentv2/internal/types"
)

type Executor struct {
	clients ClientProvider
}

type SessionContext struct {
	UserID            string
	SessionID         string
	ActiveTaskID      string
	ViewingPageID     string
	BaseTimestamp     int64
	Topic             string
	Subject           string
	TotalPages        int
	Audience          string
	GlobalStyle       string
	KnowledgePoints   []string
	TeachingGoals     []string
	TeachingLogic     string
	KeyDifficulties   []string
	Duration          string
	InteractionDesign string
	OutputFormats     []string
	ReferenceFiles    []types.ReferenceFileReq
}

type ClientProvider interface {
	InitPPT(ctx context.Context, req types.PPTInitRequest) (types.PPTInitResponse, error)
	SendFeedback(ctx context.Context, req types.PPTFeedbackRequest) error
	QueryKB(ctx context.Context, req types.KBQueryRequest) (types.KBQueryResponse, error)
	RecallMemory(ctx context.Context, req types.MemoryRecallRequest) (types.MemoryRecallResponse, error)
	SearchWeb(ctx context.Context, req types.SearchRequest) (types.SearchResponse, error)
}

type ResultCallback func(types.ContextMessage)

func New(clients ClientProvider) *Executor {
	return &Executor{
		clients: clients,
	}
}

func (e *Executor) Execute(action protocol.Action, sessionCtx SessionContext, callback ResultCallback) {
	go func() {
		var result string
		var priority string = "normal"

		switch action.Type {
		case "update_requirements":
			result = e.executeUpdateRequirements(context.Background(), action.Params, sessionCtx)
		case "ppt_init":
			result = e.executePPTInit(context.Background(), action.Params, sessionCtx, callback)
		case "ppt_mod":
			result = e.executePPTModify(context.Background(), action.Params, sessionCtx)
		case "kb_query":
			result = e.executeKBQuery(context.Background(), action.Params, sessionCtx)
		case "get_memory":
			result = e.executeGetMemory(context.Background(), action.Params, sessionCtx)
		case "web_search":
			result = e.executeWebSearch(context.Background(), action.Params, sessionCtx)
		case "require_confirm":
			result = e.executeRequireConfirm(context.Background(), action.Params, sessionCtx)
		default:
			result = fmt.Sprintf("Unknown action: %s", action.Type)
		}

		if action.Params["p"] == "h" {
			priority = "high"
		}

		callback(types.ContextMessage{
			ID:        types.NewID("ctx_"),
			Content:   result,
			Priority:  priority,
			EventType: action.Type,
			Metadata:  action.Params,
			Timestamp: time.Now().Unix(),
		})
	}()
}
