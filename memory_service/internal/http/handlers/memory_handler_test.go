package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"memory_service/internal/contract"
	transport "memory_service/internal/http"
	"memory_service/internal/http/handlers"
	"memory_service/internal/http/middleware"
	"memory_service/internal/infra/extractor"
	jwtinfra "memory_service/internal/infra/jwt"
	"memory_service/internal/model"
	"memory_service/internal/repository"
	"memory_service/internal/service"
)

type fakeWorkingStore struct {
	items map[string]model.WorkingMemoryRecord
}

func (f *fakeWorkingStore) Save(_ context.Context, wm model.WorkingMemoryRecord) error {
	if f.items == nil {
		f.items = map[string]model.WorkingMemoryRecord{}
	}
	f.items[wm.SessionID] = wm
	return nil
}

func (f *fakeWorkingStore) Get(_ context.Context, sessionID string) (*model.WorkingMemoryRecord, error) {
	if f.items == nil {
		return nil, repository.ErrNotFound
	}
	v, ok := f.items[sessionID]
	if !ok {
		return nil, repository.ErrNotFound
	}
	out := v
	return &out, nil
}

type fakeDispatcher struct {
	recallJobs      []service.RecallJob
	contextPushJobs []service.ContextPushJob
}

func (f *fakeDispatcher) DispatchRecall(_ context.Context, job service.RecallJob) error {
	f.recallJobs = append(f.recallJobs, job)
	return nil
}

func (f *fakeDispatcher) DispatchContextPush(_ context.Context, job service.ContextPushJob) error {
	f.contextPushJobs = append(f.contextPushJobs, job)
	return nil
}

func setupMemoryRouter(t *testing.T) (*gorm.DB, http.Handler, func(), *jwtinfra.TokenManager, *fakeDispatcher) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	mustExec(t, db, `CREATE TABLE users (id TEXT PRIMARY KEY, username TEXT UNIQUE, email TEXT UNIQUE, password_hash TEXT, display_name TEXT DEFAULT '', subject TEXT DEFAULT '', school TEXT DEFAULT '', role TEXT DEFAULT 'teacher', created_at INTEGER, updated_at INTEGER);`)
	mustExec(t, db, `CREATE TABLE sessions (id TEXT PRIMARY KEY, user_id TEXT, title TEXT DEFAULT '', status TEXT DEFAULT 'active', created_at INTEGER, updated_at INTEGER);`)
	mustExec(t, db, `CREATE TABLE memory_entries (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, category TEXT NOT NULL, key TEXT NOT NULL, value TEXT NOT NULL, context TEXT DEFAULT 'general', confidence REAL DEFAULT 1.0, source TEXT DEFAULT 'explicit', source_session_id TEXT, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL);`)
	mustExec(t, db, `CREATE UNIQUE INDEX idx_memory_user_key_context ON memory_entries(user_id, key, context);`)
	mustExec(t, db, `INSERT INTO users (id,username,email,password_hash,display_name,subject,school,role,created_at,updated_at) VALUES ('user_self','self','self@example.com','hash','Self','Math','School','teacher',1710000000000,1710000000000);`)
	mustExec(t, db, `INSERT INTO users (id,username,email,password_hash,display_name,subject,school,role,created_at,updated_at) VALUES ('user_other','other','other@example.com','hash','Other','Physics','School','teacher',1710000000000,1710000000000);`)

	authRepo := repository.NewAuthRepository(db)
	memRepo := repository.NewMemoryRepository(db)
	workingRepo := &fakeWorkingStore{items: map[string]model.WorkingMemoryRecord{}}
	tm := jwtinfra.NewTokenManager("test-secret", 24)
	memSvc := service.NewMemoryService(authRepo, memRepo, workingRepo, extractor.NewHybridExtractor(extractor.Config{}, nil))
	dispatcher := &fakeDispatcher{}
	memSvc.SetAsyncDispatcher(dispatcher)
	memSvc.SetArchiveIndexer(service.NoopArchiveIndexer{})

	memoryHandler := handlers.NewMemoryHandler(memSvc)
	mw := middleware.NewAuthMiddleware(tm, "internal-key")
	r := transport.NewRouter(memoryHandler, mw)
	cleanup := func() {}
	return db, r, cleanup, tm, dispatcher
}

func mustExec(t *testing.T, db *gorm.DB, sql string, args ...interface{}) {
	t.Helper()
	if err := db.Exec(sql, args...).Error; err != nil {
		t.Fatalf("exec sql failed: %v", err)
	}
}

func decodeBody(t *testing.T, body *bytes.Buffer) map[string]interface{} {
	t.Helper()
	var out map[string]interface{}
	if err := json.Unmarshal(body.Bytes(), &out); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	return out
}

func TestCanonicalRecallAcceptedResponseAndDispatch(t *testing.T) {
	_, router, cleanup, _, dispatcher := setupMemoryRouter(t)
	defer cleanup()

	payload := map[string]interface{}{
		"user_id":    "user_self",
		"session_id": "sess_async",
		"query":      "teaching style preference",
		"top_k":      5,
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/memory/recall", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Key", "internal-key")
	res := httptest.NewRecorder()

	router.ServeHTTP(res, req)

	resp := decodeBody(t, res.Body)
	if int(resp["code"].(float64)) != contract.CodeSuccess {
		t.Fatalf("expected success, got %#v", resp)
	}
	data := resp["data"].(map[string]interface{})
	if accepted := data["accepted"].(bool); !accepted {
		t.Fatalf("expected accepted=true")
	}
	if len(dispatcher.recallJobs) != 1 {
		t.Fatalf("expected one recall job, got %d", len(dispatcher.recallJobs))
	}
	job := dispatcher.recallJobs[0]
	if job.UserID != "user_self" || job.SessionID != "sess_async" || job.Query != "teaching style preference" || job.TopK != 5 {
		t.Fatalf("unexpected recall job: %#v", job)
	}
	if res.Header().Get("X-Request-ID") == "" {
		t.Fatalf("expected request id header")
	}
}

func TestCanonicalContextPushAcceptedResponseAndDispatch(t *testing.T) {
	_, router, cleanup, _, dispatcher := setupMemoryRouter(t)
	defer cleanup()

	payload := map[string]interface{}{
		"user_id":    "user_self",
		"session_id": "sess_push",
		"messages": []map[string]string{
			{"role": "user", "content": "I want a lesson on derivatives."},
			{"role": "assistant", "content": "Which grade level should it target?"},
		},
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/memory/context/push", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Key", "internal-key")
	res := httptest.NewRecorder()

	router.ServeHTTP(res, req)

	resp := decodeBody(t, res.Body)
	if int(resp["code"].(float64)) != contract.CodeSuccess {
		t.Fatalf("expected success, got %#v", resp)
	}
	data := resp["data"].(map[string]interface{})
	if accepted := data["accepted"].(bool); !accepted {
		t.Fatalf("expected accepted=true")
	}
	if int(data["message_count"].(float64)) != 2 {
		t.Fatalf("expected message_count=2, got %#v", data)
	}
	if len(dispatcher.contextPushJobs) != 1 {
		t.Fatalf("expected one context push job, got %d", len(dispatcher.contextPushJobs))
	}
	job := dispatcher.contextPushJobs[0]
	if job.UserID != "user_self" || job.SessionID != "sess_push" || len(job.Messages) != 2 {
		t.Fatalf("unexpected context push job: %#v", job)
	}
}

func TestDeprecatedProfileCompatibilityAuthMatrix(t *testing.T) {
	_, router, cleanup, tm, _ := setupMemoryRouter(t)
	defer cleanup()

	token, _, err := tm.Generate("user_self", "self")
	if err != nil {
		t.Fatalf("token: %v", err)
	}

	req1 := httptest.NewRequest(http.MethodGet, "/api/v1/memory/profile/user_self", nil)
	req1.Header.Set("Authorization", "Bearer "+token)
	res1 := httptest.NewRecorder()
	router.ServeHTTP(res1, req1)
	body1 := decodeBody(t, res1.Body)
	if int(body1["code"].(float64)) != contract.CodeSuccess {
		t.Fatalf("bearer self expected success")
	}
	if res1.Header().Get("Deprecation") != "true" {
		t.Fatalf("expected deprecation header on compatibility route")
	}

	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/memory/profile/user_other", nil)
	req2.Header.Set("Authorization", "Bearer "+token)
	res2 := httptest.NewRecorder()
	router.ServeHTTP(res2, req2)
	body2 := decodeBody(t, res2.Body)
	if int(body2["code"].(float64)) != contract.CodeInvalidCredentials {
		t.Fatalf("bearer mismatched user should be rejected")
	}

	req3 := httptest.NewRequest(http.MethodGet, "/api/v1/memory/profile/user_other", nil)
	req3.Header.Set("X-Internal-Key", "internal-key")
	res3 := httptest.NewRecorder()
	router.ServeHTTP(res3, req3)
	body3 := decodeBody(t, res3.Body)
	if int(body3["code"].(float64)) != contract.CodeSuccess {
		t.Fatalf("internal key access should succeed")
	}
}

func TestDeprecatedExtractAndWorkingRemainInternalOnly(t *testing.T) {
	_, router, cleanup, _, _ := setupMemoryRouter(t)
	defer cleanup()

	body, _ := json.Marshal(map[string]interface{}{
		"user_id":    "user_self",
		"session_id": "sess_x",
		"messages": []map[string]string{
			{"role": "user", "content": "I teach math"},
		},
	})

	req1 := httptest.NewRequest(http.MethodPost, "/api/v1/memory/extract", bytes.NewReader(body))
	req1.Header.Set("Content-Type", "application/json")
	res1 := httptest.NewRecorder()
	router.ServeHTTP(res1, req1)
	resp1 := decodeBody(t, res1.Body)
	if int(resp1["code"].(float64)) != contract.CodeInvalidCredentials {
		t.Fatalf("missing internal key should fail")
	}
	if res1.Header().Get("Deprecation") != "true" {
		t.Fatalf("expected deprecation header")
	}

	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/memory/working/sess_x", nil)
	req2.Header.Set("Authorization", "Bearer not-allowed")
	res2 := httptest.NewRecorder()
	router.ServeHTTP(res2, req2)
	resp2 := decodeBody(t, res2.Body)
	if int(resp2["code"].(float64)) != contract.CodeInvalidCredentials {
		t.Fatalf("working route should remain internal-only")
	}
}

func TestInternalRecallSyncReturnsDetailedPayload(t *testing.T) {
	db, router, cleanup, _, _ := setupMemoryRouter(t)
	defer cleanup()
	mustExec(t, db, `INSERT INTO memory_entries (id,user_id,category,key,value,context,confidence,source,created_at,updated_at) VALUES ('mem_1','user_self','preference','teaching_style','interactive','general',1.0,'explicit',1710000000000,1710000000000);`)

	body, _ := json.Marshal(map[string]interface{}{
		"user_id":    "user_self",
		"session_id": "sess_sync",
		"query":      "teaching style",
		"top_k":      3,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/internal/memory/recall/sync", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Key", "internal-key")
	res := httptest.NewRecorder()

	router.ServeHTTP(res, req)

	resp := decodeBody(t, res.Body)
	if int(resp["code"].(float64)) != contract.CodeSuccess {
		t.Fatalf("expected success, got %#v", resp)
	}
	data := resp["data"].(map[string]interface{})
	if _, ok := data["profile_summary"]; !ok {
		t.Fatalf("expected sync recall payload, got %#v", data)
	}
	if _, ok := data["preferences"]; !ok {
		t.Fatalf("expected preferences field in sync recall payload")
	}
}
