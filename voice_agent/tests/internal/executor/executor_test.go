package executor_test

import (
	"context"
	"sync"
	"testing"

	"voiceagent/internal/executor"
	"voiceagent/internal/protocol"
	"voiceagent/internal/types"
)

type mockClients struct {
	queryKBFn      func(ctx context.Context, req types.KBQueryRequest) (types.KBQueryResponse, error)
	searchWebFn    func(ctx context.Context, req types.SearchRequest) (types.SearchResponse, error)
	initPPTFn      func(ctx context.Context, req types.PPTInitRequest) (types.PPTInitResponse, error)
	sendFeedbackFn func(ctx context.Context, req types.PPTFeedbackRequest) error
}

func (m *mockClients) QueryKB(ctx context.Context, req types.KBQueryRequest) (types.KBQueryResponse, error) {
	if m.queryKBFn != nil {
		return m.queryKBFn(ctx, req)
	}
	return types.KBQueryResponse{}, nil
}

func (m *mockClients) SearchWeb(ctx context.Context, req types.SearchRequest) (types.SearchResponse, error) {
	if m.searchWebFn != nil {
		return m.searchWebFn(ctx, req)
	}
	return types.SearchResponse{}, nil
}

func (m *mockClients) InitPPT(ctx context.Context, req types.PPTInitRequest) (types.PPTInitResponse, error) {
	if m.initPPTFn != nil {
		return m.initPPTFn(ctx, req)
	}
	return types.PPTInitResponse{}, nil
}

func (m *mockClients) SendFeedback(ctx context.Context, req types.PPTFeedbackRequest) error {
	if m.sendFeedbackFn != nil {
		return m.sendFeedbackFn(ctx, req)
	}
	return nil
}

func TestExecute_UpdateRequirements(t *testing.T) {
	exec := executor.New(nil)

	action := protocol.Action{
		Type: "update_requirements",
		Params: map[string]string{
			"topic":    "数学",
			"audience": "大学生",
		},
	}

	sessionCtx := executor.SessionContext{
		UserID:    "u1",
		SessionID: "s1",
	}

	var receivedMsg types.ContextMessage
	var wg sync.WaitGroup
	wg.Add(1)

	callback := func(msg types.ContextMessage) {
		receivedMsg = msg
		wg.Done()
	}

	exec.Execute(action, sessionCtx, callback)
	wg.Wait()

	if receivedMsg.MsgType != "requirements_updated" {
		t.Errorf("expected msgType=requirements_updated, got %s", receivedMsg.MsgType)
	}
	if receivedMsg.Content == "" {
		t.Error("expected non-empty content")
	}
}

func TestExecute_KBQuery(t *testing.T) {
	mock := &mockClients{
		queryKBFn: func(ctx context.Context, req types.KBQueryRequest) (types.KBQueryResponse, error) {
			return types.KBQueryResponse{Accepted: true}, nil
		},
	}
	exec := executor.New(mock)

	action := protocol.Action{
		Type:   "kb_query",
		Params: map[string]string{"q": "测试查询"},
	}

	sessionCtx := executor.SessionContext{
		UserID:    "u1",
		SessionID: "s1",
		Subject:   "数学",
	}

	var receivedMsg types.ContextMessage
	var wg sync.WaitGroup
	wg.Add(1)

	callback := func(msg types.ContextMessage) {
		receivedMsg = msg
		wg.Done()
	}

	exec.Execute(action, sessionCtx, callback)
	wg.Wait()

	if receivedMsg.MsgType != "kb_summary" {
		t.Errorf("expected msgType=kb_summary, got %s", receivedMsg.MsgType)
	}
}

func TestExecute_PPTInit_MissingFields(t *testing.T) {
	mock := &mockClients{}
	exec := executor.New(mock)

	action := protocol.Action{
		Type:   "ppt_init",
		Params: map[string]string{"desc": "测试"},
	}

	sessionCtx := executor.SessionContext{
		UserID:    "u1",
		SessionID: "s1",
		Topic:     "数学",
	}

	var receivedMsg types.ContextMessage
	var wg sync.WaitGroup
	wg.Add(1)

	callback := func(msg types.ContextMessage) {
		receivedMsg = msg
		wg.Done()
	}

	exec.Execute(action, sessionCtx, callback)
	wg.Wait()

	if len(receivedMsg.Content) < 5 || receivedMsg.Content[:5] != "Error" {
		t.Errorf("expected error message, got %s", receivedMsg.Content)
	}
}

func TestExecute_WebSearch(t *testing.T) {
	mock := &mockClients{
		searchWebFn: func(ctx context.Context, req types.SearchRequest) (types.SearchResponse, error) {
			return types.SearchResponse{Summary: "搜索结果"}, nil
		},
	}
	exec := executor.New(mock)

	action := protocol.Action{
		Type:   "web_search",
		Params: map[string]string{"query": "测试"},
	}

	sessionCtx := executor.SessionContext{
		UserID:    "u1",
		SessionID: "s1",
	}

	var receivedMsg types.ContextMessage
	var wg sync.WaitGroup
	wg.Add(1)

	callback := func(msg types.ContextMessage) {
		receivedMsg = msg
		wg.Done()
	}

	exec.Execute(action, sessionCtx, callback)
	wg.Wait()

	if receivedMsg.MsgType != "search_result" {
		t.Errorf("expected msgType=search_result, got %s", receivedMsg.MsgType)
	}
}
