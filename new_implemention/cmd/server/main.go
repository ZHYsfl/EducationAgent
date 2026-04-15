package main

import (
	"log"

	"educationagent/internal/handler"
	"educationagent/internal/service"
	"educationagent/internal/state"

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

	handler.RegisterRoutes(r, voiceSvc, pptSvc, kbSvc, searchSvc)

	log.Println("Server listening on :8080")
	if err := r.Run(":8080"); err != nil {
		log.Fatalf("failed to run server: %v", err)
	}
}
