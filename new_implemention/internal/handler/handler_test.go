package handler

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
	"educationagent/internal/service"
	"educationagent/internal/state"
	"educationagent/internal/toolcalling"
)

func setupRouter() (*gin.Engine, *state.AppState, *service.VoiceService, *service.PPTService) {
	gin.SetMode(gin.TestMode)
	st := state.NewAppState()
	voiceSvc := service.NewVoiceService(st)
	agent := toolcalling.NewAgent(toolcalling.LLMConfig{})
	pptSvc := service.NewPPTService(st, agent, service.NewKBService(), service.NewSearchService())
	r := gin.New()
	RegisterRoutes(r, voiceSvc, pptSvc, service.NewKBService(), service.NewSearchService())
	return r, st, voiceSvc, pptSvc
}

func TestUpdateRequirements(t *testing.T) {
	r, _, _, _ := setupRouter()

	body := map[string]any{
		"from": "frontend",
		"to":   "voice_agent",
		"requirements": map[string]any{
			"topic": "math",
			"style": "simple",
		},
	}
	b, _ := json.Marshal(body)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/update_requirements", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, float64(200), resp["code"])
	data := resp["data"].(map[string]any)
	missing := data["missing_fields"].([]any)
	assert.Len(t, missing, 2)
}

func TestRequireConfirm(t *testing.T) {
	r, _, _, _ := setupRouter()

	// complete requirements first
	body := map[string]any{
		"from": "frontend",
		"to":   "voice_agent",
		"requirements": map[string]any{
			"topic":       "math",
			"style":       "simple",
			"total_pages": 10,
			"audience":    "kids",
		},
	}
	b, _ := json.Marshal(body)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/update_requirements", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, 200, w.Code)

	body2 := map[string]any{
		"from": "voice_agent",
		"to":   "frontend",
		"requirements": map[string]any{
			"topic":       "math",
			"style":       "simple",
			"total_pages": 10,
			"audience":    "kids",
		},
	}
	b2, _ := json.Marshal(body2)
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest("POST", "/api/v1/require_confirm", bytes.NewReader(b2))
	req2.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w2, req2)

	assert.Equal(t, 200, w2.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &resp))
	assert.Equal(t, float64(200), resp["code"])
}

func TestSendToPPTAgentAndFetch(t *testing.T) {
	r, st, _, pptSvc := setupRouter()
	pptSvc.SetRunChatFn(func(ctx context.Context, history []openai.ChatCompletionMessageParamUnion) ([]openai.ChatCompletionMessageParamUnion, error) {
		// simulate ppt agent sending a message then waiting for cancellation
		pptSvc.SendToVoiceAgent("ppt done")
		<-ctx.Done()
		return history, ctx.Err()
	})

	// finalize requirements
	_, _ = st.UpdateRequirements(map[string]any{
		"topic":       "math",
		"style":       "simple",
		"total_pages": 10,
		"audience":    "kids",
	})

	body := map[string]any{
		"from": "voice_agent",
		"to":   "ppt_agent",
		"data": "start ppt generation",
	}
	b, _ := json.Marshal(body)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/send_to_ppt_agent", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, 200, w.Code)

	// wait for ppt agent to enqueue message and stop
	time.Sleep(100 * time.Millisecond)

	// fetch from ppt queue
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest("GET", "/api/v1/fetch_from_ppt_message_queue", nil)
	r.ServeHTTP(w2, req2)

	assert.Equal(t, 200, w2.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &resp))
	assert.Equal(t, float64(200), resp["code"])
	assert.Equal(t, "ppt done", resp["data"])
}

func TestStartConversation(t *testing.T) {
	r, _, _, _ := setupRouter()

	body := map[string]any{"from": "frontend", "to": "voice_agent"}
	b, _ := json.Marshal(body)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/start_conversation", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
}

func TestKBQueryChunks(t *testing.T) {
	r, _, _, _ := setupRouter()

	body := map[string]any{"from": "ppt_agent", "to": "kb_service", "query": "math"}
	b, _ := json.Marshal(body)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/kb/query-chunks", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, float64(200), resp["code"])
	data := resp["data"].(map[string]any)
	assert.NotNil(t, data["chunks"])
}

func TestSearchQuery(t *testing.T) {
	r, _, _, _ := setupRouter()

	body := map[string]any{"from": "ppt_agent", "to": "search_service", "query": "math trends"}
	b, _ := json.Marshal(body)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/search/query", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, float64(200), resp["code"])
	assert.Contains(t, resp["data"], "math trends")
}
