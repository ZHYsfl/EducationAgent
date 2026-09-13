package llm

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/openai/openai-go/v3"
)

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// TestLiveDeepSeek hits the real API; it is skipped in -short mode and when
// no key is configured.
func TestLiveDeepSeek(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live test in -short mode")
	}
	apiKey := os.Getenv("DEEPSEEK_API_KEY")
	if apiKey == "" {
		t.Skip("DEEPSEEK_API_KEY not set")
	}

	client := NewClient(Config{
		APIKey:  apiKey,
		BaseURL: envOr("DEEPSEEK_BASE_URL", "https://api.deepseek.com"),
		Model:   envOr("DEEPSEEK_MODEL", "deepseek-flash"),
	})

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	messages := []openai.ChatCompletionMessageParamUnion{
		openai.UserMessage("用一句话介绍杭州"),
	}

	var text strings.Builder
	for ev := range client.Stream(ctx, messages, nil) {
		switch ev.Kind {
		case EventContent:
			text.WriteString(ev.Text)
		case EventFinish:
			t.Logf("finish_reason=%s", ev.FinishReason)
		}
	}

	if text.Len() == 0 {
		t.Fatal("expected non-empty reply")
	}
	t.Logf("reply: %s", text.String())
}
