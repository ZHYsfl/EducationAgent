package service

import (
	"context"

	"memory_service/internal/model"
)

type RecallJob struct {
	RequestID string
	UserID    string
	SessionID string
	Query     string
	TopK      int
}

type ContextPushJob struct {
	RequestID string
	UserID    string
	SessionID string
	Messages  []model.ConversationTurn
}

type AsyncDispatcher interface {
	DispatchRecall(ctx context.Context, job RecallJob) error
	DispatchContextPush(ctx context.Context, job ContextPushJob) error
}

type VoicePPTMessageRequest struct {
	TaskID    string
	SessionID string
	RequestID string
	EventType string
	Summary   string
}

type VoiceAgentClient interface {
	SendPPTMessage(ctx context.Context, req VoicePPTMessageRequest) error
}

type ArchiveConversationRequest struct {
	UserID    string
	SessionID string
	Messages  []model.ConversationTurn
}

type ArchiveIndexer interface {
	ArchiveConversation(ctx context.Context, req ArchiveConversationRequest) error
}

type NoopArchiveIndexer struct{}

func (NoopArchiveIndexer) ArchiveConversation(context.Context, ArchiveConversationRequest) error {
	return nil
}
