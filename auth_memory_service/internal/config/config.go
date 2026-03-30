package config

import (
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	ServerPort          int
	PostgresDSN         string
	RedisAddr           string
	RedisPassword       string
	RedisDB             int
	WorkingMemoryTTLHrs int
	JWTSecret           string
	JWTTTLHours         int
	VerifyTokenTTLHours int
	FrontendVerifyURL   string
	InternalKey         string
	ExtractorLLMEnabled bool
	ExtractorLLMModel   string
	ExtractorTimeoutMS  int
	ExtractorMaxTurns   int
	DeepSeekAPIKey      string
	DeepSeekBaseURL     string
}

func Load() Config {
	_ = godotenv.Load()
	return Config{
		ServerPort:          getEnvInt("AUTH_MEMORY_PORT", 9300),
		PostgresDSN:         getEnv("POSTGRES_DSN", ""),
		RedisAddr:           getEnv("REDIS_ADDR", "localhost:6379"),
		RedisPassword:       getEnv("REDIS_PASSWORD", ""),
		RedisDB:             getEnvInt("REDIS_DB", 0),
		WorkingMemoryTTLHrs: getEnvInt("WORKING_MEMORY_TTL_HOURS", 4),
		JWTSecret:           getEnv("JWT_SECRET", "change-me"),
		JWTTTLHours:         getEnvInt("JWT_TTL_HOURS", 24),
		VerifyTokenTTLHours: getEnvInt("VERIFY_TOKEN_TTL_HOURS", 24),
		FrontendVerifyURL:   getEnv("FRONTEND_VERIFY_URL", "http://localhost:3000/verify-email"),
		InternalKey:         getEnv("INTERNAL_KEY", ""),
		ExtractorLLMEnabled: getEnvBool("EXTRACTOR_LLM_ENABLED", false),
		ExtractorLLMModel:   getEnv("EXTRACTOR_LLM_MODEL", "deepseek-chat"),
		ExtractorTimeoutMS:  getEnvInt("EXTRACTOR_LLM_TIMEOUT_MS", 2000),
		ExtractorMaxTurns:   getEnvInt("EXTRACTOR_MAX_TURNS", 16),
		DeepSeekAPIKey:      getEnv("DEEPSEEK_API_KEY", ""),
		DeepSeekBaseURL:     getEnv("DEEPSEEK_BASE_URL", "https://api.deepseek.com"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func getEnvBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	switch v {
	case "1", "true", "TRUE", "True", "yes", "YES", "on", "ON":
		return true
	case "0", "false", "FALSE", "False", "no", "NO", "off", "OFF":
		return false
	default:
		return fallback
	}
}
