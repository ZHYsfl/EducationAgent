package handler

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"educationagent/internal/model"
	"educationagent/internal/service"
	"educationagent/internal/state"
	"educationagent/internal/toolcalling"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVADStartInterruptTrue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	st := state.NewAppState()

	asr := &service.StubASRService{
		Override: func(string) string { return "stop" },
	}
	interrupt := &mockInterruptService{result: true}

	r := gin.New()
	r.POST("/api/v1/voice/vad_start", VoiceVADStart(st, asr, interrupt))

	body := map[string]any{"audio": "dummy", "format": "pcm"}
	b, _ := json.Marshal(body)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/voice/vad_start", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, float64(200), resp["code"])
	data := resp["data"].(map[string]any)
	assert.True(t, data["interrupt"].(bool))

	cached, ok := st.GetLastVADInterrupt()
	assert.True(t, ok)
	assert.True(t, cached)
}

func TestVADStartInterruptFalse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	st := state.NewAppState()

	asr := &service.StubASRService{
		Override: func(string) string { return "noise" },
	}
	interrupt := &mockInterruptService{result: false}

	r := gin.New()
	r.POST("/api/v1/voice/vad_start", VoiceVADStart(st, asr, interrupt))

	body := map[string]any{"audio": "dummy", "format": "pcm"}
	b, _ := json.Marshal(body)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/voice/vad_start", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, float64(200), resp["code"])
	data := resp["data"].(map[string]any)
	assert.False(t, data["interrupt"].(bool))
}

func TestVADEndWithoutPriorStartFails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	st := state.NewAppState()
	asr := service.NewASRService()
	voiceAgent := service.NewVoiceAgentService(toolcalling.LLMConfig{})

	r := gin.New()
	r.POST("/api/v1/voice/vad_end", VoiceVADEnd(st, asr, voiceAgent))

	body := map[string]any{"audio": "dummy", "format": "pcm"}
	b, _ := json.Marshal(body)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/voice/vad_end", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, float64(400), resp["code"])
}

func TestVADEndIgnoredWhenInterruptFalse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	st := state.NewAppState()
	st.SetLastVADInterrupt(false)

	asr := service.NewASRService()
	voiceAgent := service.NewVoiceAgentService(toolcalling.LLMConfig{})

	r := gin.New()
	r.POST("/api/v1/voice/vad_end", VoiceVADEnd(st, asr, voiceAgent))

	body := map[string]any{"audio": "dummy", "format": "pcm"}
	b, _ := json.Marshal(body)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/voice/vad_end", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, float64(200), resp["code"])
	data := resp["data"].(map[string]any)
	assert.True(t, data["ignored"].(bool))
}

func TestVADEndStreamsWhenInterruptTrue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	st := state.NewAppState()
	st.SetLastVADInterrupt(true)

	asr := &service.StubASRService{
		Override: func(string) string { return "make it bigger" },
	}

	voiceAgent := &mockVoiceAgentService{
		chunks: []model.SSEChunk{
			{Type: "tts", Text: "ok"},
			{Type: "action", Payload: "send_to_ppt_agent|data:make it bigger"},
			{Type: "turn_end"},
		},
	}

	r := gin.New()
	r.POST("/api/v1/voice/vad_end", VoiceVADEnd(st, asr, voiceAgent))

	body := map[string]any{"audio": "dummy", "format": "pcm"}
	b, _ := json.Marshal(body)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/voice/vad_end", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.Equal(t, "text/event-stream", w.Header().Get("Content-Type"))

	scanner := bufio.NewScanner(w.Body)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	// We expect SSE lines: data: {...}  and blank lines.
	var foundTTS, foundAction, foundTurnEnd bool
	for _, line := range lines {
		if strings.HasPrefix(line, "data: ") {
			payload := strings.TrimPrefix(line, "data: ")
			if payload == "[DONE]" {
				continue
			}
			var chunk model.SSEChunk
			if err := json.Unmarshal([]byte(payload), &chunk); err == nil {
				switch chunk.Type {
				case "tts":
					foundTTS = true
				case "action":
					foundAction = true
				case "turn_end":
					foundTurnEnd = true
				}
			}
		}
	}
	assert.True(t, foundTTS, "expected tts chunk")
	assert.True(t, foundAction, "expected action chunk")
	assert.True(t, foundTurnEnd, "expected turn_end chunk")
}

type mockInterruptService struct {
	result bool
	err    error
}

func (m *mockInterruptService) Check(ctx context.Context, transcript string) (bool, error) {
	return m.result, m.err
}

type mockVoiceAgentService struct {
	chunks []model.SSEChunk
}

func (m *mockVoiceAgentService) StreamTurn(ctx context.Context, st *state.AppState, transcript string, out chan<- model.SSEChunk) error {
	for _, c := range m.chunks {
		select {
		case out <- c:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	close(out)
	return nil
}
