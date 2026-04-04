package handler

import (
	"encoding/json"
	"net/http"

	"educationagent/pptagentgo/pkg/infer"
)

type App struct {
	Infer *infer.Client
}

func (a *App) Router() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "service": "pptagent_go_infer"})
	})
	mux.HandleFunc("/api/v1/infer", a.handleInfer)
	mux.HandleFunc("/api/v1/generate-deck", a.handleGenerateDeck)
	mux.HandleFunc("/api/v1/generate-pres", a.handleGeneratePres)
	mux.HandleFunc("/v1/chat/completions", a.handleChatCompletions)
	return mux
}

