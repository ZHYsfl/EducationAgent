package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"educationagent/pptagentgo/internal/deckgen"
)

func (a *App) handleGenerateDeck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method", http.StatusMethodNotAllowed)
		return
	}
	if a.Infer == nil {
		writeErr(w, 50200, "PPTAGENT_* 环境变量未配置")
		return
	}
	var body deckgen.GenerateDeckRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 40001, "invalid json")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 590*time.Second)
	defer cancel()
	resp, _ := deckgen.Generate(ctx, a.Infer, body)
	if resp == nil {
		writeErr(w, 50000, "generate-deck 无响应")
		return
	}
	if !resp.OK {
		writeErr(w, 50210, resp.Error)
		return
	}
	writeOK(w, resp)
}

