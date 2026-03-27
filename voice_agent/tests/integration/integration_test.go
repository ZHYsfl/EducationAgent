package integration_test

import (
	"context"
	"testing"
	"time"

	"voiceagent/internal/protocol"
	"voiceagent/internal/types"
)

type mockExternalServices struct{}


func (m *mockExternalServices) InitPPT(ctx context.Context, req types.PPTInitRequest) (types.PPTInitResponse, error) {
	return types.PPTInitResponse{TaskID: "test_task"}, nil
}

func (m *mockExternalServices) SendFeedback(ctx context.Context, req types.PPTFeedbackRequest) error {
	return nil
}

func (m *mockExternalServices) QueryKB(ctx context.Context, req types.KBQueryRequest) (types.KBQueryResponse, error) {
	return types.KBQueryResponse{}, nil
}

func (m *mockExternalServices) RecallMemory(ctx context.Context, req types.MemoryRecallRequest) (types.MemoryRecallResponse, error) {
	return types.MemoryRecallResponse{}, nil
}

func (m *mockExternalServices) GetUserProfile(ctx context.Context, userID string) (types.UserProfile, error) {
	return types.UserProfile{}, nil
}

func (m *mockExternalServices) SearchWeb(ctx context.Context, req types.SearchRequest) (types.SearchResponse, error) {
	return types.SearchResponse{}, nil
}

func (m *mockExternalServices) GetCanvasStatus(ctx context.Context, taskID string) (types.CanvasStatusResponse, error) {
	return types.CanvasStatusResponse{}, nil
}

func (m *mockExternalServices) UploadFile(r interface{}) (interface{}, error) {
	return nil, nil
}

func (m *mockExternalServices) ExtractMemory(ctx context.Context, req types.MemoryExtractRequest) (types.MemoryExtractResponse, error) {
	return types.MemoryExtractResponse{}, nil
}

func (m *mockExternalServices) SaveWorkingMemory(ctx context.Context, req types.WorkingMemorySaveRequest) error {
	return nil
}

func (m *mockExternalServices) GetWorkingMemory(ctx context.Context, sessionID string) (*types.WorkingMemory, error) {
	return nil, nil
}

func (m *mockExternalServices) NotifyVADEvent(ctx context.Context, event types.VADEvent) error {
	return nil
}

func (m *mockExternalServices) IngestFromSearch(ctx context.Context, req types.IngestFromSearchRequest) error {
	return nil
}

func TestProtocolIntegration(t *testing.T) {
	parser := protocol.Parser{}

	input := "好的，我来帮您创建PPT。@{ppt_init|topic:AI|desc:测试}"
	result := parser.Feed(input)

	if len(result.Actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(result.Actions))
	}

	if result.Actions[0].Type != "ppt_init" {
		t.Errorf("expected ppt_init, got %s", result.Actions[0].Type)
	}
}

func TestContextEnqueue(t *testing.T) {
	received := make(chan types.ContextMessage, 10)

	go func() {
		time.Sleep(50 * time.Millisecond)
		received <- types.ContextMessage{
			Content:    "test",
			Priority:   "normal",
			ActionType: "test",
		}
	}()

	select {
	case msg := <-received:
		if msg.Content != "test" {
			t.Errorf("expected test, got %s", msg.Content)
		}
	case <-time.After(200 * time.Millisecond):
		t.Error("timeout waiting for message")
	}
}


