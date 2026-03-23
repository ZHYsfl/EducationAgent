package config

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Host                    string
	Port                    int
	RedisURL                string
	SnapshotTTL             int
	DBServiceURL            string
	DBTimeoutSec            int
	InternalKey             string
	PublicBaseURL           string
	JWTSecret               string
	JWTAlg                  string
	JWTRequireExp           bool
	AuthDisabled            bool
	PPTAGENTModel           string
	PPTAGENTVisionModel     string
	PPTAGENTAPIBase         string
	PPTAGENTAPIKey          string
	RunsDir                 string
	// CanvasRenderMode: placeholder（默认）| chromedp | auto（chromedp 失败则占位图）
	CanvasRenderMode string
	ChromePath string // PPT_CHROME_PATH，可选；chromedp 用
}

func Load() Config {
	port, _ := strconv.Atoi(strings.TrimSpace(env("PPT_AGENT_PORT", "9100")))
	ttl, _ := strconv.Atoi(strings.TrimSpace(env("PPT_SNAPSHOT_TTL_SEC", "300")))
	dbto, _ := strconv.Atoi(strings.TrimSpace(env("PPT_DATABASE_SERVICE_TIMEOUT", "120")))
	vm := strings.TrimSpace(os.Getenv("PPTAGENT_VISION_MODEL"))
	if vm == "" {
		vm = strings.TrimSpace(os.Getenv("PPTAGENT_MODEL"))
	}
	return Config{
		Host:                env("PPT_AGENT_HOST", "0.0.0.0"),
		Port:                port,
		RedisURL:            env("REDIS_URL", "redis://127.0.0.1:6379/0"),
		SnapshotTTL:         ttl,
		DBServiceURL:        strings.TrimSpace(os.Getenv("PPT_DATABASE_SERVICE_URL")),
		DBTimeoutSec:        dbto,
		InternalKey:         strings.TrimSpace(os.Getenv("INTERNAL_KEY")),
		PublicBaseURL:       strings.TrimRight(strings.TrimSpace(os.Getenv("PPT_PUBLIC_BASE_URL")), "/"),
		JWTSecret:           strings.TrimSpace(os.Getenv("PPT_JWT_SECRET")),
		JWTAlg:              env("PPT_JWT_ALGORITHM", "HS256"),
		JWTRequireExp:       strings.ToLower(env("PPT_JWT_REQUIRE_EXP", "true")) == "true" || env("PPT_JWT_REQUIRE_EXP", "true") == "",
		AuthDisabled:        parseBool(os.Getenv("PPT_AUTH_DISABLED")),
		PPTAGENTModel:       strings.TrimSpace(os.Getenv("PPTAGENT_MODEL")),
		PPTAGENTVisionModel: vm,
		PPTAGENTAPIBase:     strings.TrimRight(strings.TrimSpace(os.Getenv("PPTAGENT_API_BASE")), "/"),
		PPTAGENTAPIKey:      strings.TrimSpace(os.Getenv("PPTAGENT_API_KEY")),
		RunsDir:             env("PPT_RUNS_DIR", "runs"),
		CanvasRenderMode:    strings.ToLower(strings.TrimSpace(os.Getenv("PPT_CANVAS_RENDER"))),
		ChromePath:          strings.TrimSpace(os.Getenv("PPT_CHROME_PATH")),
	}
}

func env(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}

func parseBool(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	return s == "1" || s == "true" || s == "yes"
}

func (c Config) Enforced() bool {
	return false
}
