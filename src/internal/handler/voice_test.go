package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"educationagent/internal/service"
	"educationagent/internal/state"

	"github.com/gin-gonic/gin"
)

func setupRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	store := state.NewAppState()
	svc := service.NewVoiceService(store)
	h := NewVoiceHandler(svc, nil, nil)
	r := gin.New()
	RegisterRoutes(r, h)
	return r
}

func TestUpdateRequirements(t *testing.T) {
	r := setupRouter()
	body, _ := json.Marshal(map[string]any{
		"requirements": map[string]any{
			"topic": "AI",
			"style": "modern",
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/voice/update_requirements", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["code"].(float64) != 200 {
		t.Fatalf("code = %v", resp["code"])
	}
}

func TestRequireConfirmIncomplete(t *testing.T) {
	r := setupRouter()
	body, _ := json.Marshal(map[string]any{
		"requirements": map[string]any{},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/voice/require_confirm", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", w.Code)
	}
}

func TestStartConversationValidation(t *testing.T) {
	r := setupRouter()
	body, _ := json.Marshal(map[string]any{
		"from": "frontend",
		"to":   "voice_agent",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/voice/start_conversation", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
}

func TestSendToPPTAgent(t *testing.T) {
	r := setupRouter()
	body, _ := json.Marshal(map[string]any{"data": "feedback"})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/voice/send_to_ppt_agent", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
}

func TestGetMessagesFromPPTAgent(t *testing.T) {
	r := setupRouter()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/voice/get_messages_from_ppt_agent", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
}
