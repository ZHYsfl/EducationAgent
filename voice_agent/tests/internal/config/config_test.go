package config_test

import (
	"os"
	"testing"

	cfg "voiceagent/internal/config"
)

func TestLoadConfig_Defaults(t *testing.T) {
	for _, key := range []string{
		"SERVER_PORT", "ASR_WS_URL", "TOKEN_BUDGET",
		"TTS_URL", "SYSTEM_PROMPT",
	} {
		os.Unsetenv(key)
	}

	conf := cfg.LoadConfig()
	if conf.ServerPort != 9000 {
		t.Errorf("ServerPort = %d, want 9000", conf.ServerPort)
	}
	if conf.TokenBudget != 50 {
		t.Errorf("TokenBudget = %d, want 50", conf.TokenBudget)
	}
	if conf.MaxFillers != 3 {
		t.Errorf("MaxFillers = %d, want 3", conf.MaxFillers)
	}
	if len(conf.FillerPhrases) != 3 {
		t.Errorf("FillerPhrases len = %d, want 3", len(conf.FillerPhrases))
	}
}

func TestLoadConfig_CustomPort(t *testing.T) {
	os.Setenv("SERVER_PORT", "8080")
	defer os.Unsetenv("SERVER_PORT")

	conf := cfg.LoadConfig()
	if conf.ServerPort != 8080 {
		t.Errorf("ServerPort = %d, want 8080", conf.ServerPort)
	}
}

func TestLoadConfig_InvalidPort(t *testing.T) {
	os.Setenv("SERVER_PORT", "not_a_number")
	defer os.Unsetenv("SERVER_PORT")

	conf := cfg.LoadConfig()
	if conf.ServerPort != 0 {
		t.Errorf("ServerPort = %d, want 0 for invalid", conf.ServerPort)
	}
}

func TestLoadConfig_ExternalServiceURLs(t *testing.T) {
	os.Setenv("PPT_AGENT_URL", "http://custom:9100")
	os.Setenv("KB_SERVICE_URL", "http://custom:9200")
	defer func() {
		os.Unsetenv("PPT_AGENT_URL")
		os.Unsetenv("KB_SERVICE_URL")
	}()

	conf := cfg.LoadConfig()
	if conf.PPTAgentURL != "http://custom:9100" {
		t.Errorf("PPTAgentURL = %q", conf.PPTAgentURL)
	}
	if conf.KBServiceURL != "http://custom:9200" {
		t.Errorf("KBServiceURL = %q", conf.KBServiceURL)
	}
}
