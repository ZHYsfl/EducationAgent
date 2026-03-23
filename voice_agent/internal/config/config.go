package config

import (
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	ServerPort int

	ASRMode  string // "local" or "remote"
	ASRWSURL string // local mode

	DouBaoASRAppKey     string // remote mode
	DouBaoASRAccessKey  string
	DouBaoASRResourceId string

	SmallLLMAPIKey  string
	SmallLLMBaseURL string
	SmallLLMModel   string

	LargeLLMAPIKey  string
	LargeLLMBaseURL string
	LargeLLMModel   string

	TTSMode string // "local" or "remote"
	TTSURL  string // local mode

	DouBaoTTSAppId     string // remote mode
	DouBaoTTSToken     string
	DouBaoTTSCluster   string
	DouBaoTTSVoiceType string

	TokenBudget    int
	FillerInterval int
	FillerPhrases  []string
	MaxFillers     int

	SystemPrompt string

	AdaptiveSizesFile string

	// External services
	PPTAgentURL  string
	KBServiceURL string
	MemoryURL    string
	SearchURL    string
	DBServiceURL string

	// Fallback identity for local development when auth is not wired yet
	DefaultUserID string
}

func LoadConfig() *Config {
	_ = godotenv.Load("../.env")
	_ = godotenv.Load(".env")

	port, _ := strconv.Atoi(getEnv("SERVER_PORT", "9000"))
	tokenBudget, _ := strconv.Atoi(getEnv("TOKEN_BUDGET", "50"))
	fillerInterval, _ := strconv.Atoi(getEnv("FILLER_INTERVAL", "100"))

	return &Config{
		ServerPort: port,

		ASRMode:  getEnv("ASR_MODE", "local"),
		ASRWSURL: getEnv("ASR_WS_URL", "ws://localhost:10096"),

		DouBaoASRAppKey:     getEnv("DOUBAO_ASR_APP_KEY", ""),
		DouBaoASRAccessKey:  getEnv("DOUBAO_ASR_ACCESS_KEY", ""),
		DouBaoASRResourceId: getEnv("DOUBAO_ASR_RESOURCE_ID", "volc.bigasr.sauc.duration"),

		SmallLLMAPIKey:  getEnv("SMALL_LLM_API_KEY", "EMPTY"),
		SmallLLMBaseURL: getEnv("SMALL_LLM_BASE_URL", "http://localhost:8001/v1"),
		SmallLLMModel:   getEnv("SMALL_LLM_MODEL", "small-llm"),
		LargeLLMAPIKey:  getEnv("LARGE_LLM_API_KEY", "EMPTY"),
		LargeLLMBaseURL: getEnv("LARGE_LLM_BASE_URL", "http://localhost:8000/v1"),
		LargeLLMModel:   getEnv("LARGE_LLM_MODEL", "large-llm"),

		TTSMode: getEnv("TTS_MODE", "local"),
		TTSURL:  getEnv("TTS_URL", "http://localhost:50000"),

		DouBaoTTSAppId:     getEnv("DOUBAO_TTS_APPID", ""),
		DouBaoTTSToken:     getEnv("DOUBAO_TTS_TOKEN", ""),
		DouBaoTTSCluster:   getEnv("DOUBAO_TTS_CLUSTER", "volcano_tts"),
		DouBaoTTSVoiceType: getEnv("DOUBAO_TTS_VOICE_TYPE", "BV700_streaming"),

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
		DBServiceURL:  getEnv("DB_SERVICE_URL", "http://localhost:9500"),
		DefaultUserID: getEnv("USER_ID", "user_default"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
