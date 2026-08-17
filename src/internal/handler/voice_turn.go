package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"educationagent/internal/middleware"
	"educationagent/internal/model"

	"github.com/gin-gonic/gin"
)

// VadStart handles POST /api/v1/voice/vad_start.
func (h *VoiceHandler) VadStart(c *gin.Context) {
	var req model.VadStartRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.Fail(c, http.StatusBadRequest, "invalid request")
		return
	}
	middleware.OK(c, model.VadStartData{Interrupt: true})
}

// VadEnd handles POST /api/v1/voice/vad_end.
func (h *VoiceHandler) VadEnd(c *gin.Context) {
	var req model.VadEndRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.Fail(c, http.StatusBadRequest, "invalid request")
		return
	}

	ctx := c.Request.Context()
	transcript, err := h.asrSvc.Transcribe(ctx, req.Audio, req.Format)
	if err != nil {
		middleware.Fail(c, http.StatusBadRequest, "asr failed")
		return
	}

	h.runTurnStream(c, []string{transcript}, req.NeedsInterruptedPrefix, req.InterruptedAssistantText)
}

// TextInput handles POST /api/v1/voice/text_input.
func (h *VoiceHandler) TextInput(c *gin.Context) {
	var req model.TextInputRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.Fail(c, http.StatusBadRequest, "invalid request")
		return
	}

	h.runTurnStream(c, []string{req.Text}, req.NeedsInterruptedPrefix, req.InterruptedAssistantText)
}

// TtsFinished handles POST /api/v1/voice/tts_finished.
func (h *VoiceHandler) TtsFinished(c *gin.Context) {
	var req model.TtsFinishedRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		middleware.Fail(c, http.StatusBadRequest, "invalid request")
		return
	}
	h.store.MarkTTSDone()
	middleware.OK[any](c, nil)
}

func (h *VoiceHandler) runTurnStream(
	c *gin.Context,
	segments []string,
	needsPrefix bool,
	assistantText string,
) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")

	ctx := c.Request.Context()
	isInterrupt := needsPrefix || assistantText != ""

	if isInterrupt {
		if h.store.IsCurrentTurnActive() {
			// Interrupt of the currently active turn: start a new waiting episode.
			// The current turn will mark the tool-call line done when it finishes.
			h.store.CreateOrResetWaitingEpisode(needsPrefix, assistantText, false)
			for _, seg := range segments {
				h.store.AddSpeechSegment(seg)
			}
		} else if h.store.GetWaitingEpisode() != nil {
			// Extend the existing waiting episode with another speech segment.
			for _, seg := range segments {
				h.store.AddSpeechSegment(seg)
			}
			// The first segment's prefix was already decided by the episode.
		} else {
			// No active turn and no waiting episode; treat as a normal new turn.
			isInterrupt = false
		}
	}

	if isInterrupt {
		we := h.store.WaitForWaitingEpisode(ctx)
		if we == nil {
			return
		}
		segments = we.Segments()
		needsPrefix = we.NeedsInterruptedPrefix()
		assistantText = we.InterruptedAssistantText()
	}

	out := make(chan model.SSEChunk, 64)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				// Ensure the channel is closed on panic so the SSE stream ends.
			}
			close(out)
		}()
		h.store.LockVoiceTurn()
		defer h.store.UnlockVoiceTurn()
		_ = h.voiceAgentSvc.StreamTurn(ctx, h.store, segments, needsPrefix, assistantText, out)
	}()

	c.Stream(func(w io.Writer) bool {
		select {
		case chunk, ok := <-out:
			if !ok {
				writeSSE(w, model.SSEChunk{Type: "turn_end"})
				w.Write([]byte("data: [DONE]\n\n"))
				return false
			}
			writeSSE(w, chunk)
			return true
		case <-ctx.Done():
			w.Write([]byte("data: [DONE]\n\n"))
			return false
		}
	})
}

func writeSSE(w io.Writer, chunk model.SSEChunk) {
	b, _ := json.Marshal(chunk)
	fmt.Fprintf(w, "data: %s\n\n", b)
}
