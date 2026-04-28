package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"educationagent/internal/handler"
	"educationagent/internal/service"
	"educationagent/internal/state"
	"educationagent/internal/toolcalling"
	"educationagent/internal/voiceagent"

	"github.com/gin-gonic/gin"
)

func main() {
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	appState := state.NewAppState()

	voiceSvc := service.NewVoiceService(appState)
	kbSvc := service.NewKBService()
	searchSvc := service.NewSearchService()
	pptSvc := service.NewPPTService(appState, nil, kbSvc, searchSvc)
	asrSvc := service.NewASRService()
	// Interrupt-check LLM and Voice-Agent LLM are local fine-tuned models.
	// They expose an OpenAI-compatible HTTP API (e.g. vLLM / llama.cpp server).
	interruptSvc := service.NewInterruptService(toolcalling.LLMConfig{
		APIKey:  os.Getenv("INTERRUPT_LLM_API_KEY"),
		Model:   os.Getenv("INTERRUPT_LLM_MODEL"),
		BaseURL: os.Getenv("INTERRUPT_LLM_BASE_URL"),
	})
	// Build the voice-agent action executor that parses <action>...</action>
	// payloads and maps them to actual Go function calls.
	voiceActionExec := voiceagent.NewExecutor()
	voiceActionExec.Register("update_requirements", func(ctx context.Context, args map[string]string) (string, error) {
		reqMap := voiceagent.ArgsToMap(args, "total_pages")
		missing, err := voiceSvc.UpdateRequirements(reqMap)
		if err != nil {
			return "", err
		}
		if len(missing) > 0 {
			return fmt.Sprintf("we now still missing %s", strings.Join(missing, ", ")), nil
		}
		return "all fields are updated", nil
	})
	voiceActionExec.Register("require_confirm", func(ctx context.Context, args map[string]string) (string, error) {
		if err := voiceSvc.RequireConfirm(); err != nil {
			return "", err
		}
		req := appState.GetRequirements()
		b, _ := json.Marshal(req)
		return "require_confirm:" + string(b), nil
	})
	voiceActionExec.Register("send_to_ppt_agent", func(ctx context.Context, args map[string]string) (string, error) {
		data := args["data"]
		if err := pptSvc.OnVoiceMessage(data); err != nil {
			return "", err
		}
		return "data is sent to the ppt agent successfully", nil
	})
	voiceActionExec.Register("fetch_from_ppt_message_queue", func(ctx context.Context, args map[string]string) (string, error) {
		msg, err := voiceSvc.FetchFromPPTMessageQueue()
		if err != nil {
			return "failed to fetch the data from the ppt message queue", err
		}
		if msg == "" {
			return "queue is empty", nil
		}
		return fmt.Sprintf("the ppt message is: %s", msg), nil
	})

	voiceLLMCfg := toolcalling.LLMConfig{
		APIKey:  os.Getenv("VOICE_LLM_API_KEY"),
		Model:   os.Getenv("VOICE_LLM_MODEL"),
		BaseURL: os.Getenv("VOICE_LLM_BASE_URL"),
		ExtraBody: map[string]any{
			"chat_template_kwargs": map[string]any{"enable_thinking": false},
		},
	}
	if v := strings.TrimSpace(os.Getenv("VOICE_LLM_MAX_TOKENS")); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			voiceLLMCfg.StreamMaxTokens = n
		}
	}
	voiceAgentSvc := service.NewVoiceAgentService(voiceLLMCfg, voiceActionExec)

	handler.RegisterRoutes(r, appState, voiceSvc, pptSvc, kbSvc, searchSvc, asrSvc, interruptSvc, voiceAgentSvc)

	srv := &http.Server{
		Addr:    ":8080",
		Handler: r,
	}

	go func() {
		log.Println("Server listening on :8080")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("failed to run server: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	pptSvc.ReleaseSlidevPreviewPort()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("server shutdown: %v", err)
	}
}
