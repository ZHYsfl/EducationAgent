package handlers_test

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"multimodal-teaching-agent/internal/server"
)

func TestHandlers_BasicValidationBranches(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	app := server.NewAppForTest(nil, nil)
	r := gin.New()

	r.POST("/upload", app.UploadFile)
	r.GET("/file/:file_id", app.GetFile)
	r.DELETE("/file/:file_id", app.DeleteFile)
	r.POST("/sessions", app.CreateSession)
	r.GET("/sessions/:session_id", app.GetSession)
	r.GET("/sessions", app.ListSessions)
	r.PUT("/sessions/:session_id", app.UpdateSession)
	r.POST("/search/query", app.SearchQuery)
	r.GET("/search/results/:request_id", app.SearchResult)

	// upload missing file
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/upload", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("upload missing file expected 200 wrapper, got %d", w.Code)
	}

	// upload invalid purpose with file and user_id context
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "a.txt")
	_, _ = fw.Write([]byte("abc"))
	_ = mw.WriteField("purpose", "invalid")
	_ = mw.Close()

	r2 := gin.New()
	r2.POST("/upload", func(c *gin.Context) {
		c.Set("user_id", "user_1")
		app.UploadFile(c)
	})
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/upload", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	r2.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("upload invalid purpose expected 200 wrapper, got %d", w.Code)
	}

	// invalid IDs and params should return before touching db
	for _, tc := range []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/file/bad", ""},
		{http.MethodDelete, "/file/bad", ""},
		{http.MethodPost, "/sessions", `{"user_id":"bad"}`},
		{http.MethodGet, "/sessions/bad", ""},
		{http.MethodGet, "/sessions", ""},
		{http.MethodPut, "/sessions/bad", `{"status":"active"}`},
		{http.MethodPost, "/search/query", `{"user_id":"bad","query":"x"}`},
		{http.MethodGet, "/search/results/bad", ""},
	} {
		w = httptest.NewRecorder()
		req = httptest.NewRequest(tc.method, tc.path, bytes.NewBufferString(tc.body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("%s %s expected 200 wrapper, got %d", tc.method, tc.path, w.Code)
		}
	}
}
