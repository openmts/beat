package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeConfig(t *testing.T, content string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agent.json")
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func validConfigJSON() string {
	return `{
  "server_url": "http://127.0.0.1:8080",
  "agent_token": "agent-secret",
  "node_name": "node-one",
  "advertised_host": "10.0.0.10",
  "ssh_port": 22,
  "report_interval": "15s"
}`
}

func TestLoadConfig(t *testing.T) {
	config, err := LoadConfig(writeConfig(t, validConfigJSON(), 0o600))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if config.NodeName != "node-one" || config.ReportInterval != 15*time.Second || config.SSHPort != 22 {
		t.Fatalf("config = %#v", config)
	}
}

func TestLoadConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		content string
		mode    os.FileMode
		want    string
	}{
		{name: "insecure permissions", content: validConfigJSON(), mode: 0o644, want: "permissions"},
		{name: "invalid json", content: "{", mode: 0o600, want: "decode"},
		{name: "unknown field", content: strings.Replace(validConfigJSON(), "\n}", ",\n\"extra\": true\n}", 1), mode: 0o600, want: "unknown field"},
		{name: "missing token", content: strings.Replace(validConfigJSON(), "agent-secret", "", 1), mode: 0o600, want: "agent_token"},
		{name: "invalid url", content: strings.Replace(validConfigJSON(), "http://127.0.0.1:8080", "file:///tmp/beat", 1), mode: 0o600, want: "server_url"},
		{name: "invalid port", content: strings.Replace(validConfigJSON(), "\"ssh_port\": 22", "\"ssh_port\": 70000", 1), mode: 0o600, want: "ssh_port"},
		{name: "invalid interval", content: strings.Replace(validConfigJSON(), "15s", "100ms", 1), mode: 0o600, want: "report_interval"},
		{name: "missing name", content: strings.Replace(validConfigJSON(), "node-one", "", 1), mode: 0o600, want: "node_name"},
		{name: "missing host", content: strings.Replace(validConfigJSON(), "10.0.0.10", "", 1), mode: 0o600, want: "advertised_host"},
		{name: "zero port", content: strings.Replace(validConfigJSON(), "\"ssh_port\": 22", "\"ssh_port\": 0", 1), mode: 0o600, want: "ssh_port"},
		{name: "multiple values", content: validConfigJSON() + "\n{}", mode: 0o600, want: "multiple JSON values"},
		{name: "trailing invalid", content: validConfigJSON() + "\n{", mode: 0o600, want: "decode config"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := LoadConfig(writeConfig(t, tt.content, tt.mode))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want containing %q", err, tt.want)
			}
		})
	}
	if _, err := LoadConfig(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Fatal("expected open error")
	}
}
