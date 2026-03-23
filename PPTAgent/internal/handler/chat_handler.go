package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"toolcalling"
)

func (a *App) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method", http.StatusMethodNotAllowed)
		return
	}
	if a.Infer == nil {
		http.Error(w, "PPTAGENT_* not configured", http.StatusServiceUnavailable)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 295*time.Second)
	defer cancel()
	out, err := toolcalling.ChatCompletionForwardJSON(ctx, a.Infer.ToolcallingConfig(), body, a.Infer.DefaultModelName())
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(out)
}

