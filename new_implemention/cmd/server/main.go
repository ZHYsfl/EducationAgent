package main

import (
	"log"
	"os"

	"educationagent/internal/handler"
	"educationagent/internal/service"
	"educationagent/internal/state"
	"educationagent/internal/toolcalling"

	"github.com/gin-gonic/gin"
)

func main() {
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	appState := state.NewAppState()

	voiceSvc := service.NewVoiceService(appState)
	kbSvc := service.NewKBService()
	searchSvc := service.NewSearchService()
	pptSvc := service.NewPPTService(appState, nil, kbSvc, searchSvc)
	asrSvc := service.NewASRService()
	// Interrupt-check LLM and Voice-Agent LLM are local fine-tuned models.
	// They expose an OpenAI-compatible HTTP API (e.g. vLLM / llama.cpp server).
	interruptSvc := service.NewInterruptService(toolcalling.LLMConfig{
		APIKey:  os.Getenv("INTERRUPT_LLM_API_KEY"),
		Model:   os.Getenv("INTERRUPT_LLM_MODEL"),
		BaseURL: os.Getenv("INTERRUPT_LLM_BASE_URL"),
	})
	voiceAgentSvc := service.NewVoiceAgentService(toolcalling.LLMConfig{
		APIKey:  os.Getenv("VOICE_LLM_API_KEY"),
		Model:   os.Getenv("VOICE_LLM_MODEL"),
		BaseURL: os.Getenv("VOICE_LLM_BASE_URL"),
	})

	handler.RegisterRoutes(r, appState, voiceSvc, pptSvc, kbSvc, searchSvc, asrSvc, interruptSvc, voiceAgentSvc)

	log.Println("Server listening on :8080")
	if err := r.Run(":8080"); err != nil {
		log.Fatalf("failed to run server: %v", err)
	}
}
