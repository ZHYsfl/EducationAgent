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
