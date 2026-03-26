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

type ClientProvider interface {
	InitPPT(ctx context.Context, req types.PPTInitRequest) (types.PPTInitResponse, error)
	SendFeedback(ctx context.Context, req types.PPTFeedbackRequest) error
	QueryKB(ctx context.Context, req types.KBQueryRequest) (types.KBQueryResponse, error)
}

type ResultCallback func(types.ContextMessage)

func New(b *bus.Bus, clients ClientProvider) *Executor {
	return &Executor{
		bus:     b,
		clients: clients,
	}
}

func (e *Executor) Execute(action protocol.Action, callback ResultCallback) {
	go func() {
		var result string
		var priority string = "normal"

		switch action.Type {
		case "ppt_init":
			result = e.executePPTInit(context.Background(), action.Params)
		case "ppt_mod":
			result = e.executePPTModify(context.Background(), action.Params)
		case "kb_query":
			result = e.executeKBQuery(context.Background(), action.Params)
		default:
			result = fmt.Sprintf("Unknown action: %s", action.Type)
		}

		if action.Params["p"] == "h" {
			priority = "high"
		}

		callback(types.ContextMessage{
			Content:   result,
			Priority:  priority,
			Source:    action.Type,
			Timestamp: time.Now().Unix(),
		})
	}()
}
