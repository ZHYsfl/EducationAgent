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

	"auth_memory_service/internal/contract"
	transport "auth_memory_service/internal/http"
	"auth_memory_service/internal/http/handlers"
	"auth_memory_service/internal/http/middleware"
	"auth_memory_service/internal/infra/extractor"
	jwtinfra "auth_memory_service/internal/infra/jwt"
	"auth_memory_service/internal/infra/mailer"
	"auth_memory_service/internal/model"
	"auth_memory_service/internal/repository"
	"auth_memory_service/internal/service"
)

type fakeWorkingStore struct {
	items map[string]model.WorkingMemory
}

func (f *fakeWorkingStore) Save(_ context.Context, wm model.WorkingMemory) error {
	if f.items == nil {
		f.items = map[string]model.WorkingMemory{}
	}
	f.items[wm.SessionID] = wm
	return nil
}

func (f *fakeWorkingStore) Get(_ context.Context, sessionID string) (*model.WorkingMemory, error) {
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

func setupMemoryRouter(t *testing.T) (*gorm.DB, http.Handler, func(), *jwtinfra.TokenManager) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	mustExec(t, db, `CREATE TABLE users (id TEXT PRIMARY KEY, username TEXT UNIQUE, email TEXT UNIQUE, password_hash TEXT, display_name TEXT DEFAULT '', subject TEXT DEFAULT '', school TEXT DEFAULT '', role TEXT DEFAULT 'teacher', created_at INTEGER, updated_at INTEGER);`)
	mustExec(t, db, `CREATE TABLE pending_registrations (user_id TEXT PRIMARY KEY, username TEXT UNIQUE, email TEXT UNIQUE, password_hash TEXT, display_name TEXT DEFAULT '', subject TEXT DEFAULT '', school TEXT DEFAULT '', role TEXT DEFAULT 'teacher', verification_token_hash TEXT UNIQUE, verification_expires_at INTEGER, verification_sent_at INTEGER, created_at INTEGER, updated_at INTEGER);`)
	mustExec(t, db, `CREATE TABLE consumed_verification_tokens (verification_token_hash TEXT PRIMARY KEY, user_id TEXT, consumed_at INTEGER);`)
	mustExec(t, db, `CREATE TABLE issued_verification_tokens (verification_token_hash TEXT PRIMARY KEY, expires_at INTEGER, created_at INTEGER);`)
	mustExec(t, db, `CREATE TABLE sessions (id TEXT PRIMARY KEY, user_id TEXT, title TEXT DEFAULT '', status TEXT DEFAULT 'active', created_at INTEGER, updated_at INTEGER);`)
	mustExec(t, db, `CREATE TABLE memory_entries (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, category TEXT NOT NULL, key TEXT NOT NULL, value TEXT NOT NULL, context TEXT DEFAULT 'general', confidence REAL DEFAULT 1.0, source TEXT DEFAULT 'explicit', source_session_id TEXT, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL);`)
	mustExec(t, db, `CREATE UNIQUE INDEX idx_memory_user_key_context ON memory_entries(user_id, key, context);`)
	mustExec(t, db, `INSERT INTO users (id,username,email,password_hash,display_name,subject,school,role,created_at,updated_at) VALUES ('user_self','self','self@example.com','hash','Self','Math','School','teacher',1710000000000,1710000000000);`)
	mustExec(t, db, `INSERT INTO users (id,username,email,password_hash,display_name,subject,school,role,created_at,updated_at) VALUES ('user_other','other','other@example.com','hash','Other','Physics','School','teacher',1710000000000,1710000000000);`)

	authRepo := repository.NewAuthRepository(db)
	memRepo := repository.NewMemoryRepository(db)
	workingRepo := &fakeWorkingStore{items: map[string]model.WorkingMemory{}}
	tm := jwtinfra.NewTokenManager("test-secret", 24)
	authSvc := service.NewAuthService(authRepo, tm, mailer.NoopMailer{}, 24, "http://localhost:3000/verify-email")
	memSvc := service.NewMemoryService(authRepo, memRepo, workingRepo, extractor.RuleBasedExtractor{})

	authHandler := handlers.NewAuthHandler(authSvc)
	memoryHandler := handlers.NewMemoryHandler(memSvc)
	mw := middleware.NewAuthMiddleware(tm, "internal-key")
	r := transport.NewRouter(authHandler, mw)
	transport.AddMemoryRoutes(r, memoryHandler, mw)
	cleanup := func() {
	}
	return db, r, cleanup, tm
}

func mustExec(t *testing.T, db *gorm.DB, sql string, args ...interface{}) {
	t.Helper()
	if err := db.Exec(sql, args...).Error; err != nil {
		t.Fatalf("exec sql failed: %v", err)
	}
}

func TestMemoryProfileAuthMatrix(t *testing.T) {
	_, router, cleanup, tm := setupMemoryRouter(t)
	defer cleanup()

	token, _, err := tm.Generate("user_self", "self")
	if err != nil {
		t.Fatalf("token: %v", err)
	}

	req1 := httptest.NewRequest(http.MethodGet, "/api/v1/memory/profile/user_self", nil)
	req1.Header.Set("Authorization", "Bearer "+token)
	res1 := httptest.NewRecorder()
	router.ServeHTTP(res1, req1)
	var b1 map[string]interface{}
	_ = json.Unmarshal(res1.Body.Bytes(), &b1)
	if int(b1["code"].(float64)) != contract.CodeSuccess {
		t.Fatalf("bearer self expected success")
	}

	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/memory/profile/user_other", nil)
	req2.Header.Set("Authorization", "Bearer "+token)
	res2 := httptest.NewRecorder()
	router.ServeHTTP(res2, req2)
	var b2 map[string]interface{}
	_ = json.Unmarshal(res2.Body.Bytes(), &b2)
	if int(b2["code"].(float64)) != contract.CodeInvalidCredentials {
		t.Fatalf("bearer mismatched user should be rejected")
	}

	req3 := httptest.NewRequest(http.MethodGet, "/api/v1/memory/profile/user_other", nil)
	req3.Header.Set("X-Internal-Key", "internal-key")
	res3 := httptest.NewRecorder()
	router.ServeHTTP(res3, req3)
	var b3 map[string]interface{}
	_ = json.Unmarshal(res3.Body.Bytes(), &b3)
	if int(b3["code"].(float64)) != contract.CodeSuccess {
		t.Fatalf("internal key access should succeed")
	}
}

func TestMemoryInternalProtectedEndpoints(t *testing.T) {
	_, router, cleanup, _ := setupMemoryRouter(t)
	defer cleanup()

	payload := map[string]interface{}{"user_id": "user_self", "session_id": "sess_x", "messages": []map[string]string{{"role": "user", "content": "I teach math"}}}
	body, _ := json.Marshal(payload)
	req1 := httptest.NewRequest(http.MethodPost, "/api/v1/memory/extract", bytes.NewReader(body))
	req1.Header.Set("Content-Type", "application/json")
	res1 := httptest.NewRecorder()
	router.ServeHTTP(res1, req1)
	var b1 map[string]interface{}
	_ = json.Unmarshal(res1.Body.Bytes(), &b1)
	if int(b1["code"].(float64)) != contract.CodeInvalidCredentials {
		t.Fatalf("missing internal key should fail")
	}

	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/memory/extract", bytes.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("X-Internal-Key", "internal-key")
	res2 := httptest.NewRecorder()
	router.ServeHTTP(res2, req2)
	var b2 map[string]interface{}
	_ = json.Unmarshal(res2.Body.Bytes(), &b2)
	if int(b2["code"].(float64)) != contract.CodeSuccess {
		t.Fatalf("internal key should allow extract")
	}
}
