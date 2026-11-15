package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadServerDefaultsAndFlags(t *testing.T) {
	for _, key := range []string{"BEAT_LISTEN", "BEAT_DATA_DIR", "BEAT_AGENT_TOKEN", "BEAT_AGENT_BINARY_DIR", "BEAT_DEMO_MODE", "BEAT_ENROLLMENT_TOKEN_TTL"} {
		t.Setenv(key, "")
	}
	cfg, err := LoadServer(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != "[::]:9099" {
		t.Fatalf("default listen = %q", cfg.Listen)
	}
	if !filepath.IsAbs(cfg.DataDir) || cfg.DatabasePath != filepath.Join(cfg.DataDir, "beat.db") || cfg.MTSDataPath != filepath.Join(cfg.DataDir, "mts") {
		t.Fatalf("unexpected derived paths: %+v", cfg)
	}
	if cfg.EnrollmentTokenTTL != 10*time.Minute || cfg.AgentToken != "" || cfg.DemoMode {
		t.Fatalf("unexpected secure defaults: %+v", cfg)
	}

	cfg, err = LoadServer([]string{
		"--listen", "127.0.0.1:18099",
		"--data-dir", t.TempDir(),
		"--demo",
		"--enrollment-token-ttl", "2m",
		"--agent-token", "0123456789abcdef0123456789abcdef",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != "127.0.0.1:18099" || !cfg.DemoMode || cfg.EnrollmentTokenTTL != 2*time.Minute {
		t.Fatalf("flags were not applied: %+v", cfg)
	}
}

func TestLoadServerRejectsWeakTokenAndInvalidTTL(t *testing.T) {
	if _, err := LoadServer([]string{"--agent-token", "short"}); err == nil {
		t.Fatal("weak fixed Agent token was accepted")
	}
	if _, err := LoadServer([]string{"--enrollment-token-ttl", "30s"}); err == nil {
		t.Fatal("invalid enrollment token TTL was accepted")
	}
}

func TestLoadResetAdminAcceptsDataDirFlag(t *testing.T) {
	dir := t.TempDir()
	cfg, err := LoadResetAdmin([]string{"--data-dir", dir})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DataDir != dir || cfg.DatabasePath != filepath.Join(dir, "beat.db") {
		t.Fatalf("unexpected reset-admin config: %+v", cfg)
	}
}

func TestLoadServerYAMLAndFlagPrecedence(t *testing.T) {
	for _, key := range []string{"BEAT_CONFIG", "BEAT_LISTEN", "BEAT_DATA_DIR", "BEAT_AGENT_TOKEN", "BEAT_AGENT_BINARY_DIR", "BEAT_DEMO_MODE", "BEAT_ENROLLMENT_TOKEN_TTL"} {
		t.Setenv(key, "")
	}
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "data")
	configPath := filepath.Join(dir, "server.yaml")
	body := "listen: 127.0.0.1:18098\n" +
		"data_dir: " + dataDir + "\n" +
		"agent_binary_dir: /usr/local/bin\n" +
		"demo_mode: true\n" +
		"enrollment_token_ttl: 3m\n" +
		"agent_token: 0123456789abcdef0123456789abcdef\n"
	if err := os.WriteFile(configPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BEAT_LISTEN", "127.0.0.1:18097")

	cfg, err := LoadServer([]string{"--config", configPath})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != "127.0.0.1:18098" || cfg.DataDir != dataDir || cfg.AgentBinaryDir != "/usr/local/bin" || !cfg.DemoMode || cfg.EnrollmentTokenTTL != 3*time.Minute {
		t.Fatalf("YAML configuration was not applied: %+v", cfg)
	}

	cfg, err = LoadServer([]string{"--config", configPath, "--listen", "127.0.0.1:18099", "--enrollment-token-ttl", "4m"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != "127.0.0.1:18099" || cfg.EnrollmentTokenTTL != 4*time.Minute {
		t.Fatalf("flags did not override YAML: %+v", cfg)
	}
}

func TestLoadServerRejectsInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"unknown":     "listen: ':9099'\nunknown_field: true\n",
		"duration":    "enrollment_token_ttl: tomorrow\n",
		"multi":       "listen: ':9099'\n---\nlisten: ':9098'\n",
		"invalid_tls": "tls_cert: /tmp/cert.pem\n",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(dir, name+".yaml")
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadServer([]string{"--config", path}); err == nil {
				t.Fatalf("invalid YAML %q was accepted", body)
			}
		})
	}
}

func TestLoadServerTreatsEmptyYAMLAsDefaults(t *testing.T) {
	for _, key := range []string{"BEAT_CONFIG", "BEAT_LISTEN", "BEAT_DATA_DIR", "BEAT_AGENT_TOKEN", "BEAT_AGENT_BINARY_DIR", "BEAT_DEMO_MODE", "BEAT_ENROLLMENT_TOKEN_TTL"} {
		t.Setenv(key, "")
	}
	path := filepath.Join(t.TempDir(), "server.yaml")
	if err := os.WriteFile(path, []byte(" \n# intentionally empty\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadServer([]string{"--config", path})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != DefaultListen || cfg.EnrollmentTokenTTL != DefaultTokenTTL || cfg.DemoMode {
		t.Fatalf("empty YAML did not preserve defaults: %+v", cfg)
	}
}

func TestServerUnitLoadsYAMLConfiguration(t *testing.T) {
	path := filepath.Join("..", "..", "deploy", "beat-server.service")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	unit := string(body)
	if !strings.Contains(unit, "ExecStart=/usr/local/bin/beat-server --config /etc/beat/server.yaml") {
		t.Fatalf("%s does not load the YAML configuration", path)
	}
	if strings.Contains(unit, "EnvironmentFile=") {
		t.Fatalf("%s still loads an environment file", path)
	}
}

func TestLoadResetAdminUsesYAMLDataDir(t *testing.T) {
	dir := t.TempDir()
	dataDir := filepath.Join(dir, "reset-data")
	path := filepath.Join(dir, "server.yaml")
	if err := os.WriteFile(path, []byte("data_dir: "+dataDir+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadResetAdmin([]string{"--config", path})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DataDir != dataDir || cfg.DatabasePath != filepath.Join(dataDir, "beat.db") {
		t.Fatalf("unexpected reset-admin config: %+v", cfg)
	}
}
