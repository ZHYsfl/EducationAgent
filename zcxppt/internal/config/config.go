package config

import (
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	ServerPort       int
	RedisAddr        string
	RedisPassword    string
	RedisDB          int
	JWTSecret        string
	InternalKey      string
	VoiceAgentURL    string
	KBServiceURL     string
	OSSBaseURL       string
	LLMRuntimeMode   string
	FeedbackRepoMode string
	ExportRepoMode   string
	PPTRepoMode      string
	LLMAPIKey        string
	LLMModel         string
	LLMBaseURL       string
	TaskRepoMode     string
	TaskTTLHours     int
	OSSProvider      string
	OSSBucket        string
	OSSRegion        string
	OSSSecretID      string
	OSSSecretKey     string
	OSSSigningKey    string
	OSSLocalPath     string
	RendererMode     string
	PythonPath       string
	RenderScriptPath string
	RenderDir        string
	RenderURLPrefix  string
	RenderTimeoutSec int
}

func Load() Config {
	_ = godotenv.Load()
	return Config{
		ServerPort:       getEnvInt("ZCXPPT_PORT", 9400),
		RedisAddr:        getEnv("REDIS_ADDR", "localhost:6379"),
		RedisPassword:    getEnv("REDIS_PASSWORD", ""),
		RedisDB:          getEnvInt("REDIS_DB", 0),
		JWTSecret:        getEnv("JWT_SECRET", "change-me"),
		InternalKey:      getEnv("INTERNAL_KEY", ""),
		VoiceAgentURL:    getEnv("VOICE_AGENT_URL", "http://localhost:9200"),
		KBServiceURL:     getEnv("KB_SERVICE_URL", "http://localhost:9100"),
		OSSBaseURL:       getEnv("OSS_BASE_URL", "http://localhost:9000"),
		LLMRuntimeMode:   getEnv("LLM_RUNTIME_MODE", "real"),
		FeedbackRepoMode: getEnv("FEEDBACK_REPO_MODE", "redis"),
		ExportRepoMode:   getEnv("EXPORT_REPO_MODE", "redis"),
		PPTRepoMode:      getEnv("PPT_REPO_MODE", "redis"),
		LLMAPIKey:        getEnv("LLM_API_KEY", ""),
		LLMModel:         getEnv("LLM_MODEL", "kimi-k2.5"),
		LLMBaseURL:       getEnv("LLM_BASE_URL", "https://api.moonshot.cn/v1"),
		TaskRepoMode:     getEnv("TASK_REPO_MODE", "redis"),
		TaskTTLHours:     getEnvInt("TASK_TTL_HOURS", 168),
		OSSProvider:      getEnv("OSS_PROVIDER", "local"),
		OSSBucket:        getEnv("OSS_BUCKET", "exports"),
		OSSRegion:        getEnv("OSS_REGION", ""),
		OSSSecretID:      getEnv("OSS_SECRET_ID", ""),
		OSSSecretKey:     getEnv("OSS_SECRET_KEY", ""),
		OSSSigningKey:    getEnv("OSS_SIGNING_KEY", ""),
		OSSLocalPath:     getEnv("OSS_LOCAL_PATH", "./data/oss"),
		RendererMode:     getEnv("RENDERER_MODE", "real"),
		PythonPath:       getEnv("PYTHON_PATH", "python"),
		RenderScriptPath: getEnv("RENDER_SCRIPT_PATH", "./internal/infra/renderer/render_page.py"),
		RenderDir:        getEnv("RENDER_DIR", "./data/renders"),
		RenderURLPrefix:  getEnv("RENDER_URL_PREFIX", ""),
		RenderTimeoutSec: getEnvInt("RENDER_TIMEOUT_SEC", 60),
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
