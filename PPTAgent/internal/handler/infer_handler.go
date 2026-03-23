package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"educationagent/pptagentgo/pkg/infer"
)

func (a *App) handleInfer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method", http.StatusMethodNotAllowed)
		return
	}
	if a.Infer == nil {
		writeErr(w, 50200, "PPTAGENT_* 环境变量未配置")
		return
	}
	var body InferRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, 40001, "invalid json")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 295*time.Second)
	defer cancel()
	out, err := a.Infer.Complete(ctx, body.Prompt, body.SystemMessage, &infer.Options{
		JSONMode:       body.ReturnJSON,
		Temperature:    body.Temperature,
		MaxTokens:      body.MaxTokens,
		ImageURLs:      body.ImageURLs,
		ImageDetail:    body.ImageDetail,
		ModelOverride:  body.Model,
		UseVisionModel: body.UseVisionModel,
	})
	if err != nil {
		writeErr(w, 50210, err.Error())
		return
	}
	writeOK(w, InferResponse{OK: true, Content: out})
}

