package main

import (
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	ServerPort int

	ASRWSURL string

	SmallLLMAPIKey  string
	SmallLLMBaseURL string
	SmallLLMModel   string

	LargeLLMAPIKey  string
	LargeLLMBaseURL string
	LargeLLMModel   string

	TTSURL string

	TokenBudget   int
	FillerInterval int
	FillerPhrase1 string
	FillerPhrase2 string

	SystemPrompt string

	AdaptiveSizesFile string
}

func LoadConfig() *Config {
	godotenv.Load()

	port, _ := strconv.Atoi(getEnv("SERVER_PORT", "9000"))
	tokenBudget, _ := strconv.Atoi(getEnv("TOKEN_BUDGET", "50"))
	fillerInterval, _ := strconv.Atoi(getEnv("FILLER_INTERVAL", "100"))

	return &Config{
		ServerPort:      port,
		ASRWSURL:        getEnv("ASR_WS_URL", "ws://localhost:10096"),
		SmallLLMAPIKey:  getEnv("SMALL_LLM_API_KEY", "EMPTY"),
		SmallLLMBaseURL: getEnv("SMALL_LLM_BASE_URL", "http://localhost:8001/v1"),
		SmallLLMModel:   getEnv("SMALL_LLM_MODEL", "small-llm"),
		LargeLLMAPIKey:  getEnv("LARGE_LLM_API_KEY", "EMPTY"),
		LargeLLMBaseURL: getEnv("LARGE_LLM_BASE_URL", "http://localhost:8000/v1"),
		LargeLLMModel:   getEnv("LARGE_LLM_MODEL", "large-llm"),
		TTSURL:          getEnv("TTS_URL", "http://localhost:50000"),
		TokenBudget:     tokenBudget,
		FillerInterval:  fillerInterval,
		FillerPhrase1:   getEnv("FILLER_PHRASE_1", "好的，让我想一下"),
		FillerPhrase2:   getEnv("FILLER_PHRASE_2", "还在查，稍等一下"),
		SystemPrompt:      getEnv("SYSTEM_PROMPT", "你是一个有帮助的AI教育助手，请用中文回答问题。"),
		AdaptiveSizesFile: getEnv("ADAPTIVE_SIZES_FILE", "adaptive_sizes.json"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
