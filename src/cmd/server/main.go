package main

import (
	"log"

	"educationagent/internal/handler"
	"educationagent/internal/service"
	"educationagent/internal/state"

	_ "agent_runtime"

	"github.com/gin-gonic/gin"
)

func main() {
	gin.SetMode(gin.ReleaseMode)
	store := state.NewAppState()
	svc := service.NewVoiceService(store)
	h := handler.NewVoiceHandler(svc)

	r := gin.New()
	// Recover from panics; no default logger to keep output clean.
	r.Use(gin.Recovery())
	handler.RegisterRoutes(r, h)

	addr := ":8080"
	log.Printf("voice agent server listening on %s", addr)
	if err := r.Run(addr); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
