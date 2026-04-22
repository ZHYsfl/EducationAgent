package handler

import (
	"educationagent/internal/service"
	"educationagent/internal/state"

	"github.com/gin-gonic/gin"
)

// RegisterRoutes wires all API handlers to the gin engine.
func RegisterRoutes(
	r *gin.Engine,
	st *state.AppState,
	voiceSvc *service.VoiceService,
	pptSvc *service.PPTService,
	kbSvc service.KBService,
	searchSvc service.SearchService,
	asr service.ASRService,
	interrupt service.InterruptService,
	voiceAgent service.VoiceAgentService,
) {
	r.POST("/api/v1/update_requirements", VoiceUpdateRequirements(voiceSvc))
	r.POST("/api/v1/require_confirm", VoiceRequireConfirm(voiceSvc))
	r.POST("/api/v1/send_to_ppt_agent", VoiceSendToPPTAgent(voiceSvc, pptSvc))
	r.GET("/api/v1/fetch_from_ppt_message_queue", VoiceFetchFromPPTQueue(voiceSvc))
	r.POST("/api/v1/start_conversation", StartConversation(st, pptSvc))
	r.POST("/api/v1/send_to_voice_agent", PPTSendToVoiceAgent(pptSvc))
	r.POST("/api/v1/kb/query-chunks", KBQueryChunks(kbSvc))
	r.POST("/api/v1/search/query", SearchQuery(searchSvc))
	r.POST("/api/v1/voice/vad_start", VoiceVADStart(st, asr, interrupt))
	r.POST("/api/v1/voice/vad_end", VoiceVADEnd(st, asr, voiceAgent))
	r.POST("/api/v1/voice/text_input", VoiceTextInput(st, voiceAgent))
	r.GET("/api/v1/ppt/log-stream", PPTLogStream(st))
	r.GET("/api/v1/fs/list", FSList(st))
	r.GET("/api/v1/fs/read", FSRead(st))
	r.GET("/api/v1/fs/download", FSDownload(st))
}
