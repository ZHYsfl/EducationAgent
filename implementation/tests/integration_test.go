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
	"educationagent/internal/tools"
)

func setupIntegrationServer() (*gin.Engine, *state.AppState, *service.ArmService) {
	gin.SetMode(gin.TestMode)
	st := state.NewAppState()
	agent := toolcalling.NewAgent(toolcalling.LLMConfig{})
	armSvc := service.NewArmService(st, agent, tools.NewArmGateway("http://127.0.0.1:1"))
	asrSvc := service.NewASRService()
	interruptSvc := service.NewInterruptService(toolcalling.LLMConfig{})
	voiceAgentSvc := service.NewVoiceAgentService(toolcalling.LLMConfig{}, nil)
	r := gin.New()
	handler.RegisterRoutes(r, st, armSvc, asrSvc, interruptSvc, voiceAgentSvc)
	return r, st, armSvc
}

func setupVADIntegrationServer() (*gin.Engine, *state.AppState, *service.StubASRService, *mockVoiceAgentSvc) {
	gin.SetMode(gin.TestMode)
	st := state.NewAppState()
	agent := toolcalling.NewAgent(toolcalling.LLMConfig{})
	armSvc := service.NewArmService(st, agent, tools.NewArmGateway("http://127.0.0.1:1"))
	asrSvc := &service.StubASRService{}
	interruptSvc := &mockInterruptCheckSvc{result: true}
	voiceAgentSvc := &mockVoiceAgentSvc{}
	r := gin.New()
	handler.RegisterRoutes(r, st, armSvc, asrSvc, interruptSvc, voiceAgentSvc)
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

func (m *mockVoiceAgentSvc) StreamTurn(ctx context.Context, st *state.AppState, transcript string, needsInterruptedPrefix bool, interruptedAssistant string, out chan<- model.SSEChunk) error {
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

func parseResp(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	var resp map[string]any
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	return resp
}

// TestFullConversationFlow walks the async dual-agent chain end to end:
// task dispatch → arm agent runtime → progress report → mid-task change.
func TestFullConversationFlow(t *testing.T) {
	r, st, armSvc := setupIntegrationServer()

	// 1. start conversation
	w := doPost(t, r, "/api/v1/start_conversation", map[string]any{
		"from": "frontend",
		"to":   "voice_agent",
	})
	assert.Equal(t, 200, w.Code)
	resp := parseResp(t, w)
	assert.Equal(t, float64(200), resp["code"])

	// 2. send a task to the arm agent — should start the runtime.
	// The fake turn loop reports progress and then blocks so the runtime
	// stays alive.
	firstBlocker := make(chan struct{})
	armSvc.SetRunTurnFn(func(ctx context.Context, history []openai.ChatCompletionMessageParamUnion) ([]openai.ChatCompletionMessageParamUnion, error) {
		st.SendToVoiceAgent("已到达物块位置，准备抓取 red 物块")
		<-firstBlocker
		return history, ctx.Err()
	})

	w = doPost(t, r, "/api/v1/send_to_arm_agent", map[string]any{
		"from":    "voice_agent",
		"to":      "arm_agent",
		"content": "抓取 red 物块并放到 (1.0,2.0,3.0)。",
	})
	assert.Equal(t, 200, w.Code)
	resp = parseResp(t, w)
	assert.Equal(t, float64(200), resp["code"])
	assert.Equal(t, "发送成功", resp["data"])

	// wait for the runtime to start and report
	require.Eventually(t, func() bool {
		return armSvc.IsRuntimeRunning() && st.ArmMessageQueueLen() > 0
	}, time.Second, 10*time.Millisecond)

	// the task entered the arm context as one consumed user message
	history := st.GetArmHistory()
	require.GreaterOrEqual(t, len(history), 2)
	last := history[len(history)-1]
	require.NotNil(t, last.OfUser)
	assert.Equal(t, "all_messages_from_voice_agent:抓取 red 物块并放到 (1.0,2.0,3.0)。", last.OfUser.Content.OfString.Value)

	// 3. voice agent consumes the arm report
	w = doPost(t, r, "/api/v1/get_message_from_arm_agent", map[string]any{})
	assert.Equal(t, 200, w.Code)
	resp = parseResp(t, w)
	assert.Equal(t, float64(200), resp["code"])
	assert.Equal(t, "all_messages_from_arm_agent:已到达物块位置，准备抓取 red 物块", resp["data"])

	// 4. a change message arrives while the runtime is running.
	// OnVoiceMessage does NOT cancel a running runtime; it only enqueues.
	w = doPost(t, r, "/api/v1/send_to_arm_agent", map[string]any{
		"from":    "voice_agent",
		"to":      "arm_agent",
		"content": "用户改主意了，请改抓 yellow 物块",
	})
	assert.Equal(t, 200, w.Code)
	resp = parseResp(t, w)
	assert.Equal(t, float64(200), resp["code"])

	// the running goroutine should NOT have been cancelled.
	time.Sleep(50 * time.Millisecond)
	assert.True(t, armSvc.IsRuntimeRunning())
	assert.Equal(t, 1, st.VoiceMessageQueueLen())

	// cleanup: stop the runtime explicitly because the queue still holds the
	// change message, so the goroutine would otherwise keep looping.
	close(firstBlocker)
	armSvc.StopRuntime()
	armSvc.WaitRuntime()
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
	asr.Override = func(string) string { return "帮我把红色的物块抓到 1.0, 2.0, 3.0 那里" }
	voiceAgent.chunks = []model.SSEChunk{
		{Type: "tts", Text: "好的"},
		{Type: "action", Payload: "send_to_arm_agent:抓取 red 物块并放到 (1.0,2.0,3.0)。"},
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
