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
	r := gin.New()
	handler.RegisterRoutes(r, voiceSvc, pptSvc, service.NewKBService(), service.NewSearchService())
	return r, st, pptSvc
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
	assert.True(t, st.IsConversationStarted())

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
	// Fake chat loop enqueues a message directly via state (so the runtime itself
	// keeps running) and then blocks on ctx.Done().
	pptSvc.SetRunChatFn(func(ctx context.Context, history []openai.ChatCompletionMessageParamUnion) ([]openai.ChatCompletionMessageParamUnion, error) {
		st.SendToVoiceAgent("the new version of the ppt is generated successfully")
		<-ctx.Done()
		return history, ctx.Err()
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
	w = doPost(t, r, "/api/v1/fetch_from_ppt_message_queue", map[string]any{
		"from": "voice_agent",
	})
	assert.Equal(t, 200, w.Code)
	resp = parseResp(t, w)
	assert.Equal(t, float64(200), resp["code"])
	assert.Equal(t, "the new version of the ppt is generated successfully", resp["data"])

	// 7. voice agent sends feedback -> runtime should restart
	// The old runtime is still running. OnVoiceMessage will detect it is running,
	// cancel it, and start a new one.
	feedbackBlocker := make(chan struct{})
	pptSvc.SetRunChatFn(func(ctx context.Context, history []openai.ChatCompletionMessageParamUnion) ([]openai.ChatCompletionMessageParamUnion, error) {
		select {
		case <-feedbackBlocker:
		case <-ctx.Done():
		}
		return history, ctx.Err()
	})

	w = doPost(t, r, "/api/v1/send_to_ppt_agent", map[string]any{
		"from": "voice_agent",
		"to":   "ppt_agent",
		"data": "people have some critical feedbacks to the ppt, the feedbacks are: the font should be bigger",
	})
	assert.Equal(t, 200, w.Code)
	resp = parseResp(t, w)
	assert.Equal(t, float64(200), resp["code"])

	// wait for restart
	time.Sleep(50 * time.Millisecond)
	assert.True(t, pptSvc.IsRuntimeRunning())

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
