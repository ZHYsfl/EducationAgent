package main

import (
	"log"
	"os"

	"agent_runtime"
	"educationagent/internal/handler"
	"educationagent/internal/service"
	"educationagent/internal/state"
	"educationagent/internal/voiceagent"

	"github.com/gin-gonic/gin"
)

func main() {
	gin.SetMode(gin.ReleaseMode)
	store := state.NewAppState()
	svc := service.NewVoiceService(store)

	exec := voiceagent.NewExecutor(svc)
	asrSvc := service.ASRService(&service.StubASR{})

	voiceCfg := agent_runtime.LLMConfig{
		APIKey:  os.Getenv("VOICE_LLM_API_KEY"),
		BaseURL: os.Getenv("VOICE_LLM_BASE_URL"),
		Model:   os.Getenv("VOICE_LLM_MODEL"),
	}
	voiceAgentSvc := service.NewVoiceAgentService(
		voiceCfg,
		exec,
		agent_runtime.WithMemoryMode(agent_runtime.MemoryModeSlideWindow, 20),
	)

	h := handler.NewVoiceHandler(svc, voiceAgentSvc, asrSvc)

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
