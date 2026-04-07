package middleware

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	jwtinfra "memory_service/internal/infra/jwt"
)

func TestMarkDeprecatedLogsAttemptedBearerOnAuthFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tm := jwtinfra.NewTokenManager("test-secret", 24)
	mw := NewAuthMiddleware(tm, "internal-key")

	var logs bytes.Buffer
	orig := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(orig)

	r := gin.New()
	r.Use(mw.RequestMetadata())
	r.GET("/api/v1/memory/profile/:user_id", mw.MarkDeprecated("compatibility"), mw.RequireLegacyProfileAccess("user_id"), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/memory/profile/user_self", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")
	res := httptest.NewRecorder()
	r.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected wrapped 200 response, got %d", res.Code)
	}
	line := logs.String()
	if !strings.Contains(line, "route_class=compatibility") {
		t.Fatalf("missing route class log: %s", line)
	}
	if !strings.Contains(line, "auth_mode=bearer") {
		t.Fatalf("missing attempted bearer auth mode in log: %s", line)
	}
	if !strings.Contains(line, "caller_type=end_user") {
		t.Fatalf("missing attempted caller_type in log: %s", line)
	}
}

func TestMarkDeprecatedLogsAttemptedInternalOnAuthFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tm := jwtinfra.NewTokenManager("test-secret", 24)
	mw := NewAuthMiddleware(tm, "expected-internal-key")

	var logs bytes.Buffer
	orig := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(orig)

	r := gin.New()
	r.Use(mw.RequestMetadata())
	r.POST("/api/v1/memory/extract", mw.MarkDeprecated("compatibility"), mw.RequireInternalService(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/memory/extract", nil)
	req.Header.Set("X-Internal-Key", "wrong-key")
	res := httptest.NewRecorder()
	r.ServeHTTP(res, req)

	if res.Code != http.StatusOK {
		t.Fatalf("expected wrapped 200 response, got %d", res.Code)
	}
	line := logs.String()
	if !strings.Contains(line, "route_class=compatibility") {
		t.Fatalf("missing route class log: %s", line)
	}
	if !strings.Contains(line, "auth_mode=internal") {
		t.Fatalf("missing attempted internal auth mode in log: %s", line)
	}
	if !strings.Contains(line, "caller_type=internal_service") {
		t.Fatalf("missing attempted caller_type in log: %s", line)
	}
}
