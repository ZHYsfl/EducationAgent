package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/openai/openai-go/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"educationagent/internal/handler"
	"educationagent/internal/model"
	"educationagent/internal/service"
	"educationagent/internal/state"
	"educationagent/internal/toolcalling"
)

func setupIntegrationServer() (*gin.Engine, *state.AppState, *service.PPTService) {
	gin.SetMode(gin.TestMode)
	st := state.NewAppState()
	voiceSvc := service.NewVoiceService(st)
	agent := toolcalling.NewAgent(toolcalling.LLMConfig{})
	pptSvc := service.NewPPTService(st, agent, service.NewKBService(), service.NewSearchService())
	asrSvc := service.NewASRService()
	interruptSvc := service.NewInterruptService(toolcalling.LLMConfig{})
	voiceAgentSvc := service.NewVoiceAgentService(toolcalling.LLMConfig{}, nil)
	r := gin.New()
	handler.RegisterRoutes(r, st, voiceSvc, pptSvc, service.NewKBService(), service.NewSearchService(), asrSvc, interruptSvc, voiceAgentSvc)
	return r, st, pptSvc
}

func setupVADIntegrationServer() (*gin.Engine, *state.AppState, *service.StubASRService, *mockVoiceAgentSvc) {
	gin.SetMode(gin.TestMode)
	st := state.NewAppState()
	voiceSvc := service.NewVoiceService(st)
	agent := toolcalling.NewAgent(toolcalling.LLMConfig{})
	pptSvc := service.NewPPTService(st, agent, service.NewKBService(), service.NewSearchService())
	asrSvc := &service.StubASRService{}
	interruptSvc := &mockInterruptCheckSvc{result: true}
	voiceAgentSvc := &mockVoiceAgentSvc{}
	r := gin.New()
	handler.RegisterRoutes(r, st, voiceSvc, pptSvc, service.NewKBService(), service.NewSearchService(), asrSvc, interruptSvc, voiceAgentSvc)
	return r, st, asrSvc, voiceAgentSvc
}

type mockInterruptCheckSvc struct {
	result bool
	err    error
}

func (m *mockInterruptCheckSvc) Check(ctx context.Context, transcript string) (bool, error) {
	return m.result, m.err
}

type mockVoiceAgentSvc struct {
	chunks []model.SSEChunk
}

func (m *mockVoiceAgentSvc) StreamTurn(ctx context.Context, st *state.AppState, transcript string, out chan<- model.SSEChunk) error {
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

func doPost(t *testing.T, r *gin.Engine, path string, body any) *httptest.ResponseRecorder {
	b, err := json.Marshal(body)
	require.NoError(t, err)
	w := httptest.NewRecorder()
	req, err := http.NewRequest("POST", path, bytes.NewReader(b))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

func doGet(t *testing.T, r *gin.Engine, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req, err := http.NewRequest("GET", path, nil)
	require.NoError(t, err)
	r.ServeHTTP(w, req)
	return w
}

func parseResp(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	var resp map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	return resp
}

func TestFullConversationFlow(t *testing.T) {
	r, st, pptSvc := setupIntegrationServer()

	// 1. start conversation
	w := doPost(t, r, "/api/v1/start_conversation", map[string]any{
		"from": "frontend",
		"to":   "voice_agent",
	})
	assert.Equal(t, 200, w.Code)
	resp := parseResp(t, w)
	assert.Equal(t, float64(200), resp["code"])

	// 2. partial update requirements
	w = doPost(t, r, "/api/v1/update_requirements", map[string]any{
		"from": "frontend",
		"to":   "voice_agent",
		"requirements": map[string]any{
			"topic": "math",
			"style": "simple and elegant",
		},
	})
	assert.Equal(t, 200, w.Code)
	resp = parseResp(t, w)
	data := resp["data"].(map[string]any)
	missing := data["missing_fields"].([]any)
	assert.Equal(t, []any{"total_pages", "audience"}, missing)

	// 3. complete update requirements
	w = doPost(t, r, "/api/v1/update_requirements", map[string]any{
		"from": "voice_agent",
		"to":   "frontend",
		"requirements": map[string]any{
			"total_pages": 15,
			"audience":    "middle school students",
		},
	})
	assert.Equal(t, 200, w.Code)
	resp = parseResp(t, w)
	data = resp["data"].(map[string]any)
	require.Nil(t, data["missing_fields"])

	// 4. require confirm
	w = doPost(t, r, "/api/v1/require_confirm", map[string]any{
		"from": "voice_agent",
		"to":   "frontend",
		"requirements": map[string]any{
			"topic":       "math",
			"style":       "simple and elegant",
			"total_pages": 15,
			"audience":    "middle school students",
		},
	})
	assert.Equal(t, 200, w.Code)
	resp = parseResp(t, w)
	assert.Equal(t, float64(200), resp["code"])

	// 5. send to ppt agent (first time) – should start runtime
	// Fake chat loop enqueues a message and then blocks so the runtime stays alive.
	firstBlocker := make(chan struct{})
	pptSvc.SetRunChatFn(func(ctx context.Context, history []openai.ChatCompletionMessageParamUnion) ([]openai.ChatCompletionMessageParamUnion, error) {
		st.SendToVoiceAgent("the new version of the ppt is generated successfully")
		<-firstBlocker
		return history, nil
	})

	w = doPost(t, r, "/api/v1/send_to_ppt_agent", map[string]any{
		"from": "voice_agent",
		"to":   "ppt_agent",
		"data": "user's requirements are: topic: math, style: simple and elegant, total_pages: 15, audience: middle school students",
	})
	assert.Equal(t, 200, w.Code)
	resp = parseResp(t, w)
	assert.Equal(t, float64(200), resp["code"])

	// wait for runtime to start and send message
	require.Eventually(t, func() bool {
		return pptSvc.IsRuntimeRunning()
	}, time.Second, 10*time.Millisecond)

	// 6. fetch from ppt message queue
	w = doGet(t, r, "/api/v1/fetch_from_ppt_message_queue")
	assert.Equal(t, 200, w.Code)
	resp = parseResp(t, w)
	assert.Equal(t, float64(200), resp["code"])
	assert.Equal(t, "the new version of the ppt is generated successfully", resp["data"])

	// 7. voice agent sends feedback while runtime is running.
	// In the new architecture OnVoiceMessage does NOT cancel a running runtime;
	// it only enqueues the feedback. the existing goroutine stays alive.
	w = doPost(t, r, "/api/v1/send_to_ppt_agent", map[string]any{
		"from": "voice_agent",
		"to":   "ppt_agent",
		"data": "people have some critical feedbacks to the ppt, the feedbacks are: the font should be bigger",
	})
	assert.Equal(t, 200, w.Code)
	resp = parseResp(t, w)
	assert.Equal(t, float64(200), resp["code"])

	// the running goroutine should NOT have been cancelled.
	time.Sleep(50 * time.Millisecond)
	assert.True(t, pptSvc.IsRuntimeRunning())

	// cleanup: stop the runtime explicitly because the queue still has the
	// feedback message, so the goroutine would otherwise keep looping.
	close(firstBlocker)
	pptSvc.StopRuntime()
	pptSvc.WaitRuntime()

	// 8. update_requirements should now fail because tools disappeared
	w = doPost(t, r, "/api/v1/update_requirements", map[string]any{
		"from": "frontend",
		"to":   "voice_agent",
		"requirements": map[string]any{
			"topic": "science",
		},
	})
	assert.Equal(t, 200, w.Code) // HTTP 200 with uniform envelope code 400
	resp = parseResp(t, w)
	assert.Equal(t, float64(400), resp["code"])
	assert.Contains(t, resp["message"], "failed")

	// cleanup
	pptSvc.StopRuntime()
	pptSvc.WaitRuntime()
}

func TestVADFlow(t *testing.T) {
	r, st, asr, voiceAgent := setupVADIntegrationServer()

	// 1. start conversation
	w := doPost(t, r, "/api/v1/start_conversation", map[string]any{
		"from": "frontend",
		"to":   "voice_agent",
	})
	assert.Equal(t, 200, w.Code)
	resp := parseResp(t, w)
	assert.Equal(t, float64(200), resp["code"])
	assert.True(t, st.IsConversationStarted())

	// 2. vad_start with mocked ASR returning an interrupt
	asr.Override = func(string) string { return "stop" }
	w = doPost(t, r, "/api/v1/voice/vad_start", map[string]any{
		"audio":  "dummy",
		"format": "pcm",
	})
	assert.Equal(t, 200, w.Code)
	resp = parseResp(t, w)
	assert.Equal(t, float64(200), resp["code"])
	data := resp["data"].(map[string]any)
	assert.True(t, data["interrupt"].(bool))

	// 3. vad_end should stream voice agent response via SSE
	asr.Override = func(string) string { return "make the font bigger" }
	voiceAgent.chunks = []model.SSEChunk{
		{Type: "tts", Text: "ok"},
		{Type: "action", Payload: "send_to_ppt_agent|data:make the font bigger"},
		{Type: "turn_end"},
	}

	body := map[string]any{"audio": "dummy", "format": "pcm"}
	b, err := json.Marshal(body)
	require.NoError(t, err)
	w = httptest.NewRecorder()
	req, err := http.NewRequest("POST", "/api/v1/voice/vad_end", bytes.NewReader(b))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.Equal(t, "text/event-stream", w.Header().Get("Content-Type"))
	assert.Contains(t, w.Body.String(), `"type":"tts"`)
	assert.Contains(t, w.Body.String(), `"type":"action"`)
	assert.Contains(t, w.Body.String(), `"type":"turn_end"`)

	// 4. vad_end when interrupt was false should return ignored immediately
	st.SetLastVADInterrupt(false)
	w = doPost(t, r, "/api/v1/voice/vad_end", map[string]any{
		"audio":  "dummy",
		"format": "pcm",
	})
	assert.Equal(t, 200, w.Code)
	resp = parseResp(t, w)
	assert.Equal(t, float64(200), resp["code"])
	ignoredData := resp["data"].(map[string]any)
	assert.True(t, ignoredData["ignored"].(bool))
}
