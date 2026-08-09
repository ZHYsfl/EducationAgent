package main

import (
	"context"
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

	// Arm agent: background embodied executor. LLM from ARM_LLM_* env vars,
	// embodied tools via the RESTful gateway at ARM_GATEWAY_BASE_URL
	// (default http://127.0.0.1:8000, see api_of_embodied_tools.md).
	armSvc := service.NewArmService(appState, nil, nil)
	asrSvc := service.NewASRService()
	// Interrupt-check LLM and Voice-Agent LLM are local fine-tuned models.
	// They expose an OpenAI-compatible HTTP API (e.g. vLLM / llama.cpp server).
	interruptSvc := service.NewInterruptService(toolcalling.LLMConfig{
		APIKey:  os.Getenv("INTERRUPT_LLM_API_KEY"),
		Model:   os.Getenv("INTERRUPT_LLM_MODEL"),
		BaseURL: os.Getenv("INTERRUPT_LLM_BASE_URL"),
	})
	// Build the voice-agent tool executor that parses <tool_call>...</tool_call>
	// payloads and maps them to actual Go calls. The voice agent has exactly
	// two tools (api_of_voice_tools.md): send_to_arm_agent and
	// get_message_from_arm_agent.
	voiceToolExec := voiceagent.NewExecutor()
	voiceToolExec.Register("send_to_arm_agent", func(ctx context.Context, args map[string]string) (string, error) {
		content := strings.TrimSpace(args["content"])
		if content == "" {
			return "", fmt.Errorf("send_to_arm_agent 缺少任务内容")
		}
		if err := armSvc.OnVoiceMessage(content); err != nil {
			return "", err
		}
		return "发送成功", nil
	})
	voiceToolExec.Register("get_message_from_arm_agent", func(ctx context.Context, args map[string]string) (string, error) {
		msgs := appState.DrainArmMessageQueue()
		if len(msgs) == 0 {
			return "当前没有新消息", nil
		}
		return "all_messages_from_arm_agent:" + strings.Join(msgs, ";"), nil
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
	voiceAgentSvc := service.NewVoiceAgentService(voiceLLMCfg, voiceToolExec)

	handler.RegisterRoutes(r, appState, armSvc, asrSvc, interruptSvc, voiceAgentSvc)

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

	armSvc.StopRuntime()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("server shutdown: %v", err)
	}
}
