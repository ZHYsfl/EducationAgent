package executor

import (
	"context"
	"fmt"
	"time"

	"voiceagent/internal/bus"
	"voiceagent/internal/protocol"
	"voiceagent/internal/types"
)

type Executor struct {
	bus     *bus.Bus
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
	SearchWeb(ctx context.Context, req types.SearchRequest) (types.SearchResponse, error)
}

type ResultCallback func(types.ContextMessage)

func New(b *bus.Bus, clients ClientProvider) *Executor {
	return &Executor{
		bus:     b,
		clients: clients,
	}
}

func (e *Executor) Execute(action protocol.Action, sessionCtx SessionContext, callback ResultCallback) {
	go func() {
		var result string
		var priority string = "normal"
		var msgType string

		switch action.Type {
		case "update_requirements":
			result = e.executeUpdateRequirements(context.Background(), action.Params, sessionCtx)
			msgType = "requirements_updated"
		case "ppt_init":
			result = e.executePPTInit(context.Background(), action.Params, sessionCtx)
			msgType = "ppt_status"
		case "ppt_mod":
			result = e.executePPTModify(context.Background(), action.Params, sessionCtx)
			msgType = "ppt_status"
		case "kb_query":
			result = e.executeKBQuery(context.Background(), action.Params, sessionCtx)
			msgType = "kb_summary"
		case "web_search":
			result = e.executeWebSearch(context.Background(), action.Params, sessionCtx)
			msgType = "search_result"
		default:
			result = fmt.Sprintf("Unknown action: %s", action.Type)
			msgType = "tool_result"
		}

		if action.Params["p"] == "h" {
			priority = "high"
		}

		callback(types.ContextMessage{
			ID:         types.NewID("ctx_"),
			Content:    result,
			Priority:   priority,
			ActionType: action.Type,
			MsgType:    msgType,
			Metadata:   action.Params,
			Timestamp:  time.Now().Unix(),
		})
	}()
}
