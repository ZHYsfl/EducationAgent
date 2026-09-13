// Command server runs the voice PPT assistant backend.
package main

import (
	"context"
	"log"
	"net/http"

	"educationagent/internal/config"
	"educationagent/internal/engineserver"
)

func main() {
	if err := config.LoadFile(".env"); err != nil {
		log.Printf(".env not loaded: %v", err)
	}
	cfg := config.Load()

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	log.Printf("educationagent listening on %s", cfg.ServerAddr)
	log.Printf("deepseek: base_url=%s model=%s", cfg.DeepSeekBaseURL, cfg.DeepSeekModel)
	log.Printf("asr: %s", cfg.ASRBaseURL)
	log.Printf("tts: %s", cfg.TTSBaseURL)
	if cfg.DeepSeekAPIKey == "" {
		log.Println("warning: DEEPSEEK_API_KEY is empty")
	}

	log.Fatal(http.ListenAndServe(cfg.ServerAddr, mux))
}
