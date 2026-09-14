package tests

import (
	"net/http/httptest"
	"strings"
	"testing"

	"educationagent/internal/engineserver"
)

func startWSServer(t *testing.T, app *engineserver.App) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(app.Handler)
	t.Cleanup(srv.Close)
	return srv
}

func wsURLOf(srv *httptest.Server) string {
	return "ws" + strings.TrimPrefix(srv.URL, "http") + engineserver.TTSPath
}

func doneClosed(ch <-chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}
