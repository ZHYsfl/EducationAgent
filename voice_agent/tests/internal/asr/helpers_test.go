package asr_test

import (
	"net/http"
	"net/http/httptest"

	"github.com/gorilla/websocket"
)

var wsUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func wsURL(srv *httptest.Server) string {
	return "ws" + srv.URL[4:]
}
