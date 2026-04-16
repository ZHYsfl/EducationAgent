package handler

import (
	"encoding/json"
	"io"
	"net/http"

	"educationagent/internal/model"
	"educationagent/internal/service"
	"educationagent/internal/state"

	"github.com/gin-gonic/gin"
)

// VoiceVADStart handles POST /api/v1/voice/vad_start.
func VoiceVADStart(st *state.AppState, asr service.ASRService, interrupt service.InterruptService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req model.VADStartRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusOK, model.UniformResponse{Code: 400, Message: "invalid request body"})
			return
		}

		transcript, err := asr.Transcribe(c.Request.Context(), req.Audio)
		if err != nil {
			c.JSON(http.StatusOK, model.UniformResponse{Code: 400, Message: "asr failed"})
			return
		}

		isInterrupt, err := interrupt.Check(c.Request.Context(), transcript)
		if err != nil {
			c.JSON(http.StatusOK, model.UniformResponse{Code: 400, Message: "interrupt check failed"})
			return
		}

		st.SetLastVADInterrupt(isInterrupt)
		c.JSON(http.StatusOK, model.UniformResponse{
			Code:    200,
			Message: "success",
			Data:    model.VADStartData{Interrupt: isInterrupt},
		})
	}
}

// VoiceVADEnd handles POST /api/v1/voice/vad_end.
// It streams the response using Server-Sent Events.
func VoiceVADEnd(st *state.AppState, asr service.ASRService, voiceAgent service.VoiceAgentService) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Ensure only one voice turn (including its full synchronous action sequence)
		// runs at a time. If a previous turn is still executing actions, wait here.
		st.LockVoiceTurn()
		defer st.UnlockVoiceTurn()

		var req model.VADEndRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusOK, model.UniformResponse{Code: 400, Message: "invalid request body"})
			return
		}

		// Look up cached interrupt result.
		wasInterrupt, ok := st.GetLastVADInterrupt()
		if !ok {
			c.JSON(http.StatusOK, model.UniformResponse{Code: 400, Message: "no prior vad_start found"})
			return
		}

		if !wasInterrupt {
			c.JSON(http.StatusOK, model.UniformResponse{
				Code:    200,
				Message: "success",
				Data:    model.VADEndIgnoredData{Ignored: true},
			})
			return
		}

		// Full ASR.
		transcript, err := asr.Transcribe(c.Request.Context(), req.Audio)
		if err != nil {
			c.JSON(http.StatusOK, model.UniformResponse{Code: 400, Message: "asr failed"})
			return
		}

		// Stream voice agent response via SSE.
		c.Writer.Header().Set("Content-Type", "text/event-stream")
		c.Writer.Header().Set("Cache-Control", "no-cache")
		c.Writer.Header().Set("Connection", "keep-alive")
		c.Writer.WriteHeader(http.StatusOK)

		out := make(chan model.SSEChunk, 32)
		go func() {
			_ = voiceAgent.StreamTurn(c.Request.Context(), st, transcript, req.NeedsInterruptedPrefix, req.InterruptedAssistantText, out)
		}()

		for chunk := range out {
			data, _ := json.Marshal(chunk)
			_, _ = c.Writer.Write([]byte("data: "))
			_, _ = c.Writer.Write(data)
			_, _ = c.Writer.Write([]byte("\n\n"))
			c.Writer.Flush()
		}
		// SSE spec recommends sending a final comment or empty data line.
		_, _ = io.WriteString(c.Writer, "data: [DONE]\n\n")
		c.Writer.Flush()
	}
}
