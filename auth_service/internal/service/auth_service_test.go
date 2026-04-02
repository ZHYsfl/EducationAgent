package service

import (
	"fmt"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"auth_service/internal/contract"
	jwtinfra "auth_service/internal/infra/jwt"
	"auth_service/internal/model"
	"auth_service/internal/repository"
	"auth_service/internal/util"
)

type mailerMock struct {
	mu      sync.Mutex
	fail    bool
	count   int
	lastURL string
	email   string
}

func (m *mailerMock) SendVerificationEmail(email, verifyURL string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail {
		return gorm.ErrInvalidDB
	}
	m.count++
	m.lastURL = verifyURL
	m.email = email
	return nil
}

func (m *mailerMock) token() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, _ := url.Parse(m.lastURL)
	return u.Query().Get("token")
}

func setupAuthService(t *testing.T) (*AuthService, *repository.AuthRepository, *gorm.DB, *mailerMock) {
	t.Helper()
	db, err := gorm.Open(
		sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared&_busy_timeout=5000", strings.ReplaceAll(t.Name(), "/", "_"))),
		&gorm.Config{Logger: logger.Default.LogMode(logger.Silent)},
	)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	mustExec(t, db, `CREATE TABLE users (
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
);`)
	mustExec(t, db, `CREATE TABLE pending_registrations (
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
);`)
	mustExec(t, db, `CREATE TABLE consumed_verification_tokens (
    verification_token_hash TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    consumed_at INTEGER NOT NULL
);`)
	mustExec(t, db, `CREATE TABLE issued_verification_tokens (
    verification_token_hash TEXT PRIMARY KEY,
    expires_at INTEGER NOT NULL,
    created_at INTEGER NOT NULL
);`)
	repo := repository.NewAuthRepository(db)
	mm := &mailerMock{}
	tm := jwtinfra.NewTokenManager("test-secret", 24)
	svc := NewAuthService(repo, tm, mm, 24, "http://localhost:3000/verify-email")
	return svc, repo, db, mm
}

func TestRegisterSuccessAndHashOnlyToken(t *testing.T) {
	svc, repo, _, mm := setupAuthService(t)
	resp, err := svc.Register(RegisterRequest{
		Username:    "alice",
		Email:       "alice@example.com",
		Password:    "password123",
		DisplayName: "Alice",
		Subject:     "Math",
		School:      "School",
	})
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}
	if !strings.HasPrefix(resp.UserID, "user_") {
		t.Fatalf("user id prefix mismatch: %s", resp.UserID)
	}
	if !resp.VerificationRequired {
		t.Fatalf("verification_required should be true")
	}
	if resp.VerificationExpiresAt < 1_000_000_000_000 {
		t.Fatalf("verification_expires_at should be ms")
	}
	token := mm.token()
	if token == "" {
		t.Fatalf("verification token not sent")
	}
	pending, err := repo.GetPendingByUsername("alice")
	if err != nil {
		t.Fatalf("pending not found: %v", err)
	}
	if pending.VerificationTokenHash == token {
		t.Fatalf("plaintext token must not be stored")
	}
	if pending.VerificationTokenHash != util.HashToken(token) {
		t.Fatalf("stored hash mismatch")
	}
}

func TestRegisterDuplicateAndPendingRetry(t *testing.T) {
	svc, _, _, mm := setupAuthService(t)
	first, err := svc.Register(RegisterRequest{Username: "bob", Email: "bob@example.com", Password: "password123"})
	if err != nil {
		t.Fatalf("first register: %v", err)
	}
	firstToken := mm.token()
	retry, err := svc.Register(RegisterRequest{Username: "bob", Email: "bob@example.com", Password: "password1234"})
	if err != nil {
		t.Fatalf("retry register: %v", err)
	}
	if first.UserID != retry.UserID {
		t.Fatalf("pending retry should keep same user_id")
	}
	if firstToken == mm.token() {
		t.Fatalf("pending retry should refresh verification token")
	}

	_, err = svc.Register(RegisterRequest{Username: "bob", Email: "other@example.com", Password: "password123"})
	se := err.(*ServiceError)
	if se.Code != contract.CodeConflictUsername {
		t.Fatalf("expected 40901, got %d", se.Code)
	}
	_, err = svc.Register(RegisterRequest{Username: "other", Email: "bob@example.com", Password: "password123"})
	se = err.(*ServiceError)
	if se.Code != contract.CodeConflictEmail {
		t.Fatalf("expected 40902, got %d", se.Code)
	}
}

func TestVerifySuccessReplayInvalidExpired(t *testing.T) {
	svc, repo, db, mm := setupAuthService(t)
	reg, err := svc.Register(RegisterRequest{Username: "cindy", Email: "cindy@example.com", Password: "password123"})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	token := mm.token()
	verifyResp, err := svc.Verify(VerifyRequest{Token: token})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if verifyResp.ExpiresAt < 1_000_000_000_000 {
		t.Fatalf("expires_at should be ms")
	}
	if verifyResp.UserID != reg.UserID {
		t.Fatalf("verified user id mismatch")
	}
	if _, err := repo.GetPendingByUsername("cindy"); err == nil {
		t.Fatalf("pending row should be deleted after verify")
	}
	var consumed model.ConsumedVerificationToken
	if err := db.Where("verification_token_hash = ?", util.HashToken(token)).First(&consumed).Error; err != nil {
		t.Fatalf("consumed token row missing: %v", err)
	}
	if consumed.ConsumedAt < 1_000_000_000_000 {
		t.Fatalf("consumed_at should be ms")
	}

	_, err = svc.Verify(VerifyRequest{Token: token})
	if err == nil || err.(*ServiceError).Code != contract.CodeVerificationTokenConsumed {
		t.Fatalf("replay expected 40903")
	}
	_, err = svc.Verify(VerifyRequest{Token: "unknown-token"})
	if err == nil || err.(*ServiceError).Code != contract.CodeInvalidVerificationToken {
		t.Fatalf("invalid expected 40103")
	}

	expiredToken := "expired-token"
	nowMs := util.NowMilli()
	mustExec(t, db, `INSERT INTO pending_registrations (user_id,username,email,password_hash,display_name,subject,school,role,verification_token_hash,verification_expires_at,verification_sent_at,created_at,updated_at)
VALUES ('user_expired','expuser','exp@example.com','hash','','','','teacher',?,?,?, ?, ?);`, util.HashToken(expiredToken), nowMs-1, nowMs-2, nowMs-2, nowMs-2)
	_, err = svc.Verify(VerifyRequest{Token: expiredToken})
	if err == nil || err.(*ServiceError).Code != contract.CodeExpiredVerificationToken {
		t.Fatalf("expired expected 40104")
	}
}

func TestVerifyExpiredIsCleanupIndependent(t *testing.T) {
	svc, _, db, _ := setupAuthService(t)
	token := "cleanup-expired-token"
	hash := util.HashToken(token)
	nowMs := util.NowMilli()
	mustExec(t, db, `INSERT INTO issued_verification_tokens (verification_token_hash, expires_at, created_at) VALUES (?, ?, ?)`, hash, nowMs-1, nowMs-1000)
	_, err := svc.Verify(VerifyRequest{Token: token})
	if err == nil || err.(*ServiceError).Code != contract.CodeExpiredVerificationToken {
		t.Fatalf("expired cleanup-independent token should return 40104")
	}
}

func TestVerifyTransactionRollbackOnConsumedInsertFailure(t *testing.T) {
	svc, repo, db, mm := setupAuthService(t)
	_, err := svc.Register(RegisterRequest{Username: "rollback", Email: "rollback@example.com", Password: "password123"})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	token := mm.token()
	hash := util.HashToken(token)

	mustExec(t, db, `CREATE TRIGGER fail_consumed_insert BEFORE INSERT ON consumed_verification_tokens BEGIN SELECT RAISE(ABORT, 'force consumed insert failure'); END;`)

	_, err = svc.Verify(VerifyRequest{Token: token})
	if err == nil || err.(*ServiceError).Code != contract.CodeInternalError {
		t.Fatalf("expected verify internal error when consumed insert fails")
	}

	if _, err := repo.GetPendingByTokenHash(hash); err != nil {
		t.Fatalf("pending row should remain after rollback: %v", err)
	}
	if _, err := repo.GetUserByUsername("rollback"); err == nil {
		t.Fatalf("user insert should be rolled back")
	}
	consumed, err := repo.IsVerificationTokenConsumed(hash)
	if err != nil {
		t.Fatalf("query consumed row failed: %v", err)
	}
	if consumed {
		t.Fatalf("consumed marker should be rolled back")
	}
}

func mustExec(t *testing.T, db *gorm.DB, sql string, args ...interface{}) {
	t.Helper()
	if err := db.Exec(sql, args...).Error; err != nil {
		t.Fatalf("exec sql failed: %v", err)
	}
}

func TestVerifyConcurrentRace(t *testing.T) {
	svc, _, _, mm := setupAuthService(t)
	_, err := svc.Register(RegisterRequest{Username: "race", Email: "race@example.com", Password: "password123"})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	token := mm.token()

	codes := make(chan int, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := svc.Verify(VerifyRequest{Token: token})
			if err == nil {
				codes <- contract.CodeSuccess
				return
			}
			codes <- err.(*ServiceError).Code
		}()
	}
	wg.Wait()
	close(codes)

	countSuccess := 0
	countConsumed := 0
	for c := range codes {
		if c == contract.CodeSuccess {
			countSuccess++
		}
		if c == contract.CodeVerificationTokenConsumed {
			countConsumed++
		}
	}
	if countSuccess != 1 || countConsumed != 1 {
		t.Fatalf("expected one success and one 40903, got success=%d consumed=%d", countSuccess, countConsumed)
	}
}

func TestLoginByUsernameAndEmailWrongPasswordAndUnverified(t *testing.T) {
	svc, _, _, mm := setupAuthService(t)
	_, err := svc.Register(RegisterRequest{Username: "loginuser", Email: "login@example.com", Password: "password123"})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	_, err = svc.Login(LoginRequest{Username: "loginuser", Password: "password123"})
	if err == nil || err.(*ServiceError).Code != contract.CodeEmailNotVerified {
		t.Fatalf("unverified login expected 40102")
	}
	_, err = svc.Verify(VerifyRequest{Token: mm.token()})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	byUsername, err := svc.Login(LoginRequest{Username: "loginuser", Password: "password123"})
	if err != nil {
		t.Fatalf("login username: %v", err)
	}
	if byUsername.ExpiresAt < 1_000_000_000_000 {
		t.Fatalf("login expires_at should be ms")
	}
	byEmail, err := svc.Login(LoginRequest{Email: "login@example.com", Password: "password123"})
	if err != nil {
		t.Fatalf("login email: %v", err)
	}
	if byEmail.UserID != byUsername.UserID {
		t.Fatalf("login by username/email should return same user")
	}
	_, err = svc.Login(LoginRequest{Username: "loginuser", Password: "wrongpass"})
	if err == nil || err.(*ServiceError).Code != contract.CodeInvalidCredentials {
		t.Fatalf("wrong password expected 40100")
	}
}

func TestJWTClaimNames(t *testing.T) {
	tm := jwtinfra.NewTokenManager("test-secret", 24)
	token, exp, err := tm.Generate("user_1", "alice")
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	uid, uname, gotExp, err := tm.Parse(token)
	if err != nil {
		t.Fatalf("parse token: %v", err)
	}
	if uid != "user_1" || uname != "alice" || gotExp != exp {
		t.Fatalf("unexpected claims parse result")
	}

	expiredTM := jwtinfra.NewTokenManager("test-secret", 0)
	expiredToken, _, err := expiredTM.Generate("user_2", "bob")
	if err != nil {
		t.Fatalf("generate expired token: %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	if _, _, _, err := expiredTM.Parse(expiredToken); err != jwtinfra.ErrTokenExpired {
		t.Fatalf("expected token expired error")
	}
}
