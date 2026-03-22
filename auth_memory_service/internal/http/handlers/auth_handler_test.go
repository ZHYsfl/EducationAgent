package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"auth_memory_service/internal/contract"
	transport "auth_memory_service/internal/http"
	"auth_memory_service/internal/http/handlers"
	"auth_memory_service/internal/http/middleware"
	jwtinfra "auth_memory_service/internal/infra/jwt"
	"auth_memory_service/internal/infra/mailer"
	"auth_memory_service/internal/repository"
	"auth_memory_service/internal/service"
)

func TestProfileFromBearerToken(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:profile_test?mode=memory&cache=shared&_busy_timeout=5000"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Exec(`CREATE TABLE users (
    id TEXT PRIMARY KEY,
    username TEXT NOT NULL UNIQUE,
    email TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    display_name TEXT DEFAULT '',
    subject TEXT DEFAULT '',
    school TEXT DEFAULT '',
    role TEXT DEFAULT 'teacher',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);`).Error; err != nil {
		t.Fatalf("create users: %v", err)
	}
	if err := db.Exec(`CREATE TABLE pending_registrations (
    user_id TEXT PRIMARY KEY,
    username TEXT NOT NULL UNIQUE,
    email TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    display_name TEXT DEFAULT '',
    subject TEXT DEFAULT '',
    school TEXT DEFAULT '',
    role TEXT DEFAULT 'teacher',
    verification_token_hash TEXT NOT NULL UNIQUE,
    verification_expires_at INTEGER NOT NULL,
    verification_sent_at INTEGER NOT NULL,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);`).Error; err != nil {
		t.Fatalf("create pending: %v", err)
	}
	if err := db.Exec(`CREATE TABLE consumed_verification_tokens (
    verification_token_hash TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    consumed_at INTEGER NOT NULL
);`).Error; err != nil {
		t.Fatalf("create consumed: %v", err)
	}
	if err := db.Exec(`INSERT INTO users (id,username,email,password_hash,display_name,subject,school,role,created_at,updated_at)
VALUES ('user_abc','alice','alice@example.com','hash','Alice','Math','School','teacher',1710000000000,1710000000000);`).Error; err != nil {
		t.Fatalf("insert user: %v", err)
	}

	tm := jwtinfra.NewTokenManager("test-secret", 24)
	token, _, err := tm.Generate("user_abc", "alice")
	if err != nil {
		t.Fatalf("token generate: %v", err)
	}
	repo := repository.NewAuthRepository(db)
	svc := service.NewAuthService(repo, tm, mailer.NoopMailer{}, 24, "http://localhost:3000/verify-email")
	h := handlers.NewAuthHandler(svc)
	mw := middleware.NewAuthMiddleware(tm, "internal-key")
	r := transport.NewRouter(h, mw)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/profile", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("unexpected http status: %d", w.Code)
	}

	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("json unmarshal: %v", err)
	}
	if int(body["code"].(float64)) != contract.CodeSuccess {
		t.Fatalf("unexpected business code: %v", body["code"])
	}
	data := body["data"].(map[string]interface{})
	if data["user_id"].(string) != "user_abc" {
		t.Fatalf("unexpected profile user_id")
	}
	if _, ok := data["password_hash"]; ok {
		t.Fatalf("profile must not include password_hash")
	}
}
