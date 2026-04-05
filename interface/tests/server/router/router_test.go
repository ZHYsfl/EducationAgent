package router_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"multimodal-teaching-agent/internal/server"
)

func TestRouter_FileRouteRequiresSlashParam(t *testing.T) {
	t.Parallel()

	app := &server.App{}
	r := server.SetupRouter(app)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/files/file_1", nil)
	req.Header.Set("Authorization", "Bearer user_1")
	r.ServeHTTP(w, req)

	if w.Code == http.StatusNotFound {
		t.Fatal("route should be mounted with dynamic :file_id path")
	}
}

func TestRouter_GroupMounts(t *testing.T) {
	t.Parallel()

	app := &server.App{}
	r := server.SetupRouter(app)

	cases := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/v1/files/upload"},
		{http.MethodGet, "/api/v1/files/file_1"},
		{http.MethodDelete, "/api/v1/files/file_1"},
		{http.MethodPost, "/api/v1/sessions"},
		{http.MethodGet, "/api/v1/sessions/sess_1"},
		{http.MethodGet, "/api/v1/sessions"},
		{http.MethodPut, "/api/v1/sessions/sess_1"},
		{http.MethodPost, "/api/v1/search/query"},
		{http.MethodGet, "/api/v1/search/results/search_1"},
	}

	for _, tc := range cases {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(tc.method, tc.path, nil)
		req.Header.Set("Authorization", "Bearer user_1")
		r.ServeHTTP(w, req)
		if w.Code == http.StatusNotFound {
			t.Fatalf("route not found: %s %s", tc.method, tc.path)
		}
	}
}
