package handler

import (
	"encoding/json"
	"io"
	"log"
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

		// Always treat any detected speech as an interrupt so the voice agent
		// responds to every user turn. The VAD already filters out noise via
		// minSpeechDurationMs, so a second LLM-based interrupt check is redundant.
		isInterrupt := true
		log.Printf("vad_start: historyLen=%d transcript=%q", st.VoiceHistoryLen(), transcript)

		c.JSON(http.StatusOK, model.UniformResponse{
			Code:    200,
			Message: "success",
			Data:    model.VADStartData{Interrupt: isInterrupt},
		})
	}
}

// VoiceTextInput handles POST /api/v1/voice/text_input.
// Skips ASR and feeds the text directly into the voice agent stream.
func VoiceTextInput(st *state.AppState, voiceAgent service.VoiceAgentService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req struct {
			Text string `json:"text"`
		}
		if err := c.ShouldBindJSON(&req); err != nil || req.Text == "" {
			c.JSON(http.StatusOK, model.UniformResponse{Code: 400, Message: "invalid request body"})
			return
		}

		st.LockVoiceTurn()
		defer st.UnlockVoiceTurn()

		c.Writer.Header().Set("Content-Type", "text/event-stream")
		c.Writer.Header().Set("Cache-Control", "no-cache")
		c.Writer.Header().Set("Connection", "keep-alive")
		c.Writer.WriteHeader(http.StatusOK)

		out := make(chan model.SSEChunk, 32)
		go func() {
			_ = voiceAgent.StreamTurn(c.Request.Context(), st, req.Text, false, "", out)
		}()

		for chunk := range out {
			data, _ := json.Marshal(chunk)
			_, _ = c.Writer.Write([]byte("data: "))
			_, _ = c.Writer.Write(data)
			_, _ = c.Writer.Write([]byte("\n\n"))
			c.Writer.Flush()
		}
		_, _ = io.WriteString(c.Writer, "data: [DONE]\n\n")
		c.Writer.Flush()
	}
}

// VoiceVADEnd handles POST /api/v1/voice/vad_end.
// It streams the response using Server-Sent Events.
func VoiceVADEnd(st *state.AppState, asr service.ASRService, voiceAgent service.VoiceAgentService) gin.HandlerFunc {
	return func(c *gin.Context) {
		st.LockVoiceTurn()
		defer st.UnlockVoiceTurn()

		var req model.VADEndRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusOK, model.UniformResponse{Code: 400, Message: "invalid request body"})
			return
		}

		// Full ASR — skip if caller provided text directly.
		var transcript string
		var err error
		if req.Text != "" {
			transcript = req.Text
		} else {
			transcript, err = asr.Transcribe(c.Request.Context(), req.Audio)
			log.Printf("vad_end: audioLen=%d transcript=%q err=%v", len(req.Audio), transcript, err)
			if err != nil {
				c.JSON(http.StatusOK, model.UniformResponse{Code: 400, Message: "asr failed"})
				return
			}
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
