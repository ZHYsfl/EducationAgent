package config

import (
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	ServerPort int // voice agent listen port

	ASRWSURL string // ASR WebSocket URL

	SmallLLMAPIKey  string // Small LLM API Key
	SmallLLMBaseURL string // Small LLM Base URL
	SmallLLMModel   string // Small LLM Model

	LargeLLMAPIKey  string // Large LLM API Key
	LargeLLMBaseURL string // Large LLM Base URL
	LargeLLMModel   string // Large LLM Model

	TTSURL string // TTS HTTP URL

	TokenBudget    int      // token budget for small LLM
	FillerInterval int      // filler interval for small LLM
	FillerPhrases  []string // filler phrases for small LLM
	MaxFillers     int      // max fillers for small LLM

	SystemPrompt string // system prompt

	AdaptiveSizesFile string // adaptive sizes file path

	// External services
	PPTAgentURL  string // PPT Agent HTTP URL
	KBServiceURL string // KB Service HTTP URL
	MemoryURL    string // Memory HTTP URL
	SearchURL    string // Search HTTP URL
	DBServiceURL string // DB Service HTTP URL
}

func LoadConfig() *Config {
	_ = godotenv.Load("../.env")
	_ = godotenv.Load(".env")

	port, _ := strconv.Atoi(getEnv("SERVER_PORT", "9000"))
	tokenBudget, _ := strconv.Atoi(getEnv("TOKEN_BUDGET", "50"))
	fillerInterval, _ := strconv.Atoi(getEnv("FILLER_INTERVAL", "100"))

	return &Config{
		ServerPort: port,

		ASRWSURL: getEnv("ASR_WS_URL", "ws://localhost:10096"),

		SmallLLMAPIKey:  getEnv("SMALL_LLM_API_KEY", "EMPTY"),
		SmallLLMBaseURL: getEnv("SMALL_LLM_BASE_URL", "http://localhost:8001/v1"),
		SmallLLMModel:   getEnv("SMALL_LLM_MODEL", "small-llm"),
		LargeLLMAPIKey:  getEnv("LARGE_LLM_API_KEY", "EMPTY"),
		LargeLLMBaseURL: getEnv("LARGE_LLM_BASE_URL", "http://localhost:8000/v1"),
		LargeLLMModel:   getEnv("LARGE_LLM_MODEL", "large-llm"),

		TTSURL: getEnv("TTS_URL", "http://localhost:50000"),

		TokenBudget:    tokenBudget,
		FillerInterval: fillerInterval,
		FillerPhrases: []string{
			getEnv("FILLER_PHRASE_1", "好的，让我想一下"),
			getEnv("FILLER_PHRASE_2", "还在想，稍等一下"),
			getEnv("FILLER_PHRASE_3", "马上就好"),
		},
		MaxFillers:        3,
		SystemPrompt:      getEnv("SYSTEM_PROMPT", "你是一个有帮助的AI教育助手，请用中文回答问题。"),
		AdaptiveSizesFile: getEnv("ADAPTIVE_SIZES_FILE", "adaptive_sizes.json"),

		PPTAgentURL:   getEnv("PPT_AGENT_URL", "http://localhost:9100"),
		KBServiceURL:  getEnv("KB_SERVICE_URL", "http://localhost:9200"),
		MemoryURL:     getEnv("MEMORY_SERVICE_URL", "http://localhost:9300"),
		SearchURL:     getEnv("SEARCH_SERVICE_URL", "http://localhost:9400"),
		DBServiceURL: getEnv("DB_SERVICE_URL", "http://localhost:9500"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
