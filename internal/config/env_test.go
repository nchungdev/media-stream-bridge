package config

import (
	"strings"
	"testing"
)

func setValid(t *testing.T) {
	t.Helper()
	t.Setenv("MSB_QBIT_USERNAME", "sonarr")
	t.Setenv("MSB_QBIT_PASSWORD", "a-long-enough-password")
	t.Setenv("MSB_API_KEY", "0123456789abcdef0123456789")
}

func TestApplyEnvDefaults(t *testing.T) {
	setValid(t)
	cfg := Default()
	if err := cfg.ApplyEnv(); err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Host != "127.0.0.1" || cfg.Server.Port != 6890 || cfg.MaxJobs != 200 {
		t.Fatalf("unexpected defaults: %+v", cfg.Server)
	}
}

func TestApplyEnvRejectsWeak(t *testing.T) {
	cases := map[string]map[string]string{
		"missing user":   {"MSB_QBIT_USERNAME": ""},
		"admin password": {"MSB_QBIT_PASSWORD": "admin"},
		"short password": {"MSB_QBIT_PASSWORD": "short"},
		"short api key":  {"MSB_API_KEY": "short"},
		"published key":  {"MSB_API_KEY": "fshare-bridge-secret"},
		"bad port":       {"MSB_PORT": "99999"},
		"bad max jobs":   {"MSB_MAX_JOBS": "0"},
		"bad timeout":    {"MSB_EPISODE_TIMEOUT": "forever"},
	}
	for name, env := range cases {
		t.Run(name, func(t *testing.T) {
			setValid(t)
			for k, v := range env {
				t.Setenv(k, v)
			}
			if err := Default().ApplyEnv(); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestAPIKeyFallbackFromFile(t *testing.T) {
	setValid(t)
	t.Setenv("MSB_API_KEY", "")
	cfg := Default()
	cfg.Server.APIKey = "fshare-bridge-secret"
	if err := cfg.ApplyEnv(); err == nil || !strings.Contains(err.Error(), "MSB_API_KEY") {
		t.Fatalf("want api key error, got %v", err)
	}
	cfg = Default()
	cfg.Server.APIKey = "file-api-key-0123456789abcdef"
	if err := cfg.ApplyEnv(); err != nil {
		t.Fatal(err)
	}
}

func TestListenHostIgnoresFile(t *testing.T) {
	setValid(t)
	cfg := Default()
	cfg.Server.Host = "0.0.0.0"
	if err := cfg.ApplyEnv(); err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Host != "127.0.0.1" {
		t.Fatalf("host = %q", cfg.Server.Host)
	}
	t.Setenv("MSB_LISTEN_HOST", "10.0.0.5")
	if err := Default().ApplyEnv(); err != nil {
		t.Fatal(err)
	}
}
