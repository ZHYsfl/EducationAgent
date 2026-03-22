package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"multimodal-teaching-agent/internal/server"
)

func TestAuthUserMiddleware_Unauthorized(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(server.AuthUserMiddleware())
	r.GET("/x", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected wrapped status 200, got %d", w.Code)
	}
	if body := w.Body.String(); body == "" || body == "null" {
		t.Fatal("expected error response body")
	}
}

func TestAuthUserMiddleware_Authorized(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(server.AuthUserMiddleware())
	r.GET("/x", func(c *gin.Context) {
		if v, ok := c.Get("user_id"); !ok || v.(string) != "user_1" {
			c.Status(http.StatusBadRequest)
			return
		}
		c.Status(http.StatusNoContent)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Authorization", "Bearer user_1")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
}
