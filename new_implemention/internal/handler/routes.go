package handler

import (
	"educationagent/internal/service"

	"github.com/gin-gonic/gin"
)

// RegisterRoutes wires all API handlers to the gin engine.
func RegisterRoutes(
	r *gin.Engine,
	voiceSvc *service.VoiceService,
	pptSvc *service.PPTService,
	kbSvc service.KBService,
	searchSvc service.SearchService,
) {
	r.POST("/api/v1/update_requirements", VoiceUpdateRequirements(voiceSvc))
	r.POST("/api/v1/require_confirm", VoiceRequireConfirm(voiceSvc))
	r.POST("/api/v1/send_to_ppt_agent", VoiceSendToPPTAgent(voiceSvc, pptSvc))
	r.GET("/api/v1/fetch_from_ppt_message_queue", VoiceFetchFromPPTQueue(voiceSvc))
	r.POST("/api/v1/start_conversation", StartConversation())
	r.POST("/api/v1/send_to_voice_agent", PPTSendToVoiceAgent(pptSvc))
	r.POST("/api/v1/kb/query-chunks", KBQueryChunks(kbSvc))
	r.POST("/api/v1/search/query", SearchQuery(searchSvc))
}
