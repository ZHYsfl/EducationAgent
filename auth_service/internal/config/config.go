package config

import (
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	ServerPort          int
	PostgresDSN         string
	JWTSecret           string
	JWTTTLHours         int
	VerifyTokenTTLHours int
	FrontendVerifyURL   string
	InternalKey         string
}

func Load() Config {
	_ = godotenv.Load()
	return Config{
		ServerPort:          getEnvInt("AUTH_PORT", getEnvInt("AUTH_MEMORY_PORT", 9300)),
		PostgresDSN:         getEnv("POSTGRES_DSN", ""),
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
