package config

import (
	"os"
	"testing"
)

func TestGetEnv_WithValue(t *testing.T) {
	os.Setenv("TEST_VA_KEY", "test_value")
	defer os.Unsetenv("TEST_VA_KEY")

	v := getEnv("TEST_VA_KEY", "default")
	if v != "test_value" {
		t.Errorf("got %q, want test_value", v)
	}
}

func TestGetEnv_Fallback(t *testing.T) {
	os.Unsetenv("TEST_VA_MISSING")
	v := getEnv("TEST_VA_MISSING", "fallback")
	if v != "fallback" {
		t.Errorf("got %q, want fallback", v)
	}
}

func TestLoadConfig_Defaults(t *testing.T) {
	for _, key := range []string{
		"SERVER_PORT", "ASR_MODE", "ASR_WS_URL", "TOKEN_BUDGET",
		"TTS_MODE", "TTS_URL", "SYSTEM_PROMPT",
	} {
		os.Unsetenv(key)
	}

	cfg := LoadConfig()
	if cfg.ServerPort != 9000 {
		t.Errorf("ServerPort = %d, want 9000", cfg.ServerPort)
	}
	if cfg.ASRMode != "local" {
		t.Errorf("ASRMode = %q, want local", cfg.ASRMode)
	}
	if cfg.TokenBudget != 50 {
		t.Errorf("TokenBudget = %d, want 50", cfg.TokenBudget)
	}
	if cfg.TTSMode != "local" {
		t.Errorf("TTSMode = %q, want local", cfg.TTSMode)
	}
	if cfg.MaxFillers != 3 {
		t.Errorf("MaxFillers = %d, want 3", cfg.MaxFillers)
	}
	if len(cfg.FillerPhrases) != 3 {
		t.Errorf("FillerPhrases len = %d, want 3", len(cfg.FillerPhrases))
	}
}

func TestLoadConfig_CustomPort(t *testing.T) {
	os.Setenv("SERVER_PORT", "8080")
	defer os.Unsetenv("SERVER_PORT")

	cfg := LoadConfig()
	if cfg.ServerPort != 8080 {
		t.Errorf("ServerPort = %d, want 8080", cfg.ServerPort)
	}
}

func TestLoadConfig_InvalidPort(t *testing.T) {
	os.Setenv("SERVER_PORT", "not_a_number")
	defer os.Unsetenv("SERVER_PORT")

	cfg := LoadConfig()
	if cfg.ServerPort != 0 {
		t.Errorf("ServerPort = %d, want 0 for invalid", cfg.ServerPort)
	}
}

func TestLoadConfig_RemoteASR(t *testing.T) {
	os.Setenv("ASR_MODE", "remote")
	os.Setenv("DOUBAO_ASR_APP_KEY", "key123")
	defer func() {
		os.Unsetenv("ASR_MODE")
		os.Unsetenv("DOUBAO_ASR_APP_KEY")
	}()

	cfg := LoadConfig()
	if cfg.ASRMode != "remote" {
		t.Errorf("ASRMode = %q", cfg.ASRMode)
	}
	if cfg.DouBaoASRAppKey != "key123" {
		t.Errorf("DouBaoASRAppKey = %q", cfg.DouBaoASRAppKey)
	}
}

func TestLoadConfig_ExternalServiceURLs(t *testing.T) {
	os.Setenv("PPT_AGENT_URL", "http://custom:9100")
	os.Setenv("KB_SERVICE_URL", "http://custom:9200")
	defer func() {
		os.Unsetenv("PPT_AGENT_URL")
		os.Unsetenv("KB_SERVICE_URL")
	}()

	cfg := LoadConfig()
	if cfg.PPTAgentURL != "http://custom:9100" {
		t.Errorf("PPTAgentURL = %q", cfg.PPTAgentURL)
	}
	if cfg.KBServiceURL != "http://custom:9200" {
		t.Errorf("KBServiceURL = %q", cfg.KBServiceURL)
	}
}
