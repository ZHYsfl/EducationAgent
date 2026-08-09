package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"educationagent/internal/service"
	"educationagent/internal/state"
	"educationagent/internal/toolcalling"
	"educationagent/internal/tools"

	"github.com/gin-gonic/gin"
	"github.com/openai/openai-go/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupRouter() (*gin.Engine, *state.AppState, *service.ArmService) {
	gin.SetMode(gin.TestMode)
	st := state.NewAppState()
	agent := toolcalling.NewAgent(toolcalling.LLMConfig{})
	armSvc := service.NewArmService(st, agent, tools.NewArmGateway("http://127.0.0.1:1"))
	asrSvc := service.NewASRService()
	interruptSvc := service.NewInterruptService(toolcalling.LLMConfig{})
	voiceAgentSvc := service.NewVoiceAgentService(toolcalling.LLMConfig{}, nil)
	r := gin.New()
	RegisterRoutes(r, st, armSvc, asrSvc, interruptSvc, voiceAgentSvc)
	return r, st, armSvc
}

func doPostJSON(t *testing.T, r *gin.Engine, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	require.NoError(t, err)
	w := httptest.NewRecorder()
	req, err := http.NewRequest("POST", path, bytes.NewReader(b))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

func parseEnvelope(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	return resp
}

func TestSendToArmAgentAndGetMessage(t *testing.T) {
	r, st, armSvc := setupRouter()
	armSvc.SetRunTurnFn(func(ctx context.Context, history []openai.ChatCompletionMessageParamUnion) ([]openai.ChatCompletionMessageParamUnion, error) {
		// Simulate the arm agent reporting progress, then wait for cancellation.
		st.SendToVoiceAgent("已到达物块位置")
		<-ctx.Done()
		return history, ctx.Err()
	})

	w := doPostJSON(t, r, "/api/v1/send_to_arm_agent", map[string]any{
		"from":    "voice_agent",
		"to":      "arm_agent",
		"content": "抓取 red 物块并放到 (1.0,2.0,3.0)。",
	})
	assert.Equal(t, 200, w.Code)
	resp := parseEnvelope(t, w)
	assert.Equal(t, float64(200), resp["code"])
	assert.Equal(t, "发送成功", resp["data"])

	// Wait for the arm agent to enqueue its report.
	require.Eventually(t, func() bool { return st.ArmMessageQueueLen() > 0 }, time.Second, 10*time.Millisecond)

	// Drain via the voice-side endpoint: contract string, joined with ";".
	w2 := doPostJSON(t, r, "/api/v1/get_message_from_arm_agent", map[string]any{})
	assert.Equal(t, 200, w2.Code)
	resp = parseEnvelope(t, w2)
	assert.Equal(t, float64(200), resp["code"])
	assert.Equal(t, "all_messages_from_arm_agent:已到达物块位置", resp["data"])

	// Queue now empty → fixed contract string.
	w3 := doPostJSON(t, r, "/api/v1/get_message_from_arm_agent", map[string]any{})
	resp = parseEnvelope(t, w3)
	assert.Equal(t, "当前没有新消息", resp["data"])

	armSvc.StopRuntime()
	armSvc.WaitRuntime()
}

func TestSendToVoiceAgentEndpoint(t *testing.T) {
	r, st, _ := setupRouter()

	w := doPostJSON(t, r, "/api/v1/send_to_voice_agent", map[string]any{
		"from":    "arm_agent",
		"to":      "voice_agent",
		"content": "有这种颜色的物块，且夹取物块成功",
	})
	assert.Equal(t, 200, w.Code)
	resp := parseEnvelope(t, w)
	assert.Equal(t, float64(200), resp["code"])
	assert.Equal(t, "发送成功", resp["data"])

	msgs := st.DrainArmMessageQueue()
	assert.Equal(t, []string{"有这种颜色的物块，且夹取物块成功"}, msgs)
}

func TestSendToArmAgentRejectsEmptyContent(t *testing.T) {
	r, _, _ := setupRouter()

	w := doPostJSON(t, r, "/api/v1/send_to_arm_agent", map[string]any{
		"from": "voice_agent",
		"to":   "arm_agent",
	})
	assert.Equal(t, 200, w.Code)
	resp := parseEnvelope(t, w)
	assert.Equal(t, float64(400), resp["code"])
}

func TestStartConversation(t *testing.T) {
	r, st, _ := setupRouter()

	w := doPostJSON(t, r, "/api/v1/start_conversation", map[string]any{"from": "frontend", "to": "voice_agent"})
	assert.Equal(t, 200, w.Code)
	assert.True(t, st.IsConversationStarted())
}
