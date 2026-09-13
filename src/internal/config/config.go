// Package config loads service configuration from environment variables.
package config

import (
	"os"
	"strings"
)

// Config holds every external dependency address and credential.
type Config struct {
	DeepSeekAPIKey  string
	DeepSeekBaseURL string
	DeepSeekModel   string
	ASRBaseURL      string
	TTSBaseURL      string
	ServerAddr      string
}

// Load reads configuration from the environment, falling back to defaults
// that match a local all-in-one deployment.
func Load() Config {
	return Config{
		DeepSeekAPIKey:  os.Getenv("DEEPSEEK_API_KEY"),
		DeepSeekBaseURL: getEnv("DEEPSEEK_BASE_URL", "https://api.deepseek.com"),
		DeepSeekModel:   getEnv("DEEPSEEK_MODEL", "deepseek-flash"),
		ASRBaseURL:      getEnv("ASR_BASE_URL", "http://127.0.0.1:8000"),
		TTSBaseURL:      getEnv("TTS_BASE_URL", "http://127.0.0.1:8001"),
		ServerAddr:      getEnv("SERVER_ADDR", ":8080"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// LoadFile sets KEY=VALUE pairs from a dotenv-style file into the
// environment, skipping keys that are already set, blank lines and #-comments.
func LoadFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" || os.Getenv(key) != "" {
			continue
		}
		if err := os.Setenv(key, strings.TrimSpace(value)); err != nil {
			return err
		}
	}
	return nil
}
